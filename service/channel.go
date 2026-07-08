package service

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

func formatNotifyType(channelId int, status int) string {
	return fmt.Sprintf("%s_%d_%d", dto.NotifyTypeChannelUpdate, channelId, status)
}

// disable & notify
func DisableChannel(channelError types.ChannelError, reason string) {
	common.SysLog(fmt.Sprintf("通道「%s」（#%d）发生错误，准备禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, common.LocalLogPreview(reason)))

	// 检查是否启用自动禁用功能
	if !channelError.AutoBan {
		common.SysLog(fmt.Sprintf("通道「%s」（#%d）未启用自动禁用功能，跳过禁用操作", channelError.ChannelName, channelError.ChannelId))
		return
	}

	// 解析 429 错误中的恢复时间（如 "It will reset at 2026-07-13 00:00:00 +0800 CST"）
	recoveryAt := parseQuotaResetTime(reason)

	success := model.UpdateChannelStatusWithRecovery(channelError.ChannelId, channelError.UsingKey, common.ChannelStatusAutoDisabled, reason, recoveryAt)
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被禁用", channelError.ChannelName, channelError.ChannelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, reason)
		if recoveryAt > 0 {
			content += fmt.Sprintf("，预计恢复时间：%s", time.Unix(recoveryAt, 0).Format("2006-01-02 15:04:05"))
		}
		NotifyRootUser(formatNotifyType(channelError.ChannelId, common.ChannelStatusAutoDisabled), subject, content)
	}
}

// parseQuotaResetTime 从 429 错误信息中解析恢复时间
// 示例："It will reset at 2026-07-13 00:00:00 +0800 CST"
func parseQuotaResetTime(reason string) int64 {
	// 匹配 "reset at YYYY-MM-DD HH:MM:SS" 格式
	re := regexp.MustCompile(`reset at (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})`)
	matches := re.FindStringSubmatch(reason)
	if len(matches) < 2 {
		return 0
	}
	// 尝试解析时间（带时区）
	layouts := []string{
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05 MST",
		"2006-01-02 15:04:05",
	}
	// 提取时区信息
	tzMatch := regexp.MustCompile(`(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\s+(.+)$`).FindStringSubmatch(reason)
	timeStr := matches[1]
	tzStr := ""
	if len(tzMatch) >= 3 {
		tzStr = strings.TrimSpace(tzMatch[2])
	}
	for _, layout := range layouts {
		var t time.Time
		var err error
		if tzStr != "" && (strings.Contains(layout, "MST") || strings.Contains(layout, "-0700")) {
			t, err = time.Parse(layout, timeStr+" "+tzStr)
		} else {
			t, err = time.Parse(layout, timeStr)
		}
		if err == nil {
			// 如果没时区信息，默认用 +0800 (CST)
			if t.Location() == time.UTC && tzStr == "" {
				loc, _ := time.LoadLocation("Asia/Shanghai")
				if loc != nil {
					t, _ = time.ParseInLocation(layout, timeStr, loc)
				}
			}
			return t.Unix()
		}
	}
	return 0
}

func EnableChannel(channelId int, usingKey string, channelName string) {
	success := model.UpdateChannelStatus(channelId, usingKey, common.ChannelStatusEnabled, "")
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		NotifyRootUser(formatNotifyType(channelId, common.ChannelStatusEnabled), subject, content)
	}
}

func ShouldDisableChannel(err *types.NewAPIError) bool {
	if !common.AutomaticDisableChannelEnabled {
		return false
	}
	if err == nil {
		return false
	}
	if types.IsChannelError(err) {
		return true
	}
	if types.IsSkipRetryError(err) {
		return false
	}
	if operation_setting.ShouldDisableByStatusCode(err.StatusCode) {
		return true
	}

	lowerMessage := strings.ToLower(err.Error())
	search, _ := AcSearch(lowerMessage, operation_setting.AutomaticDisableKeywords, true)
	return search
}

func ShouldEnableChannel(newAPIError *types.NewAPIError, status int) bool {
	if !common.AutomaticEnableChannelEnabled {
		return false
	}
	if newAPIError != nil {
		return false
	}
	if status != common.ChannelStatusAutoDisabled {
		return false
	}
	return true
}
