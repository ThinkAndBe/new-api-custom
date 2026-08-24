//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 端到端逻辑验证：裸码 → fetchAndBuild → writeModelsFile
// 前置：本机 new-api (127.0.0.1:3000) 运行中，测试前手动生成一个短码写入环境变量 GUIDE_CODE
func TestFetchAndWrite(t *testing.T) {
	code := os.Getenv("GUIDE_CODE")
	if code == "" {
		t.Skip("GUIDE_CODE not set; skip e2e")
	}
	cfg, err := fetchAndBuild(code, "workbuddy")
	if err != nil {
		t.Fatalf("fetchAndBuild: %v", err)
	}
	if len(cfg.Models) == 0 {
		t.Fatal("no models")
	}
	path, err := writeModelsFile("workbuddy", cfg)
	if err != nil {
		t.Fatalf("writeModelsFile: %v", err)
	}
	t.Logf("written: %s models=%d", path, len(cfg.Models))
	_ = filepath.Join
}
