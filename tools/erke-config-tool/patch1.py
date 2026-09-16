import io
p = 'keeper.go'
s = io.open(p, encoding='utf-8').read()
old = '''	scanAct := walk.NewAction()
	scanAct.SetText("扫描并上传项目/技能")
	scanAct.Triggered().Attach(func() { go k.runScanAndUpload() })
	_ = ni.ContextMenu().Actions().Add(scanAct)'''
new = '''	scanAct := walk.NewAction()
	scanAct.SetText("扫描上报项目/技能清单")
	scanAct.Triggered().Attach(func() { go k.runReportInventory() })
	_ = ni.ContextMenu().Actions().Add(scanAct)'''
assert old in s
s = s.replace(old, new)
io.open(p, 'w', encoding='utf-8', newline='').write(s)
print('keeper ok')
