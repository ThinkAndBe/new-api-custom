package service

// zhipu_browser.go — 智谱无头浏览器登录：截图透传 + 表单填充 + Token 捕获。
//
// 流程：管理员在额度监控页点「连接账号」→ 后端启动 headless Chrome →
// 截图发给前端 → 管理员在我们的页面看到智谱登录页 → 输入手机号/密码 →
// 后端填入浏览器 → 如果有验证码再截图 → 管理员输入 → 登录完成 →
// 捕获 Authorization token → 存入 ProviderQuotaAccount。

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// ZhipuBrowserSession 一次浏览器登录会话
type ZhipuBrowserSession struct {
	ID           string `json:"id"`
	Cancel       context.CancelFunc
	Ctx          context.Context
	CreatedAt    time.Time
	Status       string `json:"status"` // open / waiting_code / done / expired / error
	AuthToken    string
	CapturedURL  string // 登录成功后跳转的 URL
	ScreenshotAt time.Time
	mu           sync.Mutex
}

var (
	zhipuSessions   = sync.Map{}
	sessionTimeout  = 5 * time.Minute
	cleanupInterval = 30 * time.Second
)

func init() {
	// 定期清理过期会话
	go func() {
		for range time.Tick(cleanupInterval) {
			now := time.Now()
			zhipuSessions.Range(func(key, val interface{}) bool {
				s, ok := val.(*ZhipuBrowserSession)
				if ok && now.Sub(s.CreatedAt) > sessionTimeout {
					s.Cancel()
					zhipuSessions.Delete(key)
					common.SysLog("zhipu browser session expired: " + s.ID)
				}
				return true
			})
		}
	}()
}

// StartZhipuLogin 启动浏览器登录会话
func StartZhipuLogin() (*ZhipuBrowserSession, error) {
	// Chrome 路径（本地开发 / Docker）
	execPath := findChromePath()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("window-size", "1280,800"),
	)
	if execPath != "" {
		opts = append(opts, chromedp.ExecPath(execPath))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)

	session := &ZhipuBrowserSession{
		ID:        common.GetUUID(),
		Cancel:    func() { cancel(); allocCancel() },
		Ctx:       ctx,
		CreatedAt: time.Now(),
		Status:    "open",
	}
	zhipuSessions.Store(session.ID, session)

	// 导航到智谱登录页（直接用会话主上下文，避免派生 ctx 取消导致标签页关闭）
	err := chromedp.Run(ctx,
		chromedp.Navigate("https://bigmodel.cn/login"),
		chromedp.WaitReady(`body`, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
	)
	if err != nil {
		session.Status = "error"
		session.Cancel()
		zhipuSessions.Delete(session.ID)
		return nil, fmt.Errorf("无法打开智谱登录页: %v", err)
	}

	return session, nil
}

// GetZhipuScreenshot 截取当前页面
func GetZhipuScreenshot(sessionID string) ([]byte, error) {
	val, ok := zhipuSessions.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("会话不存在或已过期")
	}
	s := val.(*ZhipuBrowserSession)

	ctx, cancel := context.WithTimeout(s.Ctx, 10*time.Second)
	defer cancel()

	var buf []byte
	err := chromedp.Run(ctx, chromedp.FullScreenshot(&buf, 90))
	if err != nil {
		return nil, fmt.Errorf("截图失败: %v", err)
	}
	s.mu.Lock()
	s.ScreenshotAt = time.Now()
	s.mu.Unlock()
	return buf, nil
}

// ZhipuFillCredentials 在浏览器中填入手机号和密码
func ZhipuFillCredentials(sessionID, phone, password string) error {
	val, ok := zhipuSessions.Load(sessionID)
	if !ok {
		return fmt.Errorf("会话不存在或已过期")
	}
	s := val.(*ZhipuBrowserSession)

	ctx, cancel := context.WithTimeout(s.Ctx, 15*time.Second)
	defer cancel()

	// 智谱登录页可能需要先点击"密码登录" tab
	// 尝试多种选择器
	actions := []chromedp.Action{
		// 尝试点击密码登录 tab（如果有）
		chromedp.WaitVisible(`body`, chromedp.ByQuery),
	}
	_ = chromedp.Run(ctx, actions...)

	// 填入手机号
	phoneSelectors := []string{
		`input[placeholder*="手机"]`,
		`input[placeholder*="phone"]`,
		`input[type="tel"]`,
		`input[name="phone"]`,
		`input[name="phoneNumber"]`,
	}
	for _, sel := range phoneSelectors {
		if err := chromedp.Run(ctx, chromedp.Clear(sel), chromedp.SendKeys(sel, phone)); err == nil {
			break
		}
	}

	// 填入密码
	passwordSelectors := []string{
		`input[type="password"]`,
		`input[placeholder*="密码"]`,
		`input[name="password"]`,
	}
	for _, sel := range passwordSelectors {
		if err := chromedp.Run(ctx, chromedp.Clear(sel), chromedp.SendKeys(sel, password)); err == nil {
			break
		}
	}

	return nil
}

