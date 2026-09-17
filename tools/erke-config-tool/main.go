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

const version = "2.3"

// serverBase 由构建时注入（-ldflags "-X main.serverBase=..."）
var serverBase = "https://tokenhub.erke.com:3000"

// Hero 渐变用色（walk.Color 为 0x00BBGGRR）
const (
	heroColorFrom = 0xF6823B // #3B82F6 蓝
	heroColorTo   = 0xF65C8B // #8B5CF6 紫
	heroTextMain  = 0xFFFFFF
	heroTextSub   = 0xFFE7E0 // #E0E7FF 淡紫白
)

type appUI struct {
	mw            *walk.MainWindow
	body          *walk.Composite
	titleLabel    *walk.Label
	subtitleLabel *walk.Label
	codeLabel     *walk.Label
	targetLabel   *walk.Label
	codeEdit      *walk.LineEdit
	rbWork        *walk.RadioButton
	rbCode        *walk.RadioButton
	applyBtn      *walk.PushButton
	statusLabel   *walk.TextLabel
	dark          bool // 系统深色模式
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version":
			fmt.Println("erke-config-tool", version)
			return
		case "--normalize":
			// 把既有 models.json 归一化为裸数组（修复"对象包裹格式被清空"的存量问题）
			product := cliProduct(os.Args[2:])
			path, n, changed, bak, err := normalizeModelsFile(product)
			if err != nil {
				fmt.Println("[失败]", err)
				os.Exit(1)
			}
			if !changed {
				fmt.Printf("[无需处理] %s（已是裸数组或文件不存在，共 %d 个模型）\n", path, n)
				return
			}
			fmt.Printf("[已修复] %s\n  条数：%d\n  原文件备份：%s\n  说明：已改为裸数组格式（顶层 [），不会再被 WorkBuddy 硬件门限清理\n", path, n, bak)
			return
		case "--server":
			fmt.Printf("生效地址：%s\n候选列表（按顺序尝试）：\n", resolveServer())
			for i, c := range serverCandidates() {
				fmt.Printf("  %d) %s\n", i+1, c)
			}
			return
		case "--check":
			product := cliProduct(os.Args[2:])
			path, format, n, err := inspectModelsFile(product)
			if err != nil {
				fmt.Println("[异常]", err)
				os.Exit(1)
			}
			risk := "安全"
			if format == "object" {
				risk = "危险（对象包裹格式，重启可能被清空，请执行 --normalize）"
			} else if format == "empty" {
				risk = "空文件（未配置）"
			}
			fmt.Printf("文件：%s\n格式：%s\n模型数：%d\n风险：%s\n", path, format, n, risk)
			return
		}
	}
	ui := &appUI{}

	err := MainWindow{
		AssignTo: &ui.mw,
		Title:    "ERKE AI 配置工具",
		MinSize:  Size{Width: 420, Height: 340},
		Size:     Size{Width: 480, Height: 400},
		Layout:   VBox{Margins: Margins{}, Spacing: 0},
		Children: []Widget{
			// Hero 渐变头部（通栏）
			GradientComposite{
				Color1: walk.Color(heroColorFrom),
				Color2: walk.Color(heroColorTo),
				Layout: VBox{Margins: Margins{Left: 28, Top: 22, Right: 28, Bottom: 18}, Spacing: 4},
				Children: []Widget{
					Label{
						AssignTo:  &ui.titleLabel,
						Text:      "ERKE AI 配置工具",
						TextColor: heroTextMain,
						Font:      Font{Family: "Segoe UI Variable Display", PointSize: 16},
					},
					Label{
						AssignTo:  &ui.subtitleLabel,
						Text:      "v" + version + " · 在使用教程页点「生成配置码」，填到下面即可",
						TextColor: heroTextSub,
						Font:      Font{Family: "Segoe UI Variable Text", PointSize: 8},
					},
				},
			},
			// 正文
			Composite{
				AssignTo:   &ui.body,
				Background: SolidColorBrush{Color: 0xFFFFFF},
				Layout:     VBox{Margins: Margins{Left: 28, Top: 20, Right: 28, Bottom: 18}, Spacing: 12},
				Children: []Widget{
					// 配置码输入区
					Composite{
						Layout: VBox{Margins: Margins{}},
						Children: []Widget{
							Label{AssignTo: &ui.codeLabel, Text: "配置码", TextColor: walk.Color(0x444444), Font: Font{Family: "Segoe UI Variable Text", PointSize: 9}},
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
						Layout: HBox{Margins: Margins{}},
						Children: []Widget{
							Label{AssignTo: &ui.targetLabel, Text: "配置到：", TextColor: walk.Color(0x6B6B6B)},
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
						MinSize:  Size{Height: 48},
						Font:     Font{Family: "Segoe UI Variable Display", PointSize: 11},
						OnClicked: func() {
							go ui.apply()
						},
					},
					// 状态区
					TextLabel{
						AssignTo:  &ui.statusLabel,
						Text:      "填好配置码后点上方按钮",
						TextColor: walk.Color(0x8A8A8A),
						Font:      Font{Family: "Segoe UI Variable Text", PointSize: 9},
					},
					VSpacer{},
				},
			},
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

// applyDarkTheme 客户端区域暗色适配：正文底色、标签文字、控件视觉样式。
// Hero 渐变头部深浅色共用，不变。
func (ui *appUI) applyDarkTheme() {
	if bg, err := walk.NewSolidColorBrush(walk.Color(0x202020)); err == nil {
		ui.mw.SetBackground(bg)
		ui.body.SetBackground(bg)
	}
	lightText := walk.Color(0xF0F0F0)
	ui.codeLabel.SetTextColor(walk.Color(0xC8C8C8))
	ui.targetLabel.SetTextColor(walk.Color(0x9B9B9B))
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
	path, total, changed, bak, err := writeModelsFileMerged(product, cfg)
	if err != nil {
		ui.setStatus("写入文件失败: "+err.Error(), false)
		return
	}
	productName := "WorkBuddy"
	if product == "codebuddy" {
		productName = "CodeBuddy"
	}
	msg := fmt.Sprintf("✅ 配置完成！本次写入/更新 %d 个模型（文件内共 %d 个）\r\n已写入 %s\r\n格式：裸数组（不会再被重启清空）\r\n请重启 %s 生效", changed, total, path, productName)
	if bak != "" {
		msg += "\r\n原文件已备份：" + filepath.Base(bak)
	}
	ui.setStatus(msg, true)
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

// serverCandidates 返回要尝试的服务器地址（按优先级、去重）：
//  1. 环境变量 ERKE_CONFIG_SERVER（测试/临时覆盖）
//  2. 构建注入的 serverBase（默认 https://tokenhub.erke.com:3000）
//  3. 同主机的 :3000 与 443 变体（互为兜底）
//
// 为什么要兜底：443 挂在零信任(aTrust)后面，非浏览器客户端会被 302 拽到门户认证页；
// 而 :3000 是 API-only 端口、不过零信任。两边各有适用网络环境，任一可用即可。
func serverCandidates() []string {
	var out []string
	seen := map[string]bool{}
	add := func(u string) {
		u = strings.TrimSuffix(strings.TrimSpace(u), "/")
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, u)
	}
	if env := strings.TrimSpace(os.Getenv("ERKE_CONFIG_SERVER")); env != "" {
		add(env)
	}
	add(serverBase)
	// 同主机的两种端口变体
	for _, base := range append([]string{}, out...) {
		if u, err := neturl.Parse(base); err == nil && u.Host != "" {
			host := u.Hostname()
			add(u.Scheme + "://" + host + ":3000")
			add(u.Scheme + "://" + host)
		}
	}
	return out
}

var errNetworkBlocked = fmt.Errorf("网络受限")

// fetchOnce 向单个完整 URL 发请求并解析；返回 (配置, 是否为服务端的明确业务失败信息)
func fetchOnce(target, product string) (*usageConfig, string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	httpReq, err := http.NewRequest("GET", target, nil)
	if err != nil {
		return nil, "", err
	}
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
		return nil, "", fmt.Errorf("%w: %v", errNetworkBlocked, err)
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
	if err := json.Unmarshal(body, &apiResp); err != nil {
		// 拿到的不是 JSON：多半是被零信任/网关拦下返回了 HTML 登录页
		head := strings.TrimSpace(string(body))
		if len(head) > 120 {
			head = head[:120] + "…"
		}
		return nil, "", fmt.Errorf("%w: 返回内容不是 JSON（HTTP %d）：%s", errNetworkBlocked, resp.StatusCode, head)
	}
	if !apiResp.Success {
		return nil, apiResp.Message, nil // 服务端明确拒绝（如配置码无效/过期）——权威结论，不再换地址重试
	}
	return &usageConfig{Models: apiResp.Data.Models}, "", nil
}

// fetchWithCandidates 依次尝试候选地址：网络类失败换下一个地址，业务类失败（配置码无效）立即返回。
// 抽出来是为了可单测（不依赖真实网络环境）。
func fetchWithCandidates(candidates []string, code, product string) (*usageConfig, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("工具未配置服务器地址")
	}
	var lastErr error
	for _, server := range candidates {
		url := server + "/v1/usage/guide_redeem?code=" + neturl.QueryEscape(code)
		cfg, bizMsg, err := fetchOnce(url, product)
		// 先判业务失败：fetchOnce 对「服务端明确拒绝」返回的是 (nil, 消息, nil)，
		// 若先判 err==nil 会把这个当成成功，导致 GUI 拿到空配置（曾踩过）。
		if bizMsg != "" {
			return nil, fmt.Errorf("%s", bizMsg) // 配置码无效/过期：权威结论，不再换地址重试
		}
		if err == nil {
			return cfg, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("拉取配置失败：已尝试 %d 个服务器地址均不可达。\n%v\n请确认已连接公司网络（或已登录零信任/aTrust）后重试；"+
		"仍失败可运行 `erke-config-tool.exe --server` 查看候选地址并反馈给管理员。", len(candidates), lastErr)
}

// fetchAndBuild 拉取配置：支持 6 位码（走候选地址兜底）/ 完整链接（单地址）。
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
		return fetchWithCandidates(serverCandidates(), code, product)
	}
	if !strings.Contains(target, "://") {
		return nil, fmt.Errorf("请输入 6 位配置码，或粘贴完整链接（https:// 开头）")
	}
	cfg, bizMsg, err := fetchOnce(target, product)
	if err != nil {
		return nil, fmt.Errorf("拉取配置失败：%v", err)
	}
	if bizMsg != "" {
		return nil, fmt.Errorf("%s", bizMsg)
	}
	return cfg, nil
}

// cliProduct 解析 --product 参数（默认 workbuddy）
func cliProduct(args []string) string {
	for i, a := range args {
		if a == "--product" && i+1 < len(args) {
			return strings.TrimSpace(args[i+1])
		}
		if strings.HasPrefix(a, "--product=") {
			return strings.TrimSpace(strings.TrimPrefix(a, "--product="))
		}
	}
	return "workbuddy"
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
