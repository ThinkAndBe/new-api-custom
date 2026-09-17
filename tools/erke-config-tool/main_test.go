//go:build windows

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleModel(id string) usageModel {
	return usageModel{
		Id: id, Name: "ERKE " + id, Provider: "openai",
		URL: "https://tokenhub.erke.com:3000/v1", APIKey: "sk-" + id,
		MaxInputTokens: 1000000, MaxOutputTokens: 128000,
		SupportsToolCall: true, SupportsReasoning: true,
	}
}

// 沙箱：把 HOME/USERPROFILE 指到临时目录，避免测试碰到真实 ~/.workbuddy
func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)
	return dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// 核心回归：写入后顶层必须是裸数组 —— WorkBuddy 硬件门限只认这种格式，对象包裹会被清成 []
func TestWriteProducesBareArray(t *testing.T) {
	dir := withTempHome(t)
	cfg := &usageConfig{Models: []usageModel{sampleModel("glm-5.3"), sampleModel("deepseek-flash")}}

	path, total, changed, err := writeModelsFileMerged("workbuddy", cfg)
	if err != nil {
		t.Fatalf("writeModelsFileMerged: %v", err)
	}
	if total != 2 || changed != 2 {
		t.Fatalf("期望 2 条全部写入，实际 total=%d changed=%d", total, changed)
	}
	if !strings.HasPrefix(path, dir) {
		t.Fatalf("写入路径未落在沙箱 HOME 内: %s", path)
	}
	body := strings.TrimSpace(readFile(t, path))
	if !strings.HasPrefix(body, "[") {
		t.Fatalf("顶层不是裸数组，会被 WorkBuddy 清空: %s", body[:min(40, len(body))])
	}
	var back []usageModel
	if err := json.Unmarshal([]byte(body), &back); err != nil {
		t.Fatalf("按数组解析失败: %v", err)
	}
	if len(back) != 2 {
		t.Fatalf("条数不符: %d", len(back))
	}
}

// 存量修复：对象包裹 → 裸数组，且内容不丢
func TestNormalizeObjectWrapper(t *testing.T) {
	dir := withTempHome(t)
	dirPath := filepath.Join(dir, ".workbuddy")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dirPath, "models.json")
	legacy := `{"models":[{"id":"glm-5.3","name":"ERKE glm-5.3","provider":"openai","url":"https://tokenhub.erke.com:3000/v1","apiKey":"sk-a"}]}`
	if err := os.WriteFile(target, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	path, n, changed, err := normalizeModelsFile("workbuddy")
	if err != nil {
		t.Fatalf("normalizeModelsFile: %v", err)
	}
	if !changed || n != 1 {
		t.Fatalf("期望发生改动且保留 1 条，实际 changed=%v n=%d", changed, n)
	}
	// 不应留下 .bak 备份文件（用户要求不生成备份）
	if m, _ := filepath.Glob(filepath.Join(dirPath, "models.json.bak*")); len(m) != 0 {
		t.Fatalf("不应生成备份文件: %v", m)
	}
	if body := strings.TrimSpace(readFile(t, path)); !strings.HasPrefix(body, "[") {
		t.Fatalf("归一化后仍不是裸数组: %s", body)
	}
	// 幂等：再跑一次不应改动
	if _, _, changed2, err := normalizeModelsFile("workbuddy"); err != nil || changed2 {
		t.Fatalf("重复归一化应无改动: changed=%v err=%v", changed2, err)
	}
}

