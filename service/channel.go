package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func formatNotifyType(channelId int, status int) string {
	return fmt.Sprintf("%s_%d_%d", dto.NotifyTypeChannelUpdate, channelId, status)
}

// disable & notify
func DisableChannel(channelError types.ChannelError, reason string) {
	common.SysLog(fmt.Sprintf("通道「%s」（#%d）发生错误，准备禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, common.LocalLogPreview(reason)))

	// 检查是否启用自动禁用功能
	// 额度耗尽类错误（quota exhausted）无视 auto_ban 设置，必须禁用
	if !channelError.AutoBan {
		if isQuotaExhaustedReason(reason) {
			common.SysLog(fmt.Sprintf("通道「%s」（#%d）未启用自动禁用，但检测到额度耗尽错误，强制禁用", channelError.ChannelName, channelError.ChannelId))
		} else {
			common.SysLog(fmt.Sprintf("通道「%s」（#%d）未启用自动禁用功能，跳过禁用操作", channelError.ChannelName, channelError.ChannelId))
			return
		}
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

	// 2b. 中文格式 "YYYY-MM-DD HH:MM:SS 后可继续使用"
	timeStr2, _ := extractDateTimeWithTz(reason, `(\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2})`)
	if t, ok := parseWithOptionalTz(timeStr2, ""); ok {
		return t.Unix()
	}

	// 2c. "reset at MM-DD HH:MM:SS UTC" 格式（阿里云 DashScope，无年份）
	// 补充当前年份
	mmddRe := regexp.MustCompile(`reset at (\d{2})-(\d{2}) (\d{2}):(\d{2}):(\d{2})\s*(\w+)?`)
	if mm := mmddRe.FindStringSubmatch(reason); len(mm) >= 6 {
		year := now.Year()
		timeStr := fmt.Sprintf("%d-%s-%s %s:%s:%s", year, mm[1], mm[2], mm[3], mm[4], mm[5])
		tzStr := ""
		if len(mm) >= 7 && mm[6] != "" {
			tzStr = mm[6]
		}
		if t, ok := parseWithOptionalTz(timeStr, tzStr); ok {
			// 如果解析出的时间已经过去超过 7 天，可能是去年（跨年）
			if t.Before(now.AddDate(0, 0, -7)) {
				t = t.AddDate(1, 0, 0)
			}
			return t.Unix()
		}
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

// quotaExhaustedKeywords 额度耗尽的错误关键词（英文+中文）
// 用于检测 429 状态码下是"额度耗尽"还是"临时限流"
// 统一列表，ShouldDisableChannel 和 IsQuotaExhaustedError 共用
var quotaExhaustedKeywords = []string{
	// 英文 - OpenAI/通用
	"exceeded your current quota",
	"quota exceeded",
	"insufficient_quota",
	"exceeded your current balance",
	"your credit balance is too low",
	"exceeded the",
	"usage quota",
	"usage limit",
	"upgrade your plan",
	"waiting for the reset",
	// 英文 - 阿里云 DashScope
	"out of available credits",
	"insufficient balance",
	"account balance is not enough",
	"balance is not enough",
	"no enough balance",
	// 英文 - Kimi / Moonshot
	"does not have enough balance",
	"not enough balance",
	// 英文 - 通用余额/额度
	"insufficient credit",
	"out of credit",
	"credit balance",
	"billing limit",
	"spending limit",
	"billing_hard_limit_reached",
	// 中文
	"使用上限",
	"使用限制",
	"额度用尽",
	"额度耗尽",
	"额度不足",
	"配额用尽",
	"配额不足",
	"余额不足",
	"余额耗尽",
	"余额用尽",
	"后可继续使用",
	"请充值",
}

// isQuotaExhaustedByKeywords 通过关键词判断是否为额度耗尽
func isQuotaExhaustedByKeywords(lowerMessage string) bool {
	for _, kw := range quotaExhaustedKeywords {
		if strings.Contains(lowerMessage, kw) {
			return true
		}
	}
	return false
}

// IsQuotaExhaustedError 判断错误是否为额度耗尽类错误（非临时限流）
// 用于在重试循环结束后增强错误信息（返回可用模型和恢复时间）
// 不限制状态码：不同上游可能用 429/403/400 等不同状态码返回额度耗尽
// 通过关键词精确匹配区分额度耗尽和临时限流
func IsQuotaExhaustedError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	return isQuotaExhaustedByKeywords(strings.ToLower(err.Error()))
}

// isQuotaExhaustedReason 从错误原因字符串判断是否为额度耗尽
// 用于 DisableChannel 中，决定是否无视 auto_ban 强制禁用
func isQuotaExhaustedReason(reason string) bool {
	if reason == "" {
		return false
	}
	return isQuotaExhaustedByKeywords(strings.ToLower(reason))
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

	// 额度耗尽关键词匹配（不限状态码，不同上游可能用 429/403/400 等）
	// 关键词本身是确定性的额度信号，不会出现在临时限流中
	if isQuotaExhaustedByKeywords(lowerMessage) {
		return true
	}

	// 429/403 的非额度类错误（如临时限流、权限问题）不自动禁用
	if err.StatusCode == 429 || err.StatusCode == 403 {
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

// ChannelUnavailableInfo 渠道不可用时的详细信息
type ChannelUnavailableInfo struct {
	AvailableModels     []string
	RecoveryAt          int64
	RecoveryAtReadable  string
	RecoveryTimeHint    string // 用于嵌入错误消息的恢复时间提示（如 "，预计恢复时间：2026-07-23 18:00:00"）
	AvailableModelsHint string // 用于嵌入错误消息的可用模型列表（如 "gpt-4o, claude-3.5-sonnet"）
}

// isChineseContext 通过 Accept-Language 头判断请求是否为中文环境
func isChineseContext(c *gin.Context) bool {
	lang := c.GetHeader("Accept-Language")
	return strings.Contains(lang, "zh")
}

// BuildChannelUnavailableInfo 构建渠道不可用时的详细信息（可用模型 + 恢复时间）
// 用于在返回"渠道临时不可用"错误时，同时告诉用户能用什么模型以及什么时候能恢复
func BuildChannelUnavailableInfo(c *gin.Context, usingGroup, modelName string) ChannelUnavailableInfo {
	info := ChannelUnavailableInfo{}

	// 1. 获取用户可用模型列表
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	tokenGroup := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
	var groupsToCheck []string
	if usingGroup == "auto" {
		if userGroup == "" {
			userGroup, _ = model.GetUserGroup(c.GetInt("id"), false)
		}
		groupsToCheck = GetUserAutoGroup(userGroup)
		_ = tokenGroup // tokenGroup is not needed here
	} else {
		groupsToCheck = []string{usingGroup}
	}

	var availableModels []string
	for _, g := range groupsToCheck {
		groupModels := model.GetGroupEnabledModels(g)
		for _, m := range groupModels {
			if m == modelName {
				continue // 排除当前请求的（不可用的）模型
			}
			if !common.StringsContains(availableModels, m) {
				availableModels = append(availableModels, m)
			}
		}
	}
	// 限制最多 20 个，避免消息过长
	if len(availableModels) > 20 {
		availableModels = availableModels[:20]
	}
	info.AvailableModels = availableModels
	info.AvailableModelsHint = strings.Join(availableModels, ", ")

	// 2. 获取被禁用渠道的恢复时间（取最早的恢复时间）
	var earliestRecovery int64
	for _, g := range groupsToCheck {
		disabledIds := model.GetDisabledChannelIds(g, modelName)
		for _, cid := range disabledIds {
			ch, err := model.GetChannelById(cid, true)
			if err != nil || ch == nil {
				continue
			}
			otherInfo := ch.GetOtherInfo()
			if recoveryAt, ok := otherInfo["recovery_at"]; ok {
				var ts int64
				switch v := recoveryAt.(type) {
				case float64:
					ts = int64(v)
				case int64:
					ts = v
				case json.Number:
					ts, _ = v.Int64()
				}
				if ts > 0 {
					if earliestRecovery == 0 || ts < earliestRecovery {
						earliestRecovery = ts
					}
				}
			}
		}
	}

	if earliestRecovery > 0 {
		info.RecoveryAt = earliestRecovery
		info.RecoveryAtReadable = time.Unix(earliestRecovery, 0).Format("2006-01-02 15:04:05")
		// 根据请求语言格式化恢复时间提示
		if isChineseContext(c) {
			info.RecoveryTimeHint = fmt.Sprintf("，预计恢复时间：%s", info.RecoveryAtReadable)
		} else {
			info.RecoveryTimeHint = fmt.Sprintf(", estimated recovery: %s", info.RecoveryAtReadable)
		}
	}

	return info
}
