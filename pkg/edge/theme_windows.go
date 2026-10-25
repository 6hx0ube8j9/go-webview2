//go:build windows
// +build windows

package edge

import (
	"sync/atomic"
	"unsafe"

	"github.com/jchv/go-webview2/internal/w32"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	dwmwaImmersiveDarkMode    = 20
	dwmwaImmersiveDarkModeOld = 19

	DarkThemeColor  uint32 = 0x00202020
	LightThemeColor uint32 = 0x00FFFFFF
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
	RefreshSystemThemeCache()
}

func RefreshSystemThemeCache() bool {
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

func IsSystemDarkTheme() bool {
	return atomic.LoadInt32(&isDarkCache) == 1
}

func GetThemeColor() uint32 {
	if IsSystemDarkTheme() {
		return DarkThemeColor
	}
	return LightThemeColor
}

func CreateThemeBrush() uintptr {
	brush, _, _ := procCreateSolidBrush.Call(uintptr(GetThemeColor()))
	return brush
}

func SetWindowDarkMode(hwnd uintptr, isDark bool) {
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

func EraseWindowBackground(hwnd uintptr, hdc uintptr) {
	var rc w32.Rect
	_, _, _ = procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))

	brush := CreateThemeBrush()
	_, _, _ = procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
	_, _, _ = procDeleteObject.Call(brush)
}

func HandleThemeChange(hwnd uintptr) {
	isDark := RefreshSystemThemeCache()
	SetWindowDarkMode(hwnd, isDark)
	_, _, _ = procInvalidateRect.Call(hwnd, 0, 1)
}
