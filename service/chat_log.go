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
	// 未配置角色时默认只记录 user 角色（避免配置缺失导致完全不记录）
	if len(allowedRoles) == 0 {
		allowedRoles = map[string]bool{"user": true}
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

// isMetaRequest 判断是否为客户端自动发送的元请求（非用户真实意图）
// 这类请求由 ChatGPT-Next-Web / LobeChat / Open WebUI 等客户端自动触发，
// 用于生成对话标题、摘要等，对用户行为审计无价值，应跳过不记录。
// 采用精确匹配（区分大小写无关），避免误伤用户真实输入。
func isMetaRequest(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)

	// 自动生成标题类请求（整条消息就是固定的标题生成指令）
	titlePatterns := []string{
		"generate the title",
		"generate a title",
		"generate a short title",
		"generate title",
		"create a title",
		"suggest a title",
		"write a title",
		"give me a title",
		"生成标题",
		"生成一个标题",
		"生成简短标题",
		"起一个标题",
		"总结标题",
		"为这段对话生成标题",
		"为以下对话生成标题",
	}
	for _, p := range titlePatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	// 自动生成摘要类请求
	summaryPatterns := []string{
		"generate a summary of the conversation",
		"summarize the conversation so far",
		"generate conversation summary",
		"总结以上对话",
		"为以上对话生成摘要",
	}
	for _, p := range summaryPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	return false
}

// stripInjectedContent 剥离系统/工具注入的提示词，只保留用户真实输入内容
// 只返回最后一条用户输入（去掉所有历史对话和系统注入）
func stripInjectedContent(text string) string {
	// 移除纯注入标签（标签+内容全部删除，这些标签内都是系统注入不是用户输入）
	text = removeXMLTag(text, "system-reminder")
	text = removeXMLTag(text, "environment")
	text = removeXMLTag(text, "antThinking")

	// 对于可能包含用户真实输入的标签，只剥掉标签壳保留内容
	// 例如 workbuddy 会把用户输入包在 <user_query>用户输入</user_query> 里
	text = unwrapXMLTag(text, "user_query")
	text = unwrapXMLTag(text, "thinking")
	text = unwrapXMLTag(text, "instructions")
	text = unwrapXMLTag(text, "context")
	text = unwrapXMLTag(text, "additional_data")
	text = unwrapXMLTag(text, "additional_info")
	text = unwrapXMLTag(text, "metadata")
	text = unwrapXMLTag(text, "system")

	// 按行分割，去掉空行和残留的标签行（XML 标签未完整匹配时残留）
	lines := strings.Split(text, "\n")
	var nonEmptyLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// 跳过残留的标签行（如 </additional_data>、<context> 等未匹配的标签）
		if strings.HasPrefix(trimmed, "</") || (strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">") && !strings.Contains(trimmed, " ")) {
			continue
		}
		nonEmptyLines = append(nonEmptyLines, trimmed)
	}
	if len(nonEmptyLines) == 0 {
		return ""
	}

	// 保守模式：从后往前找第一条非空内容
	// 只跳过明显的媒体占位符（非用户输入），其他一律保留以避免误伤用户真实输入
	mediaPlaceholders := []string{
		"[Media omitted from provider request",
		"[Attached image/",
		"[Attached file/",
		"[Attached video/",
	}

	result := ""
	for i := len(nonEmptyLines) - 1; i >= 0; i-- {
		line := nonEmptyLines[i]
		skip := false
		// 只跳过明显的媒体占位符
		for _, prefix := range mediaPlaceholders {
			if strings.HasPrefix(line, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			result = line
			break
		}
	}

	// 如果最后一条都是媒体占位符（比如纯图片请求），就保留原内容第一条
	if result == "" {
		result = nonEmptyLines[len(nonEmptyLines)-1]
	}

	return result
}

// removeXMLTag 移除指定 XML 标签及其内容（不区分大小写）
// 用于纯注入标签，如 <system-reminder>...</system-reminder>
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

// unwrapXMLTag 只删除 XML 标签壳，保留标签内的内容（不区分大小写）
// 用于可能包含用户真实输入的标签，如 <user_query>用户输入</user_query>
// 只剥掉 <user_query> 和 </user_query>，保留 "用户输入"
func unwrapXMLTag(text, tagName string) string {
	result := text
	// 先处理带属性的起始标签 <tag ...> 和结束标签 </tag>
	for {
		lower := strings.ToLower(result)
		// 匹配 </tagName> 结束标签
		endTag := "</" + tagName + ">"
		endIdx := strings.Index(lower, endTag)
		if endIdx < 0 {
			break
		}
		result = result[:endIdx] + result[endIdx+len(endTag):]
	}
	for {
		lower := strings.ToLower(result)
		// 匹配 <tagName> 或 <tagName ...> 起始标签
		startTagPrefix := "<" + tagName
		startIdx := strings.Index(lower, startTagPrefix)
		if startIdx < 0 {
			break
		}
		// 找到 > 结束标签头
		gtIdx := strings.Index(result[startIdx:], ">")
		if gtIdx < 0 {
			break
		}
		result = result[:startIdx] + result[startIdx+gtIdx+1:]
	}
	return result
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
	// 去掉 "[user] " 前缀后判断是否为元请求（自动生成标题/摘要等）
	plainContent := strings.TrimPrefix(content, "[user] ")
	if isMetaRequest(plainContent) {
		return
	}
	model.RecordChatLog(info, content)
}
