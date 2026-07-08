package controller

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
)

var (
	healthMonitorOnce      sync.Once
	healthMonitorLogPrefix = "[健康监测]"

	// 内存健康状态
	healthState     = make(map[int]*channelHealthState)
	healthStateLock sync.Mutex
)

type channelHealthState struct {
	ConsecutiveFailures int       // 连续失败次数
	LastCheckAt         time.Time // 上次检测时间
	CooldownUntil       time.Time // 冷却到期时间（避免频繁探活失败时反复测）
}

func getHealthState(channelId int) *channelHealthState {
	healthStateLock.Lock()
	defer healthStateLock.Unlock()
	if _, ok := healthState[channelId]; !ok {
		healthState[channelId] = &channelHealthState{}
	}
	return healthState[channelId]
}

func resetHealthState(channelId int) {
	healthStateLock.Lock()
	defer healthStateLock.Unlock()
	delete(healthState, channelId)
}

// StartChannelHealthMonitorTask 启动健康监测后台任务，每 30s 扫描一次所有启用了健康监测的渠道。
func StartChannelHealthMonitorTask() {
	if !common.IsMasterNode {
		return
	}
	healthMonitorOnce.Do(func() {
		common.SysLog(healthMonitorLogPrefix + " 健康监测任务已启动")
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.LogError(nil, fmt.Sprintf("%s panic: %v", healthMonitorLogPrefix, r))
				}
			}()
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				checkAllChannelsHealth()
			}
		}()
	})
}

func checkAllChannelsHealth() {
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		common.SysError(healthMonitorLogPrefix + " 获取渠道列表失败: " + err.Error())
		return
	}
	for _, ch := range channels {
		// 默认只探测「自动禁用」的渠道（被动恢复模式）
		// 正常启用的渠道靠真实请求失败自动禁用，无需定时探测，避免浪费 token
		if ch.Status != common.ChannelStatusAutoDisabled {
			continue
		}

		settings := ch.GetOtherSettings()
		// opt-out 语义：默认参与自动恢复，只有显式设置 health_check_disabled=true 才跳过
		if settings.HealthCheckDisabled {
			continue
		}

		state := getHealthState(ch.Id)

		// 冷却期内跳过
		if time.Now().Before(state.CooldownUntil) {
			continue
		}

		// 判断是否到检测间隔（用渠道配置的间隔，默认 5 分钟）
		interval := settings.HealthCheckIntervalMinutes
		if interval <= 0 {
			interval = 5
		}
		if !state.LastCheckAt.IsZero() && time.Since(state.LastCheckAt) < time.Duration(interval)*time.Minute {
			continue
		}
		state.LastCheckAt = time.Now()

		// 检测渠道
		testUserID, err := resolveChannelTestUserID(nil)
		if err != nil {
			common.SysError(healthMonitorLogPrefix + " 获取测试用户失败: " + err.Error())
			return
		}
		checkSingleChannelHealth(ch, state, testUserID)
	}
}

