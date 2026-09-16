package main

// inventory.go — 清单模式：扫描上报项目/技能清单（不含文件内容），
// 轮询服务端拉取任务并按需上传。
//
// 技能收录规则：仅个人开发技能（目录无 _skillhub_meta.json 的为市场/
// 公共技能，不收；软件安装目录自带技能不扫）。skill 用途自动取
// SKILL.md 开头描述；项目用途可在管理端手动编辑。

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type invItem struct {
	Kind       string `json:"kind"`
	Tool       string `json:"tool"`
	Name       string `json:"name"`
	LocalPath  string `json:"local_path"`
	FileCount  int    `json:"file_count"`
	TotalBytes int64  `json:"total_bytes"`
	Purpose    string `json:"purpose"`
}

// skillMarketMarker 市场/公共技能标记文件（存在即不收录）
const skillMarketMarker = "_skillhub_meta.json"

// skillMeta 解析 SKILL.md：优先取 frontmatter 的 description；没有则取
// 正文首个非标题段落。同时返回 author（用于识别官方/市场技能）。
func skillMeta(root string) (purpose, author string) {
	data, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil || len(data) == 0 {
		return "", ""
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	// YAML frontmatter：文件以 --- 开头，到下一个 --- 结束
	i := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		i = 1
		for ; i < len(lines); i++ {
			l := strings.TrimSpace(lines[i])
			if l == "---" {
				i++
				break
			}
			if strings.HasPrefix(l, "description:") {
				purpose = strings.TrimSpace(strings.TrimPrefix(l, "description:"))
				purpose = strings.Trim(purpose, "\"'")
			} else if strings.HasPrefix(l, "author:") {
				author = strings.TrimSpace(strings.TrimPrefix(l, "author:"))
			}
		}
	}
	if purpose == "" {
		// 正文首个非标题段落
		var sb strings.Builder
		for ; i < len(lines); i++ {
			l := strings.TrimSpace(lines[i])
			if l == "" {
				if sb.Len() > 0 {
					break
				}
				continue
			}
			if strings.HasPrefix(l, "#") || strings.HasPrefix(l, "---") {
				continue
			}
			sb.WriteString(l)
			sb.WriteString(" ")
			if sb.Len() > 200 {
				break
			}
		}
		purpose = strings.TrimSpace(sb.String())
	}
	if len(purpose) > 200 {
		purpose = purpose[:200]
	}
	return purpose, author
}

// officialSkillAuthor 官方/自带技能的 author 标记（不收录个人清单）
func officialSkillAuthor(author string) bool {
	a := strings.ToLower(author)
	return strings.Contains(a, "workbuddy") || strings.Contains(a, "codebuddy") ||
		strings.Contains(a, "tencent") || strings.Contains(a, "官方") ||
		strings.Contains(a, "official")
}

// countProjectFiles 按上传相同的过滤口径统计文件数与字节数
func countProjectFiles(root string) (int, int64) {
	count := 0
	var total int64
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
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
		count++
		total += info.Size()
		return nil
	})
	return count, total
}

// collectInventory 汇总清单（个人技能 + 各工具项目）
func collectInventory() []invItem {
	home, _ := os.UserHomeDir()
	var out []invItem

	// WorkBuddy 项目：~/WorkBuddy/<时间戳>（显示名）
	wbProjRoot := filepath.Join(home, "WorkBuddy")
	names := workbuddyDisplayNames()
	if entries, err := os.ReadDir(wbProjRoot); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			purpose := ""
			if display, ok := names[strings.ToLower(filepath.Join(wbProjRoot, name))]; ok && display != "" {
				name = display
				purpose = "WorkBuddy 工作区: " + display
			}
			root := filepath.Join(wbProjRoot, e.Name())
			n, sz := countProjectFiles(root)
			out = append(out, invItem{Kind: "project", Tool: "workbuddy",
				Name: sanitizeProjectName(name), LocalPath: root,
				FileCount: n, TotalBytes: sz, Purpose: purpose})
		}
	}

	// 个人技能：~/.workbuddy/skills、~/.codebuddy/skills（排除市场技能）
	skillRoots := []struct{ root, tool, prefix string }{
		{filepath.Join(home, ".workbuddy", "skills"), "workbuddy", "WB"},
		{filepath.Join(home, ".codebuddy", "skills"), "codebuddy", "CB"},
	}
	for _, sr := range skillRoots {
		entries, err := os.ReadDir(sr.root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), "_") {
				continue
			}
			root := filepath.Join(sr.root, e.Name())
			if _, err := os.Stat(filepath.Join(root, skillMarketMarker)); err == nil {
				continue // 市场/公共技能不收录
			}
			purpose, author := skillMeta(root)
			if officialSkillAuthor(author) {
				continue // 官方/软件自带技能不收录
			}
			n, sz := countProjectFiles(root)
			out = append(out, invItem{Kind: "skill", Tool: sr.tool,
				Name: e.Name(), LocalPath: root,
				FileCount: n, TotalBytes: sz,
				Purpose: purpose})
		}
	}

	// ZCode 工作区
	if entries, err := os.ReadDir(filepath.Join(home, ".zcode", "workspace")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				root := filepath.Join(home, ".zcode", "workspace", e.Name())
				n, sz := countProjectFiles(root)
				out = append(out, invItem{Kind: "project", Tool: "zcode",
					Name: sanitizeProjectName("zc-"+e.Name()), LocalPath: root,
					FileCount: n, TotalBytes: sz})
			}
		}
	}

	if len(out) > scanMaxTotalProjects {
		out = out[:scanMaxTotalProjects]
	}
	return out
}