// ZhipuSubmitLogin 点击登录按钮
func ZhipuSubmitLogin(sessionID string) error {
	val, ok := zhipuSessions.Load(sessionID)
	if !ok {
		return fmt.Errorf("会话不存在或已过期")
	}
	s := val.(*ZhipuBrowserSession)

	ctx, cancel := context.WithTimeout(s.Ctx, 15*time.Second)
	defer cancel()

	// 点击登录按钮
	buttonSelectors := []string{
		`button:has-text("登录")`,
		`button.login-btn`,
		`button[type="submit"]`,
		`button.btn-primary`,
	}
	for _, sel := range buttonSelectors {
		if err := chromedp.Run(ctx, chromedp.Click(sel)); err == nil {
			break
		}
	}

	// 等待可能的跳转或验证码
	time.Sleep(2 * time.Second)
	s.mu.Lock()
	s.Status = "waiting_code"
	s.mu.Unlock()
	return nil
}

// ZhipuCaptureToken 等待登录完成并捕获 token
func ZhipuCaptureToken(sessionID string, timeoutSec int) (string, string, error) {
	val, ok := zhipuSessions.Load(sessionID)
	if !ok {
		return "", "", fmt.Errorf("会话不存在或已过期")
	}
	s := val.(*ZhipuBrowserSession)

	// 监听网络请求，捕获 Authorization header
	var token string
	var finalURL string

	listenCtx, listenCancel := context.WithTimeout(s.Ctx, time.Duration(timeoutSec)*time.Second)
	defer listenCancel()

	chromedp.ListenTarget(listenCtx, func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			u := e.Request.URL
			if strings.Contains(u, "bigmodel.cn") && !strings.Contains(u, "login") {
				if authRaw, ok := e.Request.Headers["Authorization"]; ok {
					auth, _ := authRaw.(string)
					if auth != "" && strings.Contains(auth, "Bearer") {
						token = auth
						finalURL = u
					}
				}
				// 也检查 X-Token header
				if xtRaw, ok := e.Request.Headers["X-Token"]; ok {
					xt, _ := xtRaw.(string)
					if xt != "" && token == "" {
						token = xt
						finalURL = u
					}
				}
			}
		}
	})

	// 等待页面跳转（登录成功后离开 /login）
	err := chromedp.Run(listenCtx,
		chromedp.WaitVisible(`body`, chromedp.ByQuery),
		chromedp.Sleep(time.Duration(timeoutSec)*time.Second),
	)
	_ = err

	if token == "" {
		// 试试从 cookies 提取
		cookies, err := network.GetCookies().Do(listenCtx)
		if err == nil {
			for _, c := range cookies {
				if strings.Contains(c.Name, "token") || strings.Contains(c.Name, "session") {
					token = c.Value
					break
				}
			}
		}
	}

	s.mu.Lock()
	if token != "" {
		s.Status = "done"
		s.AuthToken = token
		s.CapturedURL = finalURL
	} else {
		s.Status = "expired"
	}
	s.mu.Unlock()

	if token == "" {
		return "", "", fmt.Errorf("未捕获到 token（登录可能失败或需要验证码）")
	}
	return token, finalURL, nil
}

// GetZhipuSessionStatus 获取会话状态
func GetZhipuSessionStatus(sessionID string) (string, error) {
	val, ok := zhipuSessions.Load(sessionID)
	if !ok {
		return "not_found", fmt.Errorf("会话不存在")
	}
	s := val.(*ZhipuBrowserSession)
	return s.Status, nil
}

// CloseZhipuSession 关闭会话
func CloseZhipuSession(sessionID string) {
	if val, ok := zhipuSessions.Load(sessionID); ok {
		s := val.(*ZhipuBrowserSession)
		s.Cancel()
		zhipuSessions.Delete(sessionID)
	}
}

// SaveZhipuTokenToAccount 把捕获的 token 存到指定账号
func SaveZhipuTokenToAccount(accountID int, token string) error {
	a, err := model.GetProviderQuotaAccount(accountID)
	if err != nil {
		return err
	}
	a.SessionToken = token
	if err := model.UpdateProviderQuotaAccount(a); err != nil {
		return err
	}
	// 立即抓取一次额度
	go RefreshProviderQuota(accountID)
	return nil
}

// findChromePath 查找 Chrome 可执行文件
func findChromePath() string {
	paths := []string{
		// Docker 容器
		"/usr/bin/chromium-browser",
		"/usr/bin/chromium",
		"/usr/bin/google-chrome",
		// macOS
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		// Windows
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "" // 让 chromedp 自动查找
}
