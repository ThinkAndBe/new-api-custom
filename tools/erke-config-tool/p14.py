import io

p = 'keeper.go'
s = io.open(p, encoding='utf-8').read()

# ============ 1) 守护循环：去掉进程门禁，保留退出快速补写 ============
old = '''	prevRunning := map[string]bool{}
	for {
		if !k.isPaused() {
			for _, product := range []string{"workbuddy", "codebuddy"} {
				name := "WorkBuddy"
				proc := productProcessName(product)
				if product == "codebuddy" {
					name = "CodeBuddy"
				}
				running := proc != "" && isProcessRunning(proc)
				if running {
					// 客户端运行中不写文件：部分版本退出时会用内存状态回写
					// models.json，运行中写入会被冲掉（写也白写）
					prevRunning[product] = true
					continue
				}
				wasRunning := prevRunning[product]
				prevRunning[product] = false
				n, err := repairOnce(product)
				if err != nil {
					k.log("守护检查 %s 失败: %v", name, err)
				} else if n > 0 {
					if wasRunning {
						k.log("检测到 %s 已退出，立即补写 %d 个模型（下次启动生效）", name, n)
					} else {
						k.log("检测到 %s 配置被重置，已自动补写 %d 个模型", name, n)
					}
				}
			}
		}
		time.Sleep(keeperInterval)
	}
}'''
new = '''	prevRunning := map[string]bool{}
	first := true
	for {
		if !k.isPaused() {
			// 注意：不做「运行中跳过」——WorkBuddy 有多个常驻后台进程，
			// 进程检测会永远显示运行中导致守护失效。改为始终比对补写：
			// 客户端退出回写冲掉我们的条目后，下一周期（≤10s）自动补回。
			// 检测到进程全部退出的首个周期额外快速补写一次。
			for _, product := range []string{"workbuddy", "codebuddy"} {
				name := "WorkBuddy"
				if product == "codebuddy" {
					name = "CodeBuddy"
				}
				wasRunning := prevRunning[product]
				running := isProcessRunning(productProcessName(product))
				prevRunning[product] = running
				n, err := repairOnce(product)
				if err != nil {
					k.logSilent("守护检查 %s 失败: %v", name, err)
				} else if n > 0 {
					if first {
						k.logSilent("守护启动补写 %s %d 个模型", name, n)
					} else if wasRunning && !running {
						k.logSilent("检测到 %s 已退出，补写 %d 个模型", name, n)
					} else {
						k.logSilent("检测到 %s 配置被重置，自动补写 %d 个模型", name, n)
					}
				}
			}
			first = false
		}
		time.Sleep(keeperInterval)
	}
}'''
assert old in s
s = s.replace(old, new, 1)

# ============ 2) 日志静默化：自动事件走 logSilent（仅文件），手动操作才刷 UI ============
old2 = '''func (k *keeper) log(format string, args ...interface{}) {
	msg := time.Now().Format("15:04:05") + " " + fmt.Sprintf(format, args...)
	k.mu.Lock()
	k.lastLog = msg
	k.mu.Unlock()
	k.ui.setStatus("["+msg+"]", true)
}'''
new2 = '''// log 手动操作日志：文件 + 主界面状态（用户主动动作才刷 UI，避免弹窗骚扰）
func (k *keeper) log(format string, args ...interface{}) {
	msg := time.Now().Format("15:04:05") + " " + fmt.Sprintf(format, args...)
	common_log(msg)
	k.ui.setStatus("["+msg+"]", true)
}

// logSilent 自动事件日志：仅写文件，不打扰界面
func (k *keeper) logSilent(format string, args ...interface{}) {
	common_log("[auto] " + fmt.Sprintf(format, args...))
}'''
assert old2 in s
s = s.replace(old2, new2, 1)

# 上次那版遗留的 lastLog 字段引用清理
s = s.replace('''type keeper struct {
	mu     sync.Mutex
	paused bool
	ni     *walk.NotifyIcon
	ui     *appUI
	// exiting 由托盘「退出」置位，窗口 Closing 据此放行真正退出
	exiting bool
	exitFunc func()
}''', '''type keeper struct {
	mu     sync.Mutex
	paused bool
	ni     *walk.NotifyIcon
	ui     *appUI
	// exiting 由托盘「退出」置位，窗口 Closing 据此放行真正退出
	exiting bool
	exitFunc func()
}''')

# ============ 3) 收集总开关（默认关）+ 托盘菜单 ============
old3 = '''	diagAct := walk.NewAction()'''
new3 = '''	collectAct := walk.NewAction()
	collectAct.SetText("项目/技能收集")
	collectAct.Checkable = true
	collectAct.SetChecked(collectEnabled())
	collectAct.Triggered().Attach(func() {
		on := !collectEnabled()
		setCollectEnabled(on)
		_ = collectAct.SetChecked(on)
		if on {
			k.log("已开启项目/技能收集（后台静默上报清单与执行拉取任务）")
		} else {
			k.log("已关闭项目/技能收集（不再扫描与上报）")
		}
	})
	_ = ni.ContextMenu().Actions().Add(collectAct)

	diagAct := walk.NewAction()'''
assert old3 in s
s = s.replace(old3, new3, 1)

# ============ 4) 手动补写提示改回直接执行（不再拦运行中）============
old4 = '''// repairNow 托盘「立即补写」：客户端运行中会被其退出回写覆盖，提示先退出
func (k *keeper) repairNow() {
	for _, product := range []string{"workbuddy", "codebuddy"} {
		name := "WorkBuddy"
		proc := productProcessName(product)
		if product == "codebuddy" {
			name = "CodeBuddy"
		}
		if proc != "" && isProcessRunning(proc) {
			k.log("%s 正在运行：现在补写会在它退出时被覆盖。请先完全退出 %s（托盘也退），再点立即补写；守护也会在检测到退出后自动补写", name, name)
			return
		}
	}
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
		k.log("手动补写完成，共 %d 个模型（启动客户端后生效）", total)
	} else {
		k.log("配置完整，无需补写（若客户端里仍看不到模型，请用托盘「问题诊断上报」）")
	}
}'''
new4 = '''// repairNow 托盘「立即补写」：始终执行（守护与补写不做进程门禁——
// WorkBuddy 后台常驻进程导致"运行中"判断永远为真，曾致守护失效）
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
		k.log("手动补写完成，共 %d 个模型（重启客户端后生效）", total)
	} else {
		k.log("配置文件完整；若客户端里仍看不到模型，请用托盘「问题诊断上报」")
	}
}'''
assert old4 in s
s = s.replace(old4, new4, 1)

io.open(p, 'w', encoding='utf-8', newline='').write(s)
print('keeper v3.4 ok')
