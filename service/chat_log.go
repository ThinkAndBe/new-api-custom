package service

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// ExtractRequestContent 从 relayInfo.Request 中提取纯文本内容
// 支持 OpenAI 和 Claude 格式，跳过图片/文件等二进制数据
// 根据配置项 common.ChatLogLogRoles 过滤角色，common.ChatLogContentMaxLen 截断长度
// 返回格式：[system] xxx\n[user] xxx\n[assistant] xxx
func ExtractRequestContent(req dto.Request) string {
	if req == nil {
		return ""
	}

	allowedRoles := parseAllowedRoles(common.ChatLogLogRoles)
	if len(allowedRoles) == 0 {
		return ""
	}

	switch r := req.(type) {
	case *dto.GeneralOpenAIRequest:
		return extractOpenAIContent(r, allowedRoles)
	case *dto.ClaudeRequest:
		return extractClaudeContent(r, allowedRoles)
	default:
		return ""
	}
}

// parseAllowedRoles 解析角色配置字符串，返回角色集合
// 配置为逗号分隔的角色名（如 "system,user,assistant"），空或无效时返回 nil（不记录任何内容）
func parseAllowedRoles(rolesConfig string) map[string]bool {
	rolesConfig = strings.TrimSpace(rolesConfig)
	if rolesConfig == "" {
		return nil
	}
	allowed := make(map[string]bool)
	for _, r := range strings.Split(rolesConfig, ",") {
		r = strings.TrimSpace(strings.ToLower(r))
		if r != "" {
			allowed[r] = true
		}
	}
	return allowed
}

// truncateText 按配置截断文本，maxLen <= 0 时不截断
func truncateText(text string) string {
	maxLen := common.ChatLogContentMaxLen
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "...[truncated]"
}

// stripInjectedContent 剥离系统/工具注入的提示词，只保留用户真实输入内容
// 只返回最后一条用户输入（去掉所有历史对话和系统注入）
func stripInjectedContent(text string) string {
	// 移除 <system-reminder>...</system-reminder>
	text = removeXMLTag(text, "system-reminder")
	// 移除 <environment>...</environment>
	text = removeXMLTag(text, "environment")
	// 移除 <tool_call>...</tool_call>
	text = removeXMLTag(text, "tool_call")
	// 移除 <function_results>...</function_results>
	text = removeXMLTag(text, "function_results")
	// 移除 <antThinking>...</antThinking>
	text = removeXMLTag(text, "antThinking")

	// 按行分割，去掉空行
	lines := strings.Split(text, "\n")
	var nonEmptyLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			nonEmptyLines = append(nonEmptyLines, trimmed)
		}
	}
	if len(nonEmptyLines) == 0 {
		return ""
	}

	// 取最后一个非空行
	result := nonEmptyLines[len(nonEmptyLines)-1]

	// 过滤掉媒体占位符等非用户输入内容
	mediaMarkers := []string{
		"[Media omitted from provider request",
		"[Attached image/",
		"[Attached file/",
	}
	for _, marker := range mediaMarkers {
		if strings.HasPrefix(result, marker) {
			// 如果最后一条是媒体占位符，取倒数第二条
			if len(nonEmptyLines) >= 2 {
				result = nonEmptyLines[len(nonEmptyLines)-2]
			} else {
				return ""
			}
		}
	}

	return result
}

// removeXMLTag 移除指定 XML 标签及其内容（不区分大小写）
func removeXMLTag(text, tagName string) string {
	lower := strings.ToLower(text)
	result := text
	for {
		lower = strings.ToLower(result)
		startTag := "<" + tagName
		endTag := "</" + tagName + ">"
		startIdx := strings.Index(lower, startTag)
		if startIdx < 0 {
			break
		}
		endIdx := strings.Index(lower[startIdx:], endTag)
		if endIdx < 0 {
			// 没有结束标签，移除从开始标签到末尾
			result = result[:startIdx]
			break
		}
		endIdx += startIdx + len(endTag)
		result = result[:startIdx] + result[endIdx:]
	}
	return strings.TrimSpace(result)
}

// extractOpenAIContent 从 OpenAI 格式请求中提取文本
// 只提取最后一条 user message 的纯文本内容，剥离所有注入的系统提示词
func extractOpenAIContent(req *dto.GeneralOpenAIRequest, allowedRoles map[string]bool) string {
	if req == nil || len(req.Messages) == 0 {
		return ""
	}

	// 找到最后一条 user 消息
	var lastUserText string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		msg := &req.Messages[i]
		if strings.ToLower(msg.Role) != "user" {
			continue
		}
		contents := msg.ParseContent()
		var textParts []string
		for _, mc := range contents {
			if mc.Type == dto.ContentTypeText && mc.Text != "" {
				textParts = append(textParts, mc.Text)
			}
		}
		if len(textParts) > 0 {
			lastUserText = strings.Join(textParts, "\n")
			break
		}
	}

	// 剥离所有注入内容，只保留用户真实输入
	cleaned := stripInjectedContent(lastUserText)
	if cleaned == "" {
		return ""
	}
	cleaned = truncateText(cleaned)
	return fmt.Sprintf("[user] %s", cleaned)
}

// extractClaudeContent 从 Claude 格式请求中提取文本
// 只提取最后一条 user message 的纯文本内容，剥离所有注入的系统提示词
func extractClaudeContent(req *dto.ClaudeRequest, allowedRoles map[string]bool) string {
	if req == nil {
		return ""
	}

	// 找到最后一条 user 消息
	var lastUserText string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		msg := &req.Messages[i]
		if strings.ToLower(msg.Role) != "user" {
			continue
		}
		text := claudeMessageToString(msg)
		if text != "" {
			lastUserText = text
			break
		}
	}

	// Prompt（旧格式，视为 user）
	if lastUserText == "" && req.Prompt != "" {
		lastUserText = req.Prompt
	}

	// 剥离所有注入内容
	cleaned := stripInjectedContent(lastUserText)
	if cleaned == "" {
		return ""
	}
	cleaned = truncateText(cleaned)
	return fmt.Sprintf("[user] %s", cleaned)
}

// claudeSystemToString 将 Claude system 字段转为字符串
func claudeSystemToString(system any) string {
	switch v := system.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprintf("%v", v)
	}
}

// claudeMessageToString 将 Claude 消息内容转为纯文本
func claudeMessageToString(msg *dto.ClaudeMessage) string {
	if msg == nil {
		return ""
	}
	switch v := msg.Content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// RecordChatLog 记录对话日志
func RecordChatLog(info *relaycommon.RelayInfo) {
	if info == nil || info.Request == nil {
		return
	}
	content := ExtractRequestContent(info.Request)
	if content == "" {
		return
	}
	model.RecordChatLog(info, content)
}
