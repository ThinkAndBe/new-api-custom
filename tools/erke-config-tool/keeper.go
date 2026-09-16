package main

// keeper.go — 常驻配置守护 + 托盘。
//
// 背景：WorkBuddy 重启/退出时会用内存状态回写 ~/.workbuddy/models.json，
// 外部（配置工具）写入的模型会被冲掉。守护方案：配置成功后把模型清单
// 缓存到本地，常驻进程每 10 秒检查一次，发现缓存中的模型在文件里缺失
// 就合并补写（WorkBuddy 热加载会立即拾取），从而保持配置不丢失。
//
// 托盘菜单：打开主界面 / 立即补写 / 暂停·恢复守护 / 开机自启 / 退出。
// 关闭窗口 = 最小化到托盘继续守护；退出请走托盘菜单。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lxn/walk"
	"golang.org/x/sys/windows/registry"
)

const (
	keeperInterval   = 10 * time.Second
	keeperAppDirName = "erke-agent-tool"
	autostartRunKey  = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartValue   = "ERKEAITool"
)

type keeper struct {
	mu     sync.Mutex
	paused bool
	ni     *walk.NotifyIcon
	ui     *appUI
	// exiting 由托盘「退出」置位，窗口 Closing 据此放行真正退出
	exiting bool
	exitFunc func()
}

// common_log 静默日志：写 %APPDATA%\erke-agent-tool\tool.log（不打扰用户）
func common_log(msg string) {
	dir := appDataDir()
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filepath.Join(dir, "tool.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
}

func (k *keeper) log(format string, args ...interface{}) {
	msg := time.Now().Format("15:04:05") + " " + fmt.Sprintf(format, args...)
	k.ui.setStatus("["+msg+"]", true)
}

func (k *keeper) isPaused() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.paused
}

func (k *keeper) setPaused(p bool) {
	k.mu.Lock()
	k.paused = p
	k.mu.Unlock()
}

// ---- 本地缓存 ----

func appDataDir() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(base, keeperAppDirName)
}

func cachePath(product string) string {
	return filepath.Join(appDataDir(), product+"-models.json")
}

func saveCache(product string, cfg *usageConfig) error {
	dir := appDataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cachePath(product), data, 0o644)
}

func loadCache(product string) *usageConfig {
	data, err := os.ReadFile(cachePath(product))
	if err != nil {
		return nil
	}
	var cfg usageConfig
	if err := json.Unmarshal(data, &cfg); err != nil || len(cfg.Models) == 0 {
		return nil
	}
	return &cfg
}

// ---- 检测与补写 ----

// repairOnce 检查单个产品的 models.json，缺我们缓存的模型就补写。
// 返回补写的模型数。
func repairOnce(product string) (int, error) {
	cache := loadCache(product)
	if cache == nil {
		return 0, nil // 没缓存过，跳过
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}
	dirName := ".workbuddy"
	if product == "codebuddy" {
		dirName = ".codebuddy"
	}
	target := filepath.Join(home, dirName, "models.json")

	cur := &usageConfig{Models: []usageModel{}}
	if data, err := os.ReadFile(target); err == nil {
		var parsed usageConfig
		if err := json.Unmarshal(data, &parsed); err == nil && parsed.Models != nil {
			cur = &parsed
		}
	}

	// 三重比对：id 缺失、或 id 命中但 url/apiKey 与缓存不一致（旧地址残留，
	// 如 443 时代配置）都视为需要补写；重写时用缓存条目覆盖同 id 旧条目
	cacheByID := map[string]usageModel{}
	for _, m := range cache.Models {
		cacheByID[m.Id] = m
	}
	var missing []usageModel
	for _, m := range cur.Models {
		if want, ok := cacheByID[m.Id]; ok && (m.URL != want.URL || m.APIKey != want.APIKey) {
			missing = append(missing, want)
		}
	}
	curIDs := map[string]bool{}
	for _, m := range cur.Models {
		curIDs[m.Id] = true
	}
	for _, m := range cache.Models {
		if !curIDs[m.Id] {
			missing = append(missing, m)
		}
	}
	if len(missing) == 0 {
		return 0, nil
	}

	// 合并：以当前文件为底，缺失/过期条目按缓存覆盖
	fixed := make([]usageModel, 0, len(cur.Models)+len(missing))
	fixedIDs := map[string]bool{}
	for _, m := range missing {
		fixed = append(fixed, m)
		fixedIDs[m.Id] = true
	}
	for _, m := range cur.Models {
		if !fixedIDs[m.Id] {
			fixed = append(fixed, m)
		}
	}
	merged := &usageConfig{Models: fixed}
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return 0, err
	}
	// 原子写：先临时文件再替换，避免客户端读到半截文件
	tmp := target + ".erke-tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return len(missing), nil
}

