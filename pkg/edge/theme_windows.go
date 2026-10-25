//go:build windows
// +build windows

package webview2

import (
	"sync/atomic"
	"unsafe"

	"github.com/jchv/go-webview2/internal/w32"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	wmEraseBkgnd    = 0x0014
	wmSettingChange = 0x001A
	wmThemeChanged  = 0x031A

	dwmwaImmersiveDarkMode    = 20
	dwmwaImmersiveDarkModeOld = 19

	darkThemeColor  uint32 = 0x00202020 // BGR: #202020
	lightThemeColor uint32 = 0x00FFFFFF // BGR: #FFFFFF
)

var (
	modgdi32                  = windows.NewLazySystemDLL("gdi32.dll")
	procCreateSolidBrush      = modgdi32.NewProc("CreateSolidBrush")
	procDeleteObject          = modgdi32.NewProc("DeleteObject")
	moddwmapi                 = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttribute = moddwmapi.NewProc("DwmSetWindowAttribute")
	moduser32                 = windows.NewLazySystemDLL("user32.dll")
	procGetClientRect         = moduser32.NewProc("GetClientRect")
	procFillRect              = moduser32.NewProc("FillRect")
	procInvalidateRect        = moduser32.NewProc("InvalidateRect")
)

var isDarkCache int32

func init() {
	refreshSystemThemeCache()
}

// refreshSystemThemeCache 读取注册表并刷新内存缓存
func refreshSystemThemeCache() bool {
	k, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		atomic.StoreInt32(&isDarkCache, 0)
		return false
	}
	defer k.Close()

	val, _, err := k.GetIntegerValue("AppsUseLightTheme")
	if err != nil || val != 0 {
		atomic.StoreInt32(&isDarkCache, 0)
		return false
	}

	atomic.StoreInt32(&isDarkCache, 1)
	return true
}

func isSystemDarkTheme() bool {
	return atomic.LoadInt32(&isDarkCache) == 1
}

// applyWindowDarkMode 设置窗口标题栏的沉浸式深色模式
func applyWindowDarkMode(hwnd uintptr, isDark bool) {
	var darkMode int32
	if isDark {
		darkMode = 1
	}

	ret, _, _ := procDwmSetWindowAttribute.Call(
		hwnd,
		dwmwaImmersiveDarkMode,
		uintptr(unsafe.Pointer(&darkMode)),
		unsafe.Sizeof(darkMode),
	)
	if ret != 0 {
		_, _, _ = procDwmSetWindowAttribute.Call(
			hwnd,
			dwmwaImmersiveDarkModeOld,
			uintptr(unsafe.Pointer(&darkMode)),
			unsafe.Sizeof(darkMode),
		)
	}
}

// eraseWindowBackground 纯内存无闪烁填充客户区背景
func eraseWindowBackground(hwnd uintptr, hdc uintptr) {
	var rc w32.Rect
	_, _, _ = procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))

	color := lightThemeColor
	if isSystemDarkTheme() {
		color = darkThemeColor
	}

	brush, _, _ := procCreateSolidBrush.Call(uintptr(color))
	_, _, _ = procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
	_, _, _ = procDeleteObject.Call(brush)
}

// handleThemeChange 系统主题变更时的响应处理
func handleThemeChange(hwnd uintptr) {
	isDark := refreshSystemThemeCache()
	applyWindowDarkMode(hwnd, isDark)
	_, _, _ = procInvalidateRect.Call(hwnd, 0, 1)
}
