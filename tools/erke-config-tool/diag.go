package main

// diag.go — 一键问题诊断：收集 WorkBuddy 版本/models.json 状态/守护缓存/
// 日志关键行，打包上报到影子库（项目名 WB诊断-主机名），管理员在
// 「文件库」里直接查看，免来回传文件。

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

type diagReport struct {
	ToolVersion string            `json:"tool_version"`
	Hostname    string            `json:"hostname"`
	Username    string            `json:"username"`
	CreatedAt   string            `json:"created_at"`
	Products    []diagProduct     `json:"products"`
	WBVersion   string            `json:"wb_version"`
	LogExcerpt  []string          `json:"log_excerpt"`
	Notes       []string          `json:"notes"`
}

type diagProduct struct {
	Product       string   `json:"product"`
	ProcessAlive  bool     `json:"process_alive"`
	ModelsFile    string   `json:"models_file"`
	ModelIDs      []string `json:"model_ids"`
	CacheIDs      []string `json:"cache_ids"`
}

func buildDiag() *diagReport {
	r := &diagReport{
		ToolVersion: version,
		CreatedAt:   time.Now().Format("2006-01-02 15:04:05"),
	}
	if h, err := os.Hostname(); err == nil {
		r.Hostname = h
	}
	if u, err := user.Current(); err == nil {
		r.Username = u.Username
	}
	home, _ := os.UserHomeDir()

	for _, product := range []string{"workbuddy", "codebuddy"} {
		dp := diagProduct{Product: product}
		if proc := productProcessName(product); proc != "" {
			dp.ProcessAlive = isProcessRunning(proc)
		}
		dirName := ".workbuddy"
		if product == "codebuddy" {
			dirName = ".codebuddy"
		}
		p := filepath.Join(home, dirName, "models.json")
		if data, err := os.ReadFile(p); err != nil {
			dp.ModelsFile = "读取失败: " + err.Error()
		} else {
			var cfg usageConfig
			if err := json.Unmarshal(data, &cfg); err != nil {
				dp.ModelsFile = fmt.Sprintf("解析失败(%d bytes): %v", len(data), err)
			} else {
				dp.ModelsFile = fmt.Sprintf("存在，%d 个模型，%d bytes", len(cfg.Models), len(data))
				for _, m := range cfg.Models {
					dp.ModelIDs = append(dp.ModelIDs, m.Id+" @ "+m.URL)
				}
			}
		}
		if cache := loadCache(product); cache != nil {
			for _, m := range cache.Models {
				dp.CacheIDs = append(dp.CacheIDs, m.Id+" @ "+m.URL)
			}
		}
		r.Products = append(r.Products, dp)
	}

	// WorkBuddy 版本
	if data, err := os.ReadFile(filepath.Join(home, ".workbuddy", "last-launch.json")); err == nil {
		r.WBVersion = strings.TrimSpace(string(data))
	}

	// 日志关键行：Merge / model / error
	logPath := filepath.Join(os.Getenv("LOCALAPPDATA"), "WorkBuddy", "logs", "main.log")
	if data, err := os.ReadFile(logPath); err == nil {
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		var hits []string
		for i := len(lines) - 1; i >= 0 && len(hits) < 120; i-- {
			l := lines[i]
			if strings.Contains(l, "[Merge]") || strings.Contains(strings.ToLower(l), "models.json") ||
				strings.Contains(strings.ToLower(l), "custommodel") {
				hits = append([]string{l}, hits...)
			}
		}
		r.LogExcerpt = hits
	} else {
		r.Notes = append(r.Notes, "main.log 读取失败: "+err.Error())
	}
	return r
}

// runDiagReport 收集诊断 → zip → 上报影子库
func (k *keeper) runDiagReport() {
	apiKey := cachedAPIKey()
	server := resolveServer()
	if apiKey == "" || server == "" {
		k.log("诊断上报需要先完成一次配置（拿不到密钥）")
		return
	}
	r := buildDiag()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("diag.json")
	dj, _ := json.MarshalIndent(r, "", "  ")
	_, _ = w.Write(dj)
	zw.Close()

	project := "WB诊断-" + r.Hostname
	if err := uploadProject(server, apiKey, project, buf.Bytes()); err != nil {
		k.log("诊断上报失败: %v", err)
		return
	}
	k.log("诊断已上报（影子库项目 %s），请通知管理员查看", project)
}
