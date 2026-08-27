package claude

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
)

// TestOpenBlockIndexes 验证块跟踪逻辑
func TestOpenBlockIndexes(t *testing.T) {
	info := &ClaudeResponseInfo{OpenBlocks: map[int]bool{}}
	info.OpenBlocks[5] = true
	info.OpenBlocks[0] = true
	got := info.OpenBlockIndexes()
	if len(got) != 2 || got[0] != 0 || got[1] != 5 {
		t.Fatalf("OpenBlockIndexes = %v, want [0 5]", got)
	}
}

// TestSynthesizeStopOnSilentCut 模拟 thinking-only 静默断流：
// FormatClaudeResponseInfo 跟踪块，最终响应可据此补发
func TestSynthesizeStopOnSilentCut(t *testing.T) {
	info := &ClaudeResponseInfo{}
	// thinking 块 start
	i0 := 0
	start := dto.ClaudeResponse{Type: "content_block_start", Index: &i0,
		ContentBlock: &dto.ClaudeMediaMessage{Type: "thinking"}}
	_ = FormatClaudeResponseInfo(&start, nil, info)
	if !info.OpenBlocks[0] {
		t.Fatal("block 0 should be open")
	}
	if info.HasTextContent {
		t.Fatal("thinking block must not set HasTextContent")
	}
	// 断流：无 message_delta/message_stop → Done=false，合成路径生效
	if info.Done {
		t.Fatal("Done should be false on silent cut")
	}
	// text 块 start
	i1 := 1
	textStart := dto.ClaudeResponse{Type: "content_block_start", Index: &i1,
		ContentBlock: &dto.ClaudeMediaMessage{Type: "text"}}
	_ = FormatClaudeResponseInfo(&textStart, nil, info)
	if !info.HasTextContent {
		t.Fatal("text block must set HasTextContent")
	}
	// stop 后从 open 集合移除
	stop := dto.ClaudeResponse{Type: "content_block_stop", Index: &i1}
	_ = FormatClaudeResponseInfo(&stop, nil, info)
	if info.OpenBlocks[1] {
		t.Fatal("block 1 should be closed")
	}
	if !info.OpenBlocks[0] {
		t.Fatal("block 0 should still be open")
	}
}
