package service

import "testing"

// TestParseShellWrites heredoc 与重定向解析
func TestParseShellWrites(t *testing.T) {
	cmd := "cat > /home/u/proj/app.py <<'EOF'\nprint('hello')\n# done\nEOF\necho done"
	ops := parseShellWrites(cmd)
	if len(ops) != 1 {
		t.Fatalf("heredoc: want 1 op, got %d", len(ops))
	}
	if ops[0].Path != "/home/u/proj/app.py" {
		t.Fatalf("path: %s", ops[0].Path)
	}
	if ops[0].Content != "print('hello')\n# done" {
		t.Fatalf("content: %q", ops[0].Content)
	}

	cmd2 := "echo \"PORT=8080\" > /home/u/proj/.env"
	ops2 := parseShellWrites(cmd2)
	if len(ops2) != 1 || ops2[0].Path != "/home/u/proj/.env" || ops2[0].Content != "PORT=8080" {
		t.Fatalf("redirect: %+v", ops2)
	}
}

// TestParseApplyPatch codex 补丁格式解析
func TestParseApplyPatch(t *testing.T) {
	body := "*** Begin Patch\n*** Add File: src/main.py\n+print('x')\n+\n*** Update File: README.md\n+# new line\n*** End Patch"
	ops := parseApplyPatch(body)
	if len(ops) != 2 {
		t.Fatalf("want 2 ops, got %d", len(ops))
	}
	if ops[0].Path != "src/main.py" || ops[0].Action != "write" || ops[0].Content != "print('x')\n\n" {
		t.Fatalf("op0: %+v", ops[0])
	}
	if ops[1].Path != "README.md" || ops[1].Action != "edit" {
		t.Fatalf("op1: %+v", ops[1])
	}
}

// TestParseToolCallForFiles 入口分发：结构化/shell/patch
func TestParseToolCallForFiles(t *testing.T) {
	// 结构化（zcode/CodeBuddy/WorkBuddy 风格）
	ops := parseToolCallForFiles("write_file", `{"file_path":"C:/x/p/a.py","content":"print(1)"}`)
	if len(ops) != 1 || ops[0].Path != "C:/x/p/a.py" {
		t.Fatalf("structured: %+v", ops)
	}
	// shell 工具带 command（JSON 内 \n 为转义）
	ops2 := parseToolCallForFiles("Bash", `{"command":"cat > /y/q/b.sh <<'MARK'\necho hi\nMARK"}`)
	if len(ops2) != 1 || ops2[0].Path != "/y/q/b.sh" {
		t.Fatalf("shell: %+v", ops2)
	}
	// apply_patch 工具
	ops3 := parseToolCallForFiles("apply_patch", "*** Begin Patch\n*** Add File: c.txt\n+hello\n*** End Patch")
	if len(ops3) != 1 || ops3[0].Path != "c.txt" {
		t.Fatalf("patch: %+v", ops3)
	}
}