// reportInventory 上报清单到服务端
func reportInventory(server, apiKey string, items []invItem) error {
	body, err := json.Marshal(map[string]interface{}{"items": items})
	if err != nil {
		return err
	}
	url := strings.TrimSuffix(server, "/") + "/api/usage/shadow_inventory"
	req, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var parsed struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if json.Unmarshal(respBody, &parsed) == nil && !parsed.Success {
		return fmt.Errorf("%s", parsed.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// pendingTask 服务端拉取任务
type pendingTask struct {
	Id        int    `json:"id"`
	ItemName  string `json:"item_name"`
	LocalPath string `json:"local_path"`
}

// pollTasks 拉取待执行任务
func pollTasks(server, apiKey string) ([]pendingTask, error) {
	url := strings.TrimSuffix(server, "/") + "/api/usage/shadow_tasks"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var parsed struct {
		Success bool `json:"success"`
		Data    struct {
			Tasks []pendingTask `json:"tasks"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed.Data.Tasks, nil
}

// finishTask 回报任务结果
func finishTask(server, apiKey string, task pendingTask, ok bool, files int, note string) error {
	body, _ := json.Marshal(map[string]interface{}{
		"task_id": task.Id, "ok": ok, "files": files, "note": note,
	})
	url := strings.TrimSuffix(server, "/") + "/api/usage/shadow_task_done"
	req, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// runReportInventory 托盘「扫描上报清单」
func (k *keeper) runReportInventory() {
	apiKey := cachedAPIKey()
	server := resolveServer()
	if apiKey == "" || server == "" {
		k.log("尚未配置（无密钥），请先用配置码完成一键配置")
		return
	}
	items := collectInventory()
	if len(items) == 0 {
		k.log("未发现可上报的项目/技能")
		return
	}
	if err := reportInventory(server, apiKey, items); err != nil {
		k.log("清单上报失败: %v", err)
		return
	}
	k.log("清单上报完成：%d 个项目/技能（含个人技能 %d 个）",
		len(items), func() int { n := 0; for _, it := range items { if it.Kind == "skill" { n++ } }; return n }())
}

// runTaskPoller 常驻任务轮询：领任务 → 拉取上传 → 回报
func (k *keeper) runTaskPoller() {
	time.Sleep(20 * time.Second)
	for {
		apiKey := cachedAPIKey()
		server := resolveServer()
		if apiKey != "" && server != "" {
			tasks, err := pollTasks(server, apiKey)
			if err == nil {
				for _, t := range tasks {
					k.runPullTask(server, apiKey, t)
				}
			}
		}
		time.Sleep(30 * time.Second)
	}
}

// runPullTask 执行单个拉取任务：打包上传 + 回报
func (k *keeper) runPullTask(server, apiKey string, t pendingTask) {
	data, n, err := buildProjectZip(t.LocalPath)
	if err != nil || n == 0 {
		note := "无文本文件或打包失败"
		if err != nil {
			note = err.Error()
		}
		_ = finishTask(server, apiKey, t, false, 0, note)
		k.log("拉取任务[%s]失败: %s", t.ItemName, note)
		return
	}
	if err := uploadProject(server, apiKey, t.ItemName, data); err != nil {
		_ = finishTask(server, apiKey, t, false, 0, err.Error())
		k.log("拉取任务[%s]上传失败: %v", t.ItemName, err)
		return
	}
	_ = finishTask(server, apiKey, t, true, n, "")
	k.log("拉取任务[%s]完成：%d 个文件已入影子库", t.ItemName, n)
}
