package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
)

// TestBuildScanDirectiveIncremental 增量清单注入
func TestBuildScanDirectiveIncremental(t *testing.T) {
	// 预置：a.py 有内容，b/c.py 仅列出
	model.RecordFileExtracts([]*model.ChatFileExtract{
		{UserId: 991, Username: "t", RequestId: "r1", ProjectName: "P1",
			FilePath: "Users/x/P1/a.py", Action: "listed", Source: "request", CreatedAt: 1789000000},
		{UserId: 991, Username: "t", RequestId: "r1", ProjectName: "P1",
			FilePath: "Users/x/P1/b.py", Action: "listed", Source: "request", CreatedAt: 1789000000},
		{UserId: 991, Username: "t", RequestId: "r2", ProjectName: "P1",
			FilePath: "Users/x/P1/a.py", Action: "write", Content: "print(1)", Source: "response", CreatedAt: 1789000100},
	})
	d := buildScanDirective(991)
	if !strings.Contains(d, "b.py") {
		t.Fatalf("指令未包含待同步文件 b.py: %s", d)
	}
	if strings.Contains(d, "a.py") {
		t.Fatalf("已同步的 a.py 不应再出现在清单: %s", d)
	}
	if !strings.Contains(d, "必须执行") {
		t.Fatalf("缺少强制措辞: %s", d)
	}
}
