package service

import (
	"fmt"
	"regexp"
	"strconv"
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
	if recoveryAt > 0 {
		common.SysLog(fmt.Sprintf("通道「%s」（#%d）解析到额度恢复时间：%s (unix=%d)",
			channelError.ChannelName, channelError.ChannelId,
			time.Unix(recoveryAt, 0).Format("2006-01-02 15:04:05"), recoveryAt))
	} else if isLikelyQuotaError(reason) {
		// 看起来是额度错误但没解析出时间，记录便于排查
		common.SysLog(fmt.Sprintf("通道「%s」（#%d）疑似额度错误但未解析到恢复时间，原始原因：%s",
			channelError.ChannelName, channelError.ChannelId, common.LocalLogPreview(reason)))
	}

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
// 支持多种格式：
//   - "It will reset at 2026-07-13 00:00:00 +0800 CST"（阿里云 DashScope）
//   - "reset at 2026-07-13 00:00:00"（无时区，默认 Asia/Shanghai）
//   - "Try again at 2026-07-13T00:00:00Z"（OpenAI ISO 8601）
//   - "Retry-After: 3600"（秒数）
//   - "请于 2026-07-13 00:00:00 后重试"（中文）
func parseQuotaResetTime(reason string) int64 {
	now := time.Now()

	// 1. Retry-After: <seconds> 秒数格式
	retryAfterRe := regexp.MustCompile(`(?i)retry[-_ ]?after[:\s]+(\d+)`)
	if m := retryAfterRe.FindStringSubmatch(reason); len(m) >= 2 {
		if secs, err := strconv.ParseInt(m[1], 10, 64); err == nil && secs > 0 && secs < 30*24*3600 {
			return now.Unix() + secs
		}
	}

	// 2. "reset at YYYY-MM-DD HH:MM:SS [tz]" 格式（含 "It will reset at"）
	timeStr, tzStr := extractDateTimeWithTz(reason, `reset at (\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2})`)
	if t, ok := parseWithOptionalTz(timeStr, tzStr); ok {
		return t.Unix()
	}

	// 3. "Try again at YYYY-MM-DDTHH:MM:SSZ"（OpenAI ISO 8601）
	if m := regexp.MustCompile(`Try again at (\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?)`).FindStringSubmatch(reason); len(m) >= 2 {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if t, err := time.Parse(layout, m[1]); err == nil {
				return t.Unix()
			}
		}
	}

	// 4. "请于 YYYY-MM-DD HH:MM:SS 后重试" 中文格式
	if m := regexp.MustCompile(`(\d{4}-\d{2}-\d{2})\s+(\d{2}):(\d{2}):(\d{2}).*重试`).FindStringSubmatch(reason); len(m) >= 5 {
		timeStr := fmt.Sprintf("%s %s:%s:%s", m[1], m[2], m[3], m[4])
		if t, ok := parseWithOptionalTz(timeStr, ""); ok {
			return t.Unix()
		}
	}

	return 0
}

// extractDateTimeWithTz 提取 "前缀 (日期时间) [时区]" 中的日期时间和时区
func extractDateTimeWithTz(reason, pattern string) (timeStr string, tzStr string) {
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(reason)
	if len(m) < 2 {
		return "", ""
	}
	// 把日期时间统一成 "2006-01-02 15:04:05" 格式（T 替换为空格）
	timeStr = strings.ReplaceAll(m[1], "T", " ")
	// 尝试从匹配位置后面提取时区
	tzRe := regexp.MustCompile(`\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}\s+(.+?)$`)
	if tzMatch := tzRe.FindStringSubmatch(reason); len(tzMatch) >= 2 {
		rest := strings.TrimSpace(tzMatch[1])
		// 去掉末尾可能的标点
		rest = strings.TrimRight(rest, ".,;。")
		// 只保留第一个 token 作为时区
		if fields := strings.Fields(rest); len(fields) > 0 {
			tzStr = fields[0]
		}
	}
	return timeStr, tzStr
}

// parseWithOptionalTz 解析时间，可选时区
func parseWithOptionalTz(timeStr, tzStr string) (time.Time, bool) {
	if timeStr == "" {
		return time.Time{}, false
	}
	// 尝试的 layout 列表
	baseLayout := "2006-01-02 15:04:05"
	// 带时区
	if tzStr != "" {
		for _, layout := range []string{
			baseLayout + " -0700 MST",
			baseLayout + " -0700",
			baseLayout + " MST",
		} {
			if t, err := time.Parse(layout, timeStr+" "+tzStr); err == nil {
				return t, true
			}
		}
	}
	// 不带时区，默认 Asia/Shanghai
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		if t, err := time.ParseInLocation(baseLayout, timeStr, loc); err == nil {
			return t, true
		}
	}
	// 最后兜底用 UTC
	if t, err := time.Parse(baseLayout, timeStr); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// isLikelyQuotaError 粗略判断错误原因是否疑似额度/限流错误（用于日志排查）
func isLikelyQuotaError(reason string) bool {
	if reason == "" {
		return false
	}
	lower := strings.ToLower(reason)
	for _, kw := range []string{"429", "quota", "exceeded", "rate limit", "throttl", "reset at", "额度", "配额", "限流"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
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

	// 429 限流不自动禁用（额度还在，限流是临时的）
	// 但额度耗尽的 429 仍需禁用（通过关键词匹配 "quota exceeded" 等）
	if err.StatusCode == 429 {
		// 只匹配真正的额度耗尽关键词，不匹配 "rate limit" / "too frequent" 等限流关键词
		quotaKeywords := []string{
			"exceeded your current quota",
			"quota exceeded",
			"insufficient_quota",
			"exceeded your current balance",
			"your credit balance is too low",
			"exceeded the",          // "exceeded the 5-hour usage quota" 等
			"usage quota",           // "usage quota" 通用额度耗尽
			"upgrade your plan",     // "recommend upgrading your plan"
			"waiting for the reset", // "waiting for the reset"
		}
		for _, kw := range quotaKeywords {
			if strings.Contains(lowerMessage, kw) {
				return true
			}
		}
		return false
	}

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
