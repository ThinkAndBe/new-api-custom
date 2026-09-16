package main

// scanner.go — 本机 AI 工具项目/技能扫描 → 影子代码库。
//
// 扫描目标（按工具）：
//   WorkBuddy  项目 ~/WorkBuddy/*（显示名取 ~/.workbuddy/workspace-display-names.json）
//              技能 ~/.workbuddy/skills/*、安装目录 skills/
//   CodeBuddy  技能 ~/.codebuddy/skills/*、安装目录 skills/
//   ZCode      工作区 ~/.zcode/workspace/*
//   Codex      ~/.codex/AGENTS.md 等根级文档
//
// 上传：每个项目打 zip → POST /api/usage/shadow_upload?project=<名>
// （复用既有令牌鉴权端点；zip 内路径避开影子库排除目录名）。
// 安装目录经注册表 Uninstall 键发现，兼容 D 盘等非默认安装。

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

const (
	scanMaxFilesPerProject = 300        // 单项目文件数上限
	scanMaxFileBytes       = 2 << 20    // 2MB（与影子库单文件上限一致）
	scanMaxTotalProjects   = 100         // 单次上传项目数上限
)

// scanTextExt 文本文件扩展名白名单
var scanTextExt = map[string]bool{
	".go": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
	".py": true, ".java": true, ".kt": true, ".md": true, ".txt": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true,
	".c": true, ".h": true, ".cpp": true, ".hpp": true, ".cs": true,
	".rs": true, ".rb": true, ".php": true, ".sh": true, ".bat": true,
	".ps1": true, ".sql": true, ".html": true, ".css": true, ".scss": true,
	".vue": true, ".swift": true, ".m": true, ".lua": true, ".env": true,
}

var scanSkipDirs = map[string]bool{
	"node_modules": true, ".git": true, "__pycache__": true, ".venv": true,
	"venv": true, "dist": true, "build": true, ".cache": true, "target": true,
}

type scanProject struct {
	Name string // 影子库项目名
	Root string // 本地根目录
}

// discoverInstallDir 注册表 Uninstall 键里按显示名找安装目录
func discoverInstallDir(nameKeyword string) string {
	roots := []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER}
	paths := []string{
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
		`SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
	}
	for _, rk := range roots {
		for _, p := range paths {
			k, err := registry.OpenKey(rk, p, registry.READ)
			if err != nil {
				continue
			}
			subKeys, _ := k.ReadSubKeyNames(-1)
			for _, sk := range subKeys {
				uk, err := registry.OpenKey(rk, p+`\`+sk, registry.READ)
				if err != nil {
					continue
				}
				dn, _, err := uk.GetStringValue("DisplayName")
				loc, _, _ := uk.GetStringValue("InstallLocation")
				uk.Close()
				if err == nil && loc != "" && strings.Contains(strings.ToLower(dn), strings.ToLower(nameKeyword)) {
					return loc
				}
			}
			k.Close()
		}
	}
	return ""
}

// workbuddyDisplayNames 解析 WorkBuddy 工作区显示名映射
func workbuddyDisplayNames() map[string]string {
	out := map[string]string{}
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".workbuddy", "workspace-display-names.json"))
	if err != nil {
		return out
	}
	var parsed struct {
		Workspaces map[string]struct {
			Path        string `json:"path"`
			DisplayName string `json:"displayName"`
		} `json:"workspaces"`
	}
	if json.Unmarshal(data, &parsed) != nil {
		return out
	}
	for _, w := range parsed.Workspaces {
		if w.DisplayName != "" {
			out[strings.ToLower(w.Path)] = w.DisplayName
		}
	}
	return out
}

// collectScanProjects 汇总本机可扫描的项目/技能
func collectScanProjects() []scanProject {
	home, _ := os.UserHomeDir()
	var out []scanProject

	// WorkBuddy 项目：~/WorkBuddy/<时间戳>
	wbProjRoot := filepath.Join(home, "WorkBuddy")
	names := workbuddyDisplayNames()
	if entries, err := os.ReadDir(wbProjRoot); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if display, ok := names[strings.ToLower(filepath.Join(wbProjRoot, name))]; ok {
				name = display
			}
			out = append(out, scanProject{Name: "WB项目-" + sanitizeProjectName(name), Root: filepath.Join(wbProjRoot, e.Name())})
		}
	}

	// 各工具技能目录（home + 安装目录）
	skillRoots := []struct{ root, prefix string }{
		{filepath.Join(home, ".workbuddy", "skills"), "WB技能"},
		{filepath.Join(home, ".codebuddy", "skills"), "CB技能"},
	}
	if dir := discoverInstallDir("workbuddy"); dir != "" {
		skillRoots = append(skillRoots, struct{ root, prefix string }{filepath.Join(dir, "skills"), "WB技能(安装)"})
	}
	if dir := discoverInstallDir("codebuddy"); dir != "" {
		skillRoots = append(skillRoots, struct{ root, prefix string }{filepath.Join(dir, "skills"), "CB技能(安装)"})
	}
	for _, sr := range skillRoots {
		entries, err := os.ReadDir(sr.root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				out = append(out, scanProject{Name: sr.prefix + "-" + e.Name(), Root: filepath.Join(sr.root, e.Name())})
			}
		}
	}

	// ZCode 工作区
	if entries, err := os.ReadDir(filepath.Join(home, ".zcode", "workspace")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				out = append(out, scanProject{Name: "ZC工作区-" + e.Name(), Root: filepath.Join(home, ".zcode", "workspace", e.Name())})
			}
		}
	}

	if len(out) > scanMaxTotalProjects {
		out = out[:scanMaxTotalProjects]
	}
	return out
}

var projectSanitizer = regexp.MustCompile(`[\\/:*?"<>|\r\n]+`)

func sanitizeProjectName(s string) string {
	s = strings.TrimSpace(s)
	s = projectSanitizer.ReplaceAllString(s, "_")
	if len(s) > 48 {
		s = s[:48]
	}
	if s == "" {
		s = "unnamed"
	}
	return s
}

// looksTextual 前 4KB 含 NUL 判为二进制
func looksTextual(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	return !bytes.ContainsRune(buf[:n], 0)
}

// buildProjectZip 收集项目内文本文件打 zip（zip 内为项目相对路径，
// 路径不出现 workbuddy/codebuddy 等影子库排除目录名）
func buildProjectZip(root string) ([]byte, int, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	count := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if scanSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if count >= scanMaxFilesPerProject {
			return filepath.SkipAll
		}
		if !scanTextExt[strings.ToLower(filepath.Ext(d.Name()))] {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > scanMaxFileBytes || info.Size() == 0 {
			return nil
		}
		if !looksTextual(path) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) > scanMaxFileBytes {
			return nil
		}
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return nil
		}
		if _, err := w.Write(data); err != nil {
			return nil
		}
		count++
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		zw.Close()
		return nil, 0, err
	}
	zw.Close()
	if count == 0 {
		return nil, 0, nil
	}
	return buf.Bytes(), count, nil
}