// 合并语义：同 id 更新、其它条目保留，且不会把用户自有模型删掉
func TestMergeKeepsOtherModels(t *testing.T) {
	dir := withTempHome(t)
	dirPath := filepath.Join(dir, ".workbuddy")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := []usageModel{sampleModel("glm-5.3"), sampleModel("my-local-model")}
	existing[0].Name = "旧名字"
	raw, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(filepath.Join(dirPath, "models.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	updated := sampleModel("glm-5.3")
	updated.Name = "ERKE glm-5.3（新）"
	path, total, changed, err := writeModelsFileMerged("workbuddy", &usageConfig{Models: []usageModel{updated}})
	if err != nil {
		t.Fatalf("merge write: %v", err)
	}
	if total != 2 {
		t.Fatalf("应保留用户自有模型（期望 2 条，实际 %d）", total)
	}
	if changed != 1 {
		t.Fatalf("只应更新 1 条，实际 %d", changed)
	}
	var back []usageModel
	if err := json.Unmarshal([]byte(readFile(t, path)), &back); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range back {
		got[m.Id] = m.Name
	}
	if got["my-local-model"] == "" {
		t.Fatal("用户自有模型被删掉了")
	}
	if got["glm-5.3"] != "ERKE glm-5.3（新）" {
		t.Fatalf("同 id 未更新: %q", got["glm-5.3"])
	}
}

// 读取侧两种格式都要能识别（对象包裹必须被判为危险格式，供 --check 提示）
func TestInspectDetectsFormats(t *testing.T) {
	dir := withTempHome(t)
	dirPath := filepath.Join(dir, ".workbuddy")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dirPath, "models.json")

	if _, format, n, err := inspectModelsFile("workbuddy"); err != nil || format != "empty" || n != 0 {
		t.Fatalf("缺文件应为 empty: format=%s n=%d err=%v", format, n, err)
	}
	_ = os.WriteFile(target, []byte(`{"models":[{"id":"a"},{"id":"b"}]}`), 0o644)
	if _, format, n, err := inspectModelsFile("workbuddy"); err != nil || format != "object" || n != 2 {
		t.Fatalf("对象包裹应被识别: format=%s n=%d err=%v", format, n, err)
	}
	_ = os.WriteFile(target, []byte(`[{"id":"a"}]`), 0o644)
	if _, format, n, err := inspectModelsFile("workbuddy"); err != nil || format != "array" || n != 1 {
		t.Fatalf("裸数组应被识别: format=%s n=%d err=%v", format, n, err)
	}
}

// 端到端（可选）：需本机 new-api 运行 + 环境变量 GUIDE_CODE
func TestFetchAndWrite(t *testing.T) {
	code := os.Getenv("GUIDE_CODE")
	if code == "" {
		t.Skip("GUIDE_CODE not set; skip e2e")
	}
	withTempHome(t)
	cfg, err := fetchAndBuild(code, "workbuddy")
	if err != nil {
		t.Fatalf("fetchAndBuild: %v", err)
	}
	if len(cfg.Models) == 0 {
		t.Fatal("no models")
	}
	path, total, _, err := writeModelsFileMerged("workbuddy", cfg)
	if err != nil {
		t.Fatalf("writeModelsFileMerged: %v", err)
	}
	if body := strings.TrimSpace(readFile(t, path)); !strings.HasPrefix(body, "[") {
		t.Fatal("e2e 写入不是裸数组")
	}
	t.Logf("written: %s total=%d", path, total)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 候选地址：环境变量优先，且同主机 :3000 / 443 两种变体都在列表里
func TestServerCandidates(t *testing.T) {
	t.Setenv("ERKE_CONFIG_SERVER", "http://example.invalid:9999/")
	cands := serverCandidates()
	if len(cands) < 3 {
		t.Fatalf("候选地址过少: %v", cands)
	}
	if cands[0] != "http://example.invalid:9999" {
		t.Fatalf("环境变量应优先: %v", cands)
	}
	joined := strings.Join(cands, ",")
	if !strings.Contains(joined, "example.invalid:3000") || !strings.Contains(joined, "http://example.invalid,") && !strings.Contains(joined, "http://example.invalid") {
		t.Fatalf("缺少端口变体: %v", cands)
	}
}

// 兜底与短路：第一个地址不可达 → 换下一个；服务端明确拒绝 → 立即返回不再重试
func TestFetchFallsBackAndShortCircuits(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Query().Get("code") {
		case "ZZZZZZ":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":false,"message":"配置码无效或已过期，请回教程页重新生成"}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"models":[{"id":"glm-5.3","name":"ERKE glm-5.3"}]}}`))
		}
	}))
	defer srv.Close()

	// 1) 第一个地址是死端口，应从第二个（真实测试服务）拿到配置
	cfg, err := fetchWithCandidates([]string{"http://127.0.0.1:1", srv.URL}, "ABC123", "workbuddy")
	if err != nil {
		t.Fatalf("应回退成功: %v", err)
	}
	if len(cfg.Models) != 1 || cfg.Models[0].Id != "glm-5.3" {
		t.Fatalf("回退后配置不对: %+v", cfg.Models)
	}

	// 2) 业务失败（配置码无效）必须短路：只打一次请求，不再尝试后续地址
	hits = 0
	_, err = fetchWithCandidates([]string{srv.URL, "http://127.0.0.1:1"}, "ZZZZZZ", "workbuddy")
	if err == nil || !strings.Contains(err.Error(), "配置码无效") {
		t.Fatalf("应返回配置码无效: %v", err)
	}
	if hits != 1 {
		t.Fatalf("业务失败不应重试其它地址，实际请求 %d 次", hits)
	}

	// 3) 返回 HTML（模拟被零信任拦截）→ 报网络受限而不是"配置码无效"
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><title>aTrust 2.0</title><h1>302 Found</h1></body></html>"))
	}))
	defer blocked.Close()
	_, err = fetchWithCandidates([]string{blocked.URL}, "ABC123", "workbuddy")
	if err == nil || strings.Contains(err.Error(), "配置码无效") {
		t.Fatalf("被拦截时不应误报配置码无效: %v", err)
	}
	if !strings.Contains(err.Error(), "均不可达") {
		t.Fatalf("应提示地址不可达/网络受限: %v", err)
	}
}
