//go:build windows

// ERKE AI 配置工具 v2 —— 原生 Windows 界面（Win32 控件，非浏览器窗口）。
//
// 界面：6 位配置码输入 → 选 WorkBuddy/CodeBuddy → 一键配置 → 状态提示。
// 配置码在使用教程页「生成配置码」获得（5 分钟有效，一次性）。
//
// 构建（需 MinGW gcc；首次需生成一次资源 syso）：
//
//	go run github.com/akavel/rsrc -manifest app.manifest -o rsrc_windows_amd64.syso
//	CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -H windowsgui -X main.serverBase=https://tokenhub.erke.com" -o erke-config-tool.exe
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

const version = "2.0"

// serverBase 由构建时注入（-ldflags "-X main.serverBase=..."）
var serverBase = "https://tokenhub.erke.com"

type appUI struct {
	mw            *walk.MainWindow
	titleLabel    *walk.Label
	subtitleLabel *walk.Label
	codeLabel     *walk.Label
	codeEdit      *walk.LineEdit
	rbWork        *walk.RadioButton
	rbCode        *walk.RadioButton
	applyBtn      *walk.PushButton
	statusLabel   *walk.TextLabel
	dark          bool // 系统深色模式
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("erke-config-tool", version)
		return
	}
	ui := &appUI{}

	err := MainWindow{
		AssignTo: &ui.mw,
		Title:    "ERKE AI 配置工具",
		MinSize:  Size{Width: 420, Height: 320},
		Size:     Size{Width: 480, Height: 380},
		Layout:   VBox{Margins: Margins{Left: 28, Top: 22, Right: 28, Bottom: 18}, Spacing: 12},
		Children: []Widget{
			// 标题区
			Composite{
				Layout: VBox{Margins: Margins{}},
				Children: []Widget{
					Label{AssignTo: &ui.titleLabel, Text: "ERKE AI 配置工具", Font: Font{Family: "Segoe UI Variable Display", PointSize: 15}},
					Label{
						AssignTo:  &ui.subtitleLabel,
						Text:      "在使用教程页点「生成配置码」，把 6 位码填到下面",
						TextColor: walk.Color(0x6B6B6B),
						Font:      Font{Family: "Segoe UI Variable Text", PointSize: 8},
					},
				},
			},
			// 配置码输入区
			Composite{
				Layout: VBox{Margins: Margins{Top: 6, Bottom: 2}},
				Children: []Widget{
					Label{AssignTo: &ui.codeLabel, Text: "配置码", Font: Font{Family: "Segoe UI Variable Text", PointSize: 9}},
					LineEdit{
						AssignTo:  &ui.codeEdit,
						CueBanner: "000000",
						MaxLength: 6,
						Font:      Font{Family: "Consolas", PointSize: 15},
						OnTextChanged: func() {
							txt := strings.ToUpper(ui.codeEdit.Text())
							txt = strings.Map(func(r rune) rune {
								if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') {
									return r
								}
								return -1
							}, txt)
							if txt != ui.codeEdit.Text() {
								ui.codeEdit.SetText(txt)
							}
						},
					},
				},
			},
			// 客户端选择
			Composite{
				Layout: HBox{Margins: Margins{Top: 2}},
				Children: []Widget{
					Label{Text: "配置到：", TextColor: walk.Color(0x6B6B6B)},
					RadioButtonGroup{
						Buttons: []RadioButton{
							{AssignTo: &ui.rbWork, Text: "WorkBuddy", Value: 1},
							{AssignTo: &ui.rbCode, Text: "CodeBuddy", Value: 2},
						},
					},
				},
			},
			// 主按钮
			PushButton{
				AssignTo: &ui.applyBtn,
				Text:     "一键配置",
				MinSize:  Size{Height: 46},
				Font:     Font{Family: "Segoe UI Variable Display", PointSize: 11},
				OnClicked: func() {
					go ui.apply()
				},
			},
			// 状态区（卡片感：上边距留白 + 小字）
			Composite{
				Layout: VBox{Margins: Margins{Top: 8}},
				Children: []Widget{
					TextLabel{
						AssignTo:  &ui.statusLabel,
						Text:      "填好配置码后点上方按钮",
						TextColor: walk.Color(0x8A8A8A),
						Font:      Font{Family: "Segoe UI Variable Text", PointSize: 9},
					},
				},
			},
			VSpacer{},
		},
	}.Create()
	if err != nil {
		walk.MsgBox(nil, "ERKE 配置工具", "界面创建失败: "+err.Error(), walk.MsgBoxIconError)
		return
	}
	applyWin11Style(ui.mw)
	ui.dark = shouldUseDark()
	if ui.dark {
		ui.applyDarkTheme()
	}
	ui.rbWork.SetChecked(true)
	ui.mw.Run()
}

// applyDarkTheme 客户端区域暗色适配：窗口底色、标签文字、控件视觉样式。
func (ui *appUI) applyDarkTheme() {
	if bg, err := walk.NewSolidColorBrush(walk.Color(0x202020)); err == nil {
		ui.mw.SetBackground(bg)
	}
	lightText := walk.Color(0xF0F0F0)
	midText := walk.Color(0x9B9B9B)
	ui.titleLabel.SetTextColor(lightText)
	ui.subtitleLabel.SetTextColor(midText)
	ui.codeLabel.SetTextColor(walk.Color(0xC8C8C8))
	ui.codeEdit.SetTextColor(lightText)
	if bg, err := walk.NewSolidColorBrush(walk.Color(0x2B2B2B)); err == nil {
		ui.codeEdit.SetBackground(bg)
	}
	for _, w := range []walk.Widget{ui.codeEdit, ui.applyBtn, ui.rbWork, ui.rbCode} {
		darkThemeControl(w)
	}
}