func checkSingleChannelHealth(ch *model.Channel, state *channelHealthState, testUserID int) {
	// 检查禁用原因和恢复时间
	if ch.Status == common.ChannelStatusAutoDisabled {
		info := ch.GetOtherInfo()
		reason := ""
		if r, ok := info["status_reason"].(string); ok {
			reason = r
		}

		// 检查是否有预设恢复时间（429 额度重置）
		// recovery_at 存储为 JSON number，从 other_info 解析出来是 float64
		recoveryAt := int64(0)
		if r, ok := info["recovery_at"].(float64); ok {
			recoveryAt = int64(r)
		} else if r, ok := info["recovery_at"].(int64); ok {
			recoveryAt = r
		} else if r, ok := info["recovery_at"].(int); ok {
			recoveryAt = int64(r)
		}

		isQuota := isQuotaExhaustedReason(reason)
		common.SysLog(fmt.Sprintf("%s 渠道「%s」(#%d) 检查恢复: reason=%s, isQuota=%v, recoveryAt=%d, now=%d, otherInfo=%v",
			healthMonitorLogPrefix, ch.Name, ch.Id,
			common.LocalLogPreview(reason), isQuota, recoveryAt, time.Now().Unix(), info))

		if isQuota {
			if recoveryAt > 0 {
				// 有恢复时间：到时间后直接恢复（不需探测，因为额度已重置）
				if time.Now().Unix() >= recoveryAt {
					service.EnableChannel(ch.Id, "", ch.Name)
					common.SysLog(fmt.Sprintf("%s 渠道「%s」(#%d) 额度已重置，自动恢复", healthMonitorLogPrefix, ch.Name, ch.Id))
					return
				}
				// 没到恢复时间，跳过探测
				common.SysLog(fmt.Sprintf("%s 渠道「%s」(#%d) 额度未到恢复时间，跳过探测 (剩余 %s)",
					healthMonitorLogPrefix, ch.Name, ch.Id,
					time.Unix(recoveryAt, 0).Sub(time.Now()).Truncate(time.Second)))
				return
			}
			// 没有恢复时间的额度用尽，跳过探测（需要手动恢复）
			common.SysLog(fmt.Sprintf("%s 渠道「%s」(#%d) 额度用尽但无恢复时间，需手动恢复", healthMonitorLogPrefix, ch.Name, ch.Id))
			return
		}
	}

	// 使用 testChannel 做真实探活请求
	result := testChannel(ch, testUserID, "", "", false)

	if result.newAPIError == nil && result.localErr == nil {
		// 探活成功
		state.ConsecutiveFailures = 0
		state.CooldownUntil = time.Time{}

		// 仅恢复「自动禁用」的渠道，不恢复「手动禁用」（管理员明确意图停用）
		if ch.Status == common.ChannelStatusAutoDisabled {
			service.EnableChannel(ch.Id, "", ch.Name)
			common.SysLog(fmt.Sprintf("%s 渠道「%s」(#%d) 探活成功，已自动恢复", healthMonitorLogPrefix, ch.Name, ch.Id))
		}
		return
	}

	// 探活失败
	state.ConsecutiveFailures++

	// 获取失败阈值
	threshold := ch.GetOtherSettings().HealthCheckFailureThreshold
	if threshold <= 0 {
		threshold = 3
	}

	common.SysLog(fmt.Sprintf("%s 渠道「%s」(#%d) 探活失败 (%d/%d): %s",
		healthMonitorLogPrefix, ch.Name, ch.Id,
		state.ConsecutiveFailures, threshold,
		formatTestError(result.newAPIError, result.localErr)))

if state.ConsecutiveFailures >= threshold {
			// 达到阈值，禁用渠道（健康监测开启时无视 auto_ban 设置）
			chName := ch.Name
			chId := ch.Id
			chKey := ""

			service.DisableChannel(
				*types.NewChannelError(chId, ch.Type, chName, false, chKey, true),
				fmt.Sprintf("健康监测：连续 %d 次探活失败", state.ConsecutiveFailures),
			)
			common.SysLog(fmt.Sprintf("%s 渠道「%s」(#%d) 已自动禁用（连续%d次失败）",
				healthMonitorLogPrefix, chName, chId, state.ConsecutiveFailures))

		// 设置退避冷却（指数退避：60s→120s→300s，上限30min）
		backoff := []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute}
		cooldown := backoff[0]
		if state.ConsecutiveFailures-1 < len(backoff) {
			cooldown = backoff[state.ConsecutiveFailures-1]
		} else {
			cooldown = 30 * time.Minute
		}
		state.CooldownUntil = time.Now().Add(cooldown)

		// 重置失败计数（进入冷却期）
		state.ConsecutiveFailures = 0

		common.SysLog(fmt.Sprintf("%s 渠道「%s」(#%d) 进入冷却期 %.0f 分钟",
			healthMonitorLogPrefix, chName, chId, cooldown.Minutes()))
	}
}

// formatTestError 把 testChannel 返回的错误转为字符串
func formatTestError(newAPIErr *types.NewAPIError, localErr error) string {
	if newAPIErr != nil {
		return newAPIErr.Error()
	}
	if localErr != nil {
		return localErr.Error()
	}
	return "未知错误"
}

// resolveChannelTestUserID 是从 channel-test.go 引用的辅助函数。
// 为避免循环依赖，此处不再重复实现，testChannel 内部会自行处理。
// testChannel 中当 testUserID=0 时会通过 resolveChannelTestUserID(nil) 获取 root 用户。
// isQuotaExhaustedReason 判断渠道禁用原因是否为额度用尽（429/quota）
// 这类原因禁用的渠道不应自动恢复，因为测试请求不消耗额度，探测会"成功"但实际请求仍会 429
func isQuotaExhaustedReason(reason string) bool {
	if reason == "" {
		return false
	}
	lower := strings.ToLower(reason)
	// 匹配 429 状态码、额度用尽、配额超限等关键词
	keywords := []string{
		"429",
		"quota",
		"exceeded",
		"weekly usage",
		"daily usage",
		"monthly usage",
		"rate limit",
		"insufficient",
		"额度",
		"配额",
		"限流",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
