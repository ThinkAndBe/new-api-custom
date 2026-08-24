//go:build windows

// ERKE AI 配置工具（erke-config-tool）
//
// 双击运行：弹出网页 UI（本地随机端口），用户粘贴配置链接 → 选 WorkBuddy/CodeBuddy
// → 一键配置 → 工具从服务器拉取 models.json 内容写入对应目录并提示完成。
// 无命令行窗口、无需安装依赖（单文件 exe）。
//
// 配置链接格式（管理台「使用教程」页生成，含用户密钥参数）：
//   https://<server>/api/usage/guide_config?token_id=N&product=workbuddy&key=sk-xxx
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const version = "1.0"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("erke-config-tool", version)
		return
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatalMsg("无法启动本地服务: " + err.Error())
		return
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go serve(ln)
	openBrowser(fmt.Sprintf("http://127.0.0.1:%d", port))
	// 保持进程运行（浏览器即界面；关闭页面后进程随浏览器空闲退出策略见 /shutdown）
	select {}
}

func serve(ln net.Listener) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/apply", handleApply)
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
		go func() { os.Exit(0) }()
	})
	http.Serve(ln, mux)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}

type applyReq struct {
	URL     string `json:"url"`
	Product string `json:"product"`
}

type applyResp struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Models  int    `json:"models"`
	Path    string `json:"path"`
}

func handleApply(w http.ResponseWriter, r *http.Request) {
	var req applyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, applyResp{Message: "请求格式错误"})
		return
	}
	url := strings.TrimSpace(req.URL)
	if url == "" {
		writeJSON(w, applyResp{Message: "请先粘贴配置链接（在使用教程页点「复制命令」旁边的「复制链接」）"})
		return
	}
	product := req.Product
	if product != "workbuddy" && product != "codebuddy" {
		product = "workbuddy"
	}
	// 拉取配置。链接可带 key 参数（使用教程页生成时附带用户令牌），
	// 也可以不带——由服务端改为一次性签名链接时同样直接请求。
	client := &http.Client{Timeout: 30 * time.Second}
	httpReq, _ := http.NewRequest("GET", url, nil)
	if u, err := neturl.Parse(url); err == nil {
		if k := u.Query().Get("key"); k != "" {
			httpReq.Header.Set("Authorization", "Bearer "+k)
		}
	}
	// 附带产品参数（服务端仅记录用途，不参与内容生成）
	if !strings.Contains(url, "product=") {
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		httpReq.URL.RawQuery += sep + "product=" + product
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		writeJSON(w, applyResp{Message: "拉取配置失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var apiResp struct {
		Success bool `json:"success"`
		Data    struct {
			Models []map[string]any `json:"models"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil || !apiResp.Success {
		writeJSON(w, applyResp{Message: "配置链接无效或已过期，请回使用教程页重新复制"})
		return
	}
	// 写入目标文件
	dirName := ".workbuddy"
	productName := "WorkBuddy"
	if product == "codebuddy" {
		dirName = ".codebuddy"
		productName = "CodeBuddy"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		writeJSON(w, applyResp{Message: "无法定位用户目录: " + err.Error()})
		return
	}
	dir := filepath.Join(home, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, applyResp{Message: "创建目录失败: " + err.Error()})
		return
	}
	target := filepath.Join(dir, "models.json")
	cfg := map[string]any{"models": apiResp.Data.Models}
	out, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(target, out, 0o644); err != nil {
		writeJSON(w, applyResp{Message: "写入文件失败: " + err.Error()})
		return
	}
	writeJSON(w, applyResp{
		Success: true,
		Message: fmt.Sprintf("✅ 配置完成！共 %d 个模型，已写入 %s。请重启 %s 生效。", len(apiResp.Data.Models), target, productName),
		Models:  len(apiResp.Data.Models),
		Path:    target,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		exec.Command("open", url).Start()
	default:
		exec.Command("xdg-open", url).Start()
	}
}

func fatalMsg(msg string) {
	exec.Command("rundll32", "user32.dll,MessageBox", "0", msg, "ERKE 配置工具", "16").Start()
}
