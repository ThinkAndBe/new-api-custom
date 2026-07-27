package service

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

var (
	schedulePauseOnce      sync.Once
	schedulePauseLogPrefix = "[定时暂停]"
)

// StartChannelSchedulePauseTask 启动定时暂停后台任务，每 1 分钟扫描所有启用了定时暂停的渠道。
// 根据配置的规则判断当前时间是否在暂停窗口内，自动切换渠道状态 1↔4。
func StartChannelSchedulePauseTask() {
	if !common.IsMasterNode {
		return
	}
	schedulePauseOnce.Do(func() {
		common.SysLog(schedulePauseLogPrefix + " 定时暂停任务已启动")
		go func() {
			defer func() {
				if r := recover(); r != nil {
					common.SysError(schedulePauseLogPrefix + " panic: " + fmt.Sprintf("%v", r))
				}
			}()
			ticker := time.NewTicker(1 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				processSchedulePause()
			}
		}()
	})
}

func processSchedulePause() {
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		common.SysError(schedulePauseLogPrefix + " 获取渠道列表失败: " + err.Error())
		return
	}
	now := time.Now()
	weekdayStr := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[int(now.Weekday())]
	pausedCount := 0
	resumedCount := 0
	scannedCount := 0
	for _, ch := range channels {
		// 只处理状态为 1（启用）、4（定时暂停中）或 3（自动禁用）的渠道。
		// 3 也要管：定时暂停期间渠道可能因其它原因被自动禁用(3)，若不在此处理，
		// 暂停窗口结束后就没人把它恢复回来（表现为"到点不恢复"）。
		if ch.Status != common.ChannelStatusEnabled && ch.Status != common.ChannelStatusSchedulePaused && ch.Status != common.ChannelStatusAutoDisabled {
			continue
		}
		settings := ch.GetOtherSettings()
		if !settings.SchedulePauseEnabled || len(settings.SchedulePauseRules) == 0 {
			// 如果渠道状态是 4 但已关闭定时暂停，恢复为 1
			if ch.Status == common.ChannelStatusSchedulePaused {
				model.UpdateChannelStatus(ch.Id, "", common.ChannelStatusEnabled, "定时暂停已关闭，自动恢复")
				common.SysLog(fmt.Sprintf("%s 渠道「%s」(#%d) 定时暂停已关闭，自动恢复", schedulePauseLogPrefix, ch.Name, ch.Id))
				resumedCount++
			}
			continue
		}
		scannedCount++

		shouldPause, matchedRule := isInAnyPauseWindowWithRule(now, settings.SchedulePauseRules)

		switch {
		case shouldPause && ch.Status == common.ChannelStatusEnabled:
			// 进入暂停
			reason := "定时暂停"
			if matchedRule != nil && matchedRule.Reason != "" {
				reason = "定时暂停：" + matchedRule.Reason
			}
			if model.UpdateChannelStatus(ch.Id, "", common.ChannelStatusSchedulePaused, reason) {
				common.SysLog(fmt.Sprintf("%s 渠道「%s」(#%d) 已暂停 (%s %s %s~%s)",
					schedulePauseLogPrefix, ch.Name, ch.Id, weekdayStr, reason, matchedRule.Start, matchedRule.End))
				pausedCount++
			}
		case shouldPause && ch.Status == common.ChannelStatusAutoDisabled:
			// 自动禁用但当前处于暂停窗口：纠正回定时暂停，避免被卡在 3 导致窗口结束不恢复
			if model.UpdateChannelStatus(ch.Id, "", common.ChannelStatusSchedulePaused, "定时暂停") {
				common.SysLog(fmt.Sprintf("%s 渠道「%s」(#%d) 从自动禁用纠正为定时暂停 (%s)", schedulePauseLogPrefix, ch.Name, ch.Id, weekdayStr))
			}
		case !shouldPause && (ch.Status == common.ChannelStatusSchedulePaused || ch.Status == common.ChannelStatusAutoDisabled):
			// 恢复：无论是定时暂停(4)还是暂停期间被自动禁用(3)，窗口结束都拉回启用
			if model.UpdateChannelStatus(ch.Id, "", common.ChannelStatusEnabled, "定时暂停结束，自动恢复") {
				common.SysLog(fmt.Sprintf("%s 渠道「%s」(#%d) 已恢复 (%s 暂停窗口已结束)", schedulePauseLogPrefix, ch.Name, ch.Id, weekdayStr))
				resumedCount++
			}
		}
	}
	if pausedCount > 0 || resumedCount > 0 {
		common.SysLog(fmt.Sprintf("%s 扫描完成 %s %s: 暂停%d 恢复%d（共扫描 %d 个开启定时暂停的渠道）", schedulePauseLogPrefix, weekdayStr, now.Format("15:04"), pausedCount, resumedCount, scannedCount))
	}
}

// isInAnyPauseWindow 判断当前时间是否在任一暂停规则窗口内
func isInAnyPauseWindow(now time.Time, rules []dto.SchedulePauseRule) bool {
	_, rule := isInAnyPauseWindowWithRule(now, rules)
	return rule != nil
}

// isInAnyPauseWindowWithRule 返回是否在暂停窗口内，以及命中的规则
func isInAnyPauseWindowWithRule(now time.Time, rules []dto.SchedulePauseRule) (bool, *dto.SchedulePauseRule) {
	weekday := int(now.Weekday()) // 0=Sunday, 1=Monday, ..., 6=Saturday
	currentMinutes := now.Hour()*60 + now.Minute()

	for i := range rules {
		rule := &rules[i]
		// 检查星期匹配
		dayMatch := false
		for _, d := range rule.Days {
			if d == weekday {
				dayMatch = true
				break
			}
		}
		if !dayMatch {
			continue
		}

		// 解析起止时间
		startMin := parseTimeToMinutes(rule.Start)
		endMin := parseTimeToMinutes(rule.End)
		if startMin < 0 || endMin < 0 {
			continue
		}

		// 判断：支持跨天（start > end 时，表示到次日）
		if startMin <= endMin {
			if currentMinutes >= startMin && currentMinutes < endMin {
				return true, rule
			}
		} else {
			// 跨天：当天 start~次日 end
			if currentMinutes >= startMin || currentMinutes < endMin {
				return true, rule
			}
		}
	}
	return false, nil
}

// parseTimeToMinutes 解析 "HH:MM" 格式为分钟数
func parseTimeToMinutes(t string) int {
	parts := strings.Split(t, ":")
	if len(parts) != 2 {
		return -1
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return -1
	}
	return h*60 + m
}