// uploadProject 上传单个项目 zip（multipart/file 字段，端点约定）。
// 注意端点业务失败也可能返回 HTTP 200，必须解析 success 字段。
func uploadProject(server, apiKey, project string, zipData []byte) error {
	url := strings.TrimSuffix(server, "/") + "/api/usage/shadow_upload?project=" + sanitizeProjectName(project)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "project.zip")
	if err != nil {
		return err
	}
	if _, err := fw.Write(zipData); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	req, err := http.NewRequest("POST", url, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var parsed struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Files   int    `json:"files"`
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if json.Unmarshal(respBody, &parsed) == nil && !parsed.Success {
		return fmt.Errorf("%s", parsed.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// runScanAndUpload 托盘「扫描并上传」入口
func (k *keeper) runScanAndUpload() {
	apiKey := cachedAPIKey()
	if apiKey == "" {
		k.log("尚未配置过（没有可用密钥），请先用配置码完成一键配置")
		return
	}
	server := resolveServer()
	if server == "" {
		k.log("未配置服务器地址")
		return
	}
	projects := collectScanProjects()
	if len(projects) == 0 {
		k.log("未发现可扫描的项目/技能")
		return
	}
	k.log("开始扫描上传：发现 %d 个项目/技能", len(projects))
	okCount, skipCount := 0, 0
	for _, p := range projects {
		data, n, err := buildProjectZip(p.Root)
		if err != nil {
			skipCount++
			continue
		}
		if n == 0 {
			skipCount++
			continue
		}
		if err := uploadProject(server, apiKey, p.Name, data); err != nil {
			k.log("上传 %s 失败: %v", p.Name, err)
			skipCount++
			continue
		}
		okCount++
	}
	k.log("扫描上传完成：成功 %d，跳过/失败 %d", okCount, skipCount)
}

// cachedAPIKey 从守护缓存取密钥（模型条目里的 apiKey）
func cachedAPIKey() string {
	for _, product := range []string{"workbuddy", "codebuddy"} {
		if cfg := loadCache(product); cfg != nil && len(cfg.Models) > 0 {
			for _, m := range cfg.Models {
				if m.APIKey != "" {
					return m.APIKey
				}
			}
		}
	}
	return ""
}
