package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// 按恢复时间自动恢复（recovery_at 驱动）。
//
// 与「健康监测探活」的分工：
//   - 本任务负责「已知恢复时间」的渠道：到点即启用，不做探测、不看禁用原因关键词、
//     也不受单渠道「健康监测」开关影响（那是探活开关）。上游给的重置时间是权威信息，
//     到点直接恢复最可靠，也避免探活成功但真实额度仍耗尽导致的启用→429→禁用回环。
//   - 健康监测负责「没有恢复时间」的渠道：靠探活判断上游是否已恢复。
//
// 手动禁用（状态 2）不在此任务范围内——管理员明确停用的渠道不该被自动拉起。

var (
	recoveryTaskOnce  sync.Once
	recoveryLogPrefix = "[恢复时间]"
)

// StartChannelRecoveryTask 启动按恢复时间恢复的后台任务，每 30 秒扫描一次。
func StartChannelRecoveryTask() {
	if !common.IsMasterNode {
		return
	}
	recoveryTaskOnce.Do(func() {
		common.SysLog(recoveryLogPrefix + " 按恢复时间自动恢复任务已启动")
		go func() {
			defer func() {
				if r := recover(); r != nil {
					common.SysError(fmt.Sprintf("%s panic: %v", recoveryLogPrefix, r))
				}
			}()
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				RecoverChannelsByRecoveryAt()
			}
		}()
	})
}

// RecoverChannelsByRecoveryAt 扫描所有自动禁用且带未到点恢复时间的渠道，到点即启用。
// 导出以便单测直接调用（无定时器依赖）。
func RecoverChannelsByRecoveryAt() {
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		common.SysError(recoveryLogPrefix + " 获取渠道列表失败: " + err.Error())
		return
	}
	now := time.Now()
	for _, ch := range channels {
		if ch.Status != common.ChannelStatusAutoDisabled {
			continue
		}
		info := ch.GetOtherInfo()
		recoveryAt := ParseRecoveryAt(info["recovery_at"])
		if recoveryAt <= 0 || now.Unix() < recoveryAt {
			continue
		}
		// 处于定时暂停窗口内不抢跑：窗口结束后由定时暂停模块拉回，避免维护时段重新接流
		if IsChannelInPauseWindow(ch) {
			common.SysLog(fmt.Sprintf("%s 渠道「%s」(#%d) 已到恢复时间，但当前处于定时暂停窗口，暂不恢复",
				recoveryLogPrefix, ch.Name, ch.Id))
			continue
		}
		reason, _ := info["status_reason"].(string)
		EnableChannel(ch.Id, "", ch.Name)
		common.SysLog(fmt.Sprintf("%s 渠道「%s」(#%d) 已到恢复时间 %s，自动恢复（禁用原因：%s）",
			recoveryLogPrefix, ch.Name, ch.Id,
			time.Unix(recoveryAt, 0).Format("2006-01-02 15:04:05"),
			common.LocalLogPreview(reason)))
	}
}

// ParseRecoveryAt 解析 other_info.recovery_at（JSON 反序列化后可能是 float64/int64/int）。
// 非以上类型或缺失时返回 0。
func ParseRecoveryAt(raw interface{}) int64 {
	switch v := raw.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}

// ChannelRecoveryAt 读取渠道 other_info 中记录的恢复时间戳（秒），无则返回 0。
func ChannelRecoveryAt(ch *model.Channel) int64 {
	return ParseRecoveryAt(ch.GetOtherInfo()["recovery_at"])
}