func (ui *appUI) product() string {
	if ui.rbCode.Checked() {
		return "codebuddy"
	}
	return "workbuddy"
}

func (ui *appUI) setStatus(text string, ok bool) {
	ui.mw.Synchronize(func() {
		ui.statusLabel.SetText(text)
		var c walk.Color
		if ok {
			if ui.dark {
				c = 0x5BD75B
			} else {
				c = 0x008000
			}
		} else {
			if ui.dark {
				c = 0x6B6BFF
			} else {
				c = 0xB00000
			}
		}
		ui.statusLabel.SetTextColor(c)
	})
}

func (ui *appUI) apply() {
	ui.mw.Synchronize(func() {
		ui.applyBtn.SetEnabled(false)
		ui.applyBtn.SetText("配置中…")
	})
	defer ui.mw.Synchronize(func() {
		ui.applyBtn.SetEnabled(true)
		ui.applyBtn.SetText("一键配置")
	})

	product := ui.product()
	code := strings.TrimSpace(ui.codeEdit.Text())
	if !isBareCode(code) {
		ui.setStatus("请输入 6 位配置码（在教程页生成）", false)
		return
	}
	target := code

	cfg, err := fetchAndBuild(target, product)
	if err != nil {
		ui.setStatus(err.Error(), false)
		return
	}
	path, err := writeModelsFile(product, cfg)
	if err != nil {
		ui.setStatus("写入文件失败: "+err.Error(), false)
		return
	}
	productName := "WorkBuddy"
	if product == "codebuddy" {
		productName = "CodeBuddy"
	}
	ui.setStatus(fmt.Sprintf("✅ 配置完成！共 %d 个模型\r\n已写入 %s\r\n请重启 %s 生效", len(cfg.Models), path, productName), true)
}

// fetchAndBuild 拉取配置：支持 6 位码 / /redeem 相对路径 / 完整链接（可带 key）。
func fetchAndBuild(target, product string) (*usageConfig, error) {
	if isBareCode(target) {
		target = "/redeem?code=" + neturl.QueryEscape(target)
	}
	if strings.HasPrefix(target, "/redeem") {
		code := ""
		if u, err := neturl.Parse(target); err == nil {
			code = u.Query().Get("code")
		}
		if code == "" {
			return nil, fmt.Errorf("配置码为空")
		}
		server := resolveServer()
		if server == "" {
			return nil, fmt.Errorf("工具未配置服务器地址")
		}
		target = strings.TrimSuffix(server, "/") + "/api/usage/guide_redeem?code=" + neturl.QueryEscape(code)
	}
	if !strings.Contains(target, "://") {
		return nil, fmt.Errorf("请输入 6 位配置码，或粘贴完整链接（https:// 开头）")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	httpReq, _ := http.NewRequest("GET", target, nil)
	if u, err := neturl.Parse(target); err == nil {
		if k := u.Query().Get("key"); k != "" {
			httpReq.Header.Set("Authorization", "Bearer "+k)
		}
		q := u.Query()
		if q.Get("product") == "" {
			q.Set("product", product)
			httpReq.URL.RawQuery = q.Encode()
		}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("拉取配置失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var apiResp struct {
		Success bool `json:"success"`
		Data    struct {
			Models []usageModel `json:"models"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil || !apiResp.Success {
		if apiResp.Message != "" {
			return nil, fmt.Errorf("%s", apiResp.Message)
		}
		return nil, fmt.Errorf("配置码无效或已过期，请回教程页重新生成")
	}
	return &usageConfig{Models: apiResp.Data.Models}, nil
}

type usageModel struct {
	Id                string `json:"id"`
	Name              string `json:"name"`
	Provider          string `json:"provider"`
	URL               string `json:"url"`
	APIKey            string `json:"apiKey"`
	MaxInputTokens    int    `json:"maxInputTokens"`
	MaxOutputTokens   int    `json:"maxOutputTokens"`
	SupportsToolCall  bool   `json:"supportsToolCall"`
	SupportsImages    bool   `json:"supportsImages"`
	SupportsReasoning bool   `json:"supportsReasoning"`
}

type usageConfig struct {
	Models []usageModel `json:"models"`
}

func writeModelsFile(product string, cfg *usageConfig) (string, error) {
	dirName := ".workbuddy"
	if product == "codebuddy" {
		dirName = ".codebuddy"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(dir, "models.json")
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(target, out, 0o644); err != nil {
		return "", err
	}
	return target, nil
}

// resolveServer 返回服务器地址：环境变量 ERKE_CONFIG_SERVER 优先（便于测试），
// 否则用构建时注入的 serverBase。
func resolveServer() string {
	if v := strings.TrimSpace(os.Getenv("ERKE_CONFIG_SERVER")); v != "" {
		return v
	}
	return strings.TrimSpace(serverBase)
}

// isBareCode 判断是否为裸 6 位配置码（0-9 A-Z）
func isBareCode(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}
