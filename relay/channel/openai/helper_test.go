package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newClaudeFormatTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()

	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Set(common.RequestIdKey, "claude-format-test")

	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "glm-5.3"},
		ClaudeConvertInfo:  &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone},
		RelayFormat:        types.RelayFormatClaude,
		ShouldIncludeUsage: false,
		IsStream:           true,
		DisablePing:        true,
	}
	return c, recorder, info
}

// 上游正常发送 finish_reason 块时，HandleFinalResponse 不改变既有行为：
// 最后一个内容块带 finish_reason，转换器自行关闭流。
func TestHandleFinalResponseClaudeNormalFinishChunk(t *testing.T) {
	c, recorder, info := newClaudeFormatTestContext(t)

	// 模拟已发送过 message_start 与内容块
	info.SendResponseCount = 2
	info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeText

	lastData := `{"id":"chatcmpl-1","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`
	HandleFinalResponse(c, info, lastData, "chatcmpl-1", 0, "glm-5.3", "", nil, true)

	body := recorder.Body.String()
	require.Contains(t, body, "message_delta")
	require.Contains(t, body, "message_stop")
	require.Contains(t, body, `"stop_reason":"end_turn"`)
	require.True(t, info.ClaudeConvertInfo.Done)
}

// 部分上游（如 doubao-agent-plan）直接发送 [DONE]，最后一个数据块没有 finish_reason。
// 必须补发 message_delta/message_stop，否则客户端会认为响应为空（empty_model_response）。
func TestHandleFinalResponseClaudeMissingFinishReason(t *testing.T) {
	c, recorder, info := newClaudeFormatTestContext(t)

	info.SendResponseCount = 2
	info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeText

	lastData := `{"id":"chatcmpl-1","choices":[{"index":0,"delta":{"content":"ok"}}]}`
	HandleFinalResponse(c, info, lastData, "chatcmpl-1", 0, "glm-5.3", "", nil, true)

	body := recorder.Body.String()
	require.Contains(t, body, "content_block_stop")
	require.Contains(t, body, "message_delta")
	require.Contains(t, body, "message_stop")
	require.Contains(t, body, `"stop_reason":"end_turn"`)
	require.True(t, info.ClaudeConvertInfo.Done)
}

// 上游没有返回任何数据块时，也要输出合法的 message_start -> message_delta -> message_stop。
func TestHandleFinalResponseClaudeEmptyStream(t *testing.T) {
	c, recorder, info := newClaudeFormatTestContext(t)

	usage := &dto.Usage{PromptTokens: 5, CompletionTokens: 0, TotalTokens: 5}
	HandleFinalResponse(c, info, "", "chatcmpl-1", 0, "glm-5.3", "", usage, false)

	body := recorder.Body.String()
	require.Contains(t, body, "message_start")
	require.Contains(t, body, "message_delta")
	require.Contains(t, body, "message_stop")
	require.True(t, info.ClaudeConvertInfo.Done)
}

// 上游先给 finish_reason 块、后跟 usage-only 块的场景（延迟关闭）仍然正常工作，
// 不会被合成逻辑重复追加终止事件。
func TestHandleFinalResponseClaudeDeferredUsageClose(t *testing.T) {
	c, recorder, info := newClaudeFormatTestContext(t)

	info.SendResponseCount = 2
	info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeText
	info.ClaudeConvertInfo.Usage = &dto.Usage{PromptTokens: 10, CompletionTokens: 3, TotalTokens: 13}
	// 模拟中途收到了 finish_reason 但没有 usage 的块，转换器已记录 FinishReason 并延迟关闭
	info.ClaudeConvertInfo.FinishReason = "stop"

	lastData := `{"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`
	HandleFinalResponse(c, info, lastData, "chatcmpl-1", 0, "glm-5.3", "", nil, true)

	body := recorder.Body.String()
	require.Contains(t, body, "message_delta")
	require.Equal(t, 1, strings.Count(body, `"type":"message_stop"`))
	require.True(t, info.ClaudeConvertInfo.Done)
}