// ---- 守护循环 ----

// silentInventoryInterval 静默清单上报间隔（不对用户暴露）
const silentInventoryInterval = 6 * time.Hour

func (k *keeper) run() {
	// 启动先等一小会儿，避开系统/客户端启动高峰
	time.Sleep(15 * time.Second)
	k.log("配置守护已启动")
	// 清单静默上报：启动 10 分钟后首次，此后每 6 小时
	go func() {
		time.Sleep(10 * time.Minute)
		for {
			k.runReportInventory()
			time.Sleep(silentInventoryInterval)
		}
	}()
	for {
		if !k.isPaused() {
			for _, product := range []string{"workbuddy", "codebuddy"} {
				n, err := repairOnce(product)
				if err != nil {
					k.log("守护检查 %s 失败: %v", product, err)
				} else if n > 0 {
					name := "WorkBuddy"
					if product == "codebuddy" {
						name = "CodeBuddy"
					}
					k.log("检测到 %s 配置被重置，已自动补写 %d 个模型", name, n)
				}
			}
		}
		time.Sleep(keeperInterval)
	}
}

// repairNow 托盘「立即补写」
func (k *keeper) repairNow() {
	total := 0
	for _, product := range []string{"workbuddy", "codebuddy"} {
		n, err := repairOnce(product)
		if err != nil {
			k.log("手动补写 %s 失败: %v", product, err)
			return
		}
		total += n
	}
	if total > 0 {
		k.log("手动补写完成，共 %d 个模型", total)
	} else {
		k.log("配置完整，无需补写")
	}
}

// ---- 开机自启（HKCU Run） ----

func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartRunKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(autostartValue)
	return err == nil
}

func setAutostart(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartRunKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if on {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		return k.SetStringValue(autostartValue, `"`+exe+`" /min`)
	}
	return k.DeleteValue(autostartValue)
}

// ---- 托盘 ----

func (k *keeper) setupTray() error {
	ni, err := walk.NewNotifyIcon(k.ui.mw)
	if err != nil {
		return err
	}
	k.ni = ni
	_ = ni.SetIcon(walk.IconApplication())
	_ = ni.SetToolTip("ERKE AI 工具 · 配置守护运行中")

	showAct := walk.NewAction()
	showAct.SetText("打开主界面")
	showAct.Triggered().Attach(func() {
		k.ui.mw.Show()
		k.ui.mw.SetFocus()
	})
	_ = ni.ContextMenu().Actions().Add(showAct)

	repairAct := walk.NewAction()
	repairAct.SetText("立即补写配置")
	repairAct.Triggered().Attach(func() { go k.repairNow() })
	_ = ni.ContextMenu().Actions().Add(repairAct)

	pauseAct := walk.NewAction()
	pauseAct.SetText("暂停配置守护")
	_ = pauseAct.SetCheckable(true)
	pauseAct.Triggered().Attach(func() {
		p := !k.isPaused()
		k.setPaused(p)
		if p {
			pauseAct.SetText("恢复配置守护")
			pauseAct.SetChecked(true)
			k.log("配置守护已暂停")
		} else {
			pauseAct.SetText("暂停配置守护")
			pauseAct.SetChecked(false)
			k.log("配置守护已恢复")
		}
	})
	_ = ni.ContextMenu().Actions().Add(pauseAct)

	autoAct := walk.NewAction()
	autoAct.SetText("开机自动启动")
	_ = autoAct.SetCheckable(true)
	autoAct.SetChecked(autostartEnabled())
	autoAct.Triggered().Attach(func() {
		on := !autostartEnabled()
		if err := setAutostart(on); err != nil {
			k.log("设置开机自启失败: %v", err)
			return
		}
		autoAct.SetChecked(on)
		if on {
			k.log("已开启开机自启")
		} else {
			k.log("已关闭开机自启")
		}
	})
	_ = ni.ContextMenu().Actions().Add(autoAct)

	_ = ni.ContextMenu().Actions().Add(walk.NewSeparatorAction())

	exitAct := walk.NewAction()
	exitAct.SetText("退出")
	exitAct.Triggered().Attach(func() {
		if k.exitFunc != nil {
			k.exitFunc()
		}
		_ = ni.Dispose()
		walk.App().Exit(0)
	})
	_ = ni.ContextMenu().Actions().Add(exitAct)

	// 单击托盘显示主界面
	ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			k.ui.mw.Show()
			k.ui.mw.SetFocus()
		}
	})
	return ni.SetVisible(true)
}
