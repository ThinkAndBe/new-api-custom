//go:build windows

package main

// appicon.go — 任务栏/标题栏图标。
//
// 背景：exe 的资源里一直有图标（rsrc 写入的 RT_GROUP_ICON，组 id = 2，含 16/32/48/64/256 五个尺寸），
// 但 walk 的窗口默认不设置 WM_SETICON —— 任务栏按钮就会显示成空白图标（资源管理器里能看到 exe 图标，
// 只有运行中的窗口是白的）。这里显式把 exe 内的图标设给窗口，大小图标一并设置。
//
// 资源 id 由 rsrc 生成时的顺序决定（当前：1=MANIFEST，2=ICON 组），所以按候选列表逐个尝试，
// 最后兜底系统默认应用图标 —— 保证任务栏永远不是空白。

import (
	"github.com/lxn/walk"
)

func appIcon() *walk.Icon {
	// 首选资源图标组（rsrc -ico 写入的 RT_GROUP_ICON）
	for _, id := range []int{2, 1, 3, 4} {
		if ic, err := walk.NewIconFromResourceId(id); err == nil {
			return ic
		}
	}
	// 兜底：系统默认应用图标（宁可显示通用图标，也不要空白）
	return walk.IconApplication()
}
