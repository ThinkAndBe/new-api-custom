//go:build windows

package main

// win11style.go — Windows 11 风格化：暗色标题栏跟随系统、DWM 圆角、
// Segoe UI Variable 字体。直接走 syscall，不引额外依赖。

import (
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
)

const (
	dwmwaUseImmersiveDarkMode   = 20
	dwmwaWindowCornerPreference = 33
	dwmwcpRound                 = 2 // Win11 圆角
)

var (
	dwmapi               = syscall.NewLazyDLL("dwmapi.dll")
	procSetWindowAttr    = dwmapi.NewProc("DwmSetWindowAttribute")
	advapi32             = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")
	uxtheme              = syscall.NewLazyDLL("uxtheme.dll")
	procSetWindowTheme   = uxtheme.NewProc("SetWindowTheme")
)

// applyWin11Style 在窗口创建后调用：暗色标题栏跟随系统 + 圆角 + 现代字体。
func applyWin11Style(mw *walk.MainWindow) {
	hwnd := uintptr(mw.Handle())
	setDwmAttr(hwnd, dwmwaUseImmersiveDarkMode, boolToU32(shouldUseDark()))
	setDwmAttr(hwnd, dwmwaWindowCornerPreference, dwmwcpRound)
	if f, err := walk.NewFont("Segoe UI Variable Text", 10, 0); err == nil {
		mw.SetFont(f)
	}
}

// darkThemeControl 给单个控件挂 DarkMode_Explorer 视觉样式（按钮/单选/输入框的暗色渲染）。
func darkThemeControl(w walk.Widget) {
	theme, _ := syscall.UTF16PtrFromString("DarkMode_Explorer")
	procSetWindowTheme.Call(uintptr(w.Handle()), uintptr(unsafe.Pointer(theme)), 0)
}

func setDwmAttr(hwnd uintptr, attr uint32, value uint32) {
	v := value
	procSetWindowAttr.Call(hwnd, uintptr(attr), uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
}

func boolToU32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// shouldUseDark 读 HKCU\...\Themes\Personalize\AppsUseLightTheme 判断系统深色模式
func shouldUseDark() bool {
	subkey, _ := syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`)
	var hKey uintptr
	const (
		hkeyCurrentUser = 0x80000001
		keyQueryValue   = 0x0001
		regDWORD        = 4
	)
	ret, _, _ := procRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(subkey)), 0, keyQueryValue, uintptr(unsafe.Pointer(&hKey)))
	if ret != 0 {
		return false
	}
	defer procRegCloseKey.Call(hKey)

	name, _ := syscall.UTF16PtrFromString("AppsUseLightTheme")
	var data uint32
	size := uint32(unsafe.Sizeof(data))
	ret, _, _ = procRegQueryValueExW.Call(
		hKey,
		uintptr(unsafe.Pointer(name)),
		0,
		0, // lpType 传 NULL，不关心类型
		uintptr(unsafe.Pointer(&data)),
		uintptr(unsafe.Pointer(&size)),
	)
	if ret != 0 {
		return false
	}
	return data == 0
}
