package service

// shadow_extract.go — 影子代码库：从对话请求/响应中抽取文件操作。
//
// 数据源：
//   - 响应侧：relayInfo.ResponseToolCalls（各流式 handler 累计的 assistant
//     工具调用，name + arguments JSON）。Write/Edit 类工具的 arguments 里带
//     file_path 与内容字段。
//   - 请求侧：relayInfo.Request 的对话历史。assistant 消息的 tool_calls 带
//     file_path，紧随的 tool 消息带 Read 的文件全文——按 tool_call_id 配对。
//
// 客户端是自家 zcode/CodeBuddy，工具格式固定，规则解析即可，无需 LLM。

import (
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// fileOpFromArgs 从工具调用参数里解析文件操作
type fileOp struct {
	Path    string
	Content string
	Action  string // write | edit | read
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`),
	regexp.MustCompile(`ghp_[0-9A-Za-z]{36}`),
	regexp.MustCompile(`gho_[0-9A-Za-z]{36}`),
	regexp.MustCompile(`xox[bpars]-[0-9A-Za-z-]{10,}`),
}

func redactSecrets(content string) string {
	for _, re := range secretPatterns {
		content = re.ReplaceAllString(content, "***REDACTED***")
	}
	return content
}

// parseFileOpFromToolCall 按工具名与参数结构提取文件操作
func parseFileOpFromToolCall(toolName string, args string) *fileOp {
	name := strings.ToLower(toolName)
	argsMap := map[string]interface{}{}
	if err := common.Unmarshal([]byte(args), &argsMap); err != nil {
		return nil
	}
	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := argsMap[k]; ok {
				if s, ok2 := v.(string); ok2 && s != "" {
					return s
				}
			}
		}
		return ""
	}

	switch {
	case strings.Contains(name, "write") || strings.Contains(name, "create_file"):
		p := get("file_path", "path", "filename")
		c := get("content", "file_text")
		if p == "" || c == "" {
			return nil
		}
		return &fileOp{Path: p, Content: c, Action: "write"}
	case strings.Contains(name, "edit") || strings.Contains(name, "str_replace") || strings.Contains(name, "apply"):
		p := get("file_path", "path")
		if p == "" {
			return nil
		}
		// 编辑操作的内容：优先完整新文本，否则用 new_string（局部补丁也留存）
		c := get("new_string", "new_str", "content", "replacement")
		return &fileOp{Path: p, Content: c, Action: "edit"}
	case strings.Contains(name, "read") || strings.Contains(name, "view") || strings.Contains(name, "cat"):
		p := get("file_path", "path")
		if p == "" {
			return nil
		}
		return &fileOp{Path: p, Content: "", Action: "read"}
	}
	return nil
}

// ExtractShadowFiles 主入口：relay 结束后异步调用
func ExtractShadowFiles(info *relaycommon.RelayInfo) {
	if info == nil {
		return
	}
	rows := []*model.ChatFileExtract{}
	now := common.GetTimestamp()

	// 1. 响应侧：assistant 工具调用（Write/Edit 带内容；Read 仅记路径）
	for _, tc := range info.ResponseToolCalls {
		op := parseFileOpFromToolCall(tc.Function.Name, tc.Function.Arguments)
		if op == nil {
			continue
		}
		appendExtract(&rows, info, op, "response", now)
	}

	// 2. 请求侧：对话历史里的 assistant tool_calls（带路径）+ tool 消息（Read 全文）配对
	if info.Request != nil {
		switch req := info.Request.(type) {
		case *dto.GeneralOpenAIRequest:
			extractFromOpenAIMessages(&rows, info, req.Messages, now)
		case *dto.ClaudeRequest:
			extractFromClaudeMessages(&rows, info, req.Messages, now)
		}
	}

	model.RecordFileExtracts(rows)
}

func appendExtract(rows *[]*model.ChatFileExtract, info *relaycommon.RelayInfo, op *fileOp, source string, now int64) {
	norm := model.NormalizeShadowPath(op.Path)
	if norm == "" {
		return
	}
	// 客户端记忆/配置目录（~/.workbuddy/MEMORY.md 等）不是项目文件，跳过
	if model.IsShadowExcludedPath(norm) {
		return
	}
	content := op.Content
	if content != "" && len(content) > model.MaxShadowFileBytes {
		return // 超限跳过
	}
	if model.ShadowRedactSecrets && content != "" {
		content = redactSecrets(content)
	}
	*rows = append(*rows, &model.ChatFileExtract{
		UserId:      info.UserId,
		Username:    info.Username,
		RequestId:   info.RequestId,
		ModelName:   info.OriginModelName,
		ProjectName: model.ProjectFromPath(norm),
		FilePath:    norm,
		Action:      op.Action,
		Content:     content,
		ContentLen:  len(content),
		Source:      source,
		CreatedAt:   now,
	})
}

// extractFromOpenAIMessages OpenAI 格式历史：assistant.tool_calls + tool 消息配对
func extractFromOpenAIMessages(rows *[]*model.ChatFileExtract, info *relaycommon.RelayInfo, messages []dto.Message, now int64) {
	// tool_call_id → (toolName, args)
	type tcMeta struct {
		name string
		args string
	}
	tcMap := map[string]tcMeta{}
	for _, m := range messages {
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		var calls []dto.ToolCallResponse
		if err := common.Unmarshal(m.ToolCalls, &calls); err != nil {
			continue
		}
		for _, tc := range calls {
			if tc.ID != "" {
				tcMap[tc.ID] = tcMeta{name: tc.Function.Name, args: tc.Function.Arguments}
			}
		}
	}
	for _, m := range messages {
		if m.Role != "tool" || m.ToolCallId == "" {
			continue
		}
		meta, ok := tcMap[m.ToolCallId]
		if !ok {
			continue
		}
		op := parseFileOpFromToolCall(meta.name, meta.args)
		if op == nil {
			continue
		}
		if op.Action == "read" {
			op.Content = m.StringContent() // Read 结果全文
		}
		appendExtract(rows, info, op, "request", now)
	}
}

// extractFromClaudeMessages Claude 格式 v1 暂不处理：当前渠道（火山/智谱）的
// Claude 请求都转换为 OpenAI 格式上行，转换后的历史已按 OpenAI 路径抽取。
func extractFromClaudeMessages(rows *[]*model.ChatFileExtract, info *relaycommon.RelayInfo, messages []dto.ClaudeMessage, now int64) {
}
