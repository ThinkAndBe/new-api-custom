package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newClaudeNativeTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()

	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Set(common.RequestIdKey, "claude-native-test")

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "glm-5.3"},
		RelayFormat: types.RelayFormatClaude,
		IsStream:    true,
		DisablePing: true,
	}
	return c, recorder, info
}

// 上游正常发送 message_delta 后 Done 为 true，不再补发终止事件。
func TestHandleStreamFinalResponseNoDuplicateWhenDone(t *testing.T) {
	c, recorder, info := newClaudeNativeTestContext(t)

	claudeInfo := &ClaudeResponseInfo{
		Usage: &dto.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		Done:  true,
	}
	info.ReceivedResponseCount = 3

	HandleStreamFinalResponse(c, info, claudeInfo)

	body := recorder.Body.String()
	require.NotContains(t, body, "message_stop")
}

// 上游发送了部分内容块但直接断流（Done 为 false）时，必须补发 message_delta + message_stop。
func TestHandleStreamFinalResponseSynthesizesTermination(t *testing.T) {
	c, recorder, info := newClaudeNativeTestContext(t)

	// thinking-only 截断：有开着的 thinking 块、无 text 内容 → 补块关闭 +
	// message_delta(max_tokens) + message_stop（max_tokens 引导客户端按截断重试，
	// 而不是把 thinking-only 响应当作可疑空响应）
	claudeInfo := &ClaudeResponseInfo{
		Usage:      &dto.Usage{PromptTokens: 8, CompletionTokens: 0, TotalTokens: 8},
		Done:       false,
		OpenBlocks: map[int]bool{0: true},
	}
	info.ReceivedResponseCount = 3

	HandleStreamFinalResponse(c, info, claudeInfo)

	body := recorder.Body.String()
	require.Contains(t, body, "content_block_stop")
	require.Contains(t, body, "message_delta")
	require.Contains(t, body, `"stop_reason":"max_tokens"`)
	require.Contains(t, body, "message_stop")

	// 有 text 内容的正常断流 → end_turn
	c2, recorder2, info2 := newClaudeNativeTestContext(t)
	claudeInfo2 := &ClaudeResponseInfo{
		Usage:          &dto.Usage{PromptTokens: 8, CompletionTokens: 5, TotalTokens: 13},
		Done:           false,
		HasTextContent: true,
	}
	claudeInfo2.ResponseText.WriteString("hi")
	info2.ReceivedResponseCount = 3
	HandleStreamFinalResponse(c2, info2, claudeInfo2)
	body2 := recorder2.Body.String()
	require.Contains(t, body2, `"stop_reason":"end_turn"`)
	require.Contains(t, body2, "message_stop")
}

// 上游一个事件都没发（ReceivedResponseCount 为 0）时不补发，保持空响应语义。
func TestHandleStreamFinalResponseNoEventsNoSynthesis(t *testing.T) {
	c, recorder, info := newClaudeNativeTestContext(t)

	claudeInfo := &ClaudeResponseInfo{
		Usage: &dto.Usage{},
		Done:  false,
	}
	info.ReceivedResponseCount = 0

	HandleStreamFinalResponse(c, info, claudeInfo)

	body := recorder.Body.String()
	require.NotContains(t, body, "message_stop")
}
