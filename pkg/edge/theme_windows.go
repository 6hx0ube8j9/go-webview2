//go:build windows
// +build windows

package edge

import (
	"unsafe"

	"github.com/jchv/go-webview2/internal/w32"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	gclpHbrBackground         = ^uintptr(9) // GCLP_HBRBACKGROUND (-10)
	dwmwaImmersiveDarkMode    = 20
	dwmwaImmersiveDarkModeOld = 19

	DarkThemeColor  uint32 = 0x00202020 // BGR: #202020
	LightThemeColor uint32 = 0x00FFFFFF // BGR: #FFFFFF

	swpFrameChangedFlags = 0x0001 | 0x0002 | 0x0004 | 0x0010 | 0x0020 
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
	procSetClassLongPtrW      = moduser32.NewProc("SetClassLongPtrW")
	procGetClassLongPtrW      = moduser32.NewProc("GetClassLongPtrW")
	procSetWindowPos          = moduser32.NewProc("SetWindowPos")
)

var currentBrush uintptr

func init() {
	if err := procSetClassLongPtrW.Find(); err != nil {
		procSetClassLongPtrW = moduser32.NewProc("SetClassLongW")
		procGetClassLongPtrW = moduser32.NewProc("GetClassLongW")
	}
}

func CreateThemeBrush(isDark bool) uintptr {
	color := LightThemeColor
	if isDark {
		color = DarkThemeColor
	}
	brush, _, _ := procCreateSolidBrush.Call(uintptr(color))
	return brush
}

func UpdateWindowClassBrush(hwnd uintptr, isDark bool) {
	newBrush := CreateThemeBrush(isDark)
	oldBrush, _, _ := procSetClassLongPtrW.Call(hwnd, gclpHbrBackground, newBrush)
	if oldBrush != 0 && oldBrush != newBrush {
		_, _, _ = procDeleteObject.Call(oldBrush)
	}
	currentBrush = newBrush
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

	brush := currentBrush
	if brush == 0 {
		brush = CreateThemeBrush(false)
		defer procDeleteObject.Call(brush)
	}
	_, _, _ = procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
}

func InvalidateWindow(hwnd uintptr) {
	_, _, _ = procInvalidateRect.Call(hwnd, 0, 1)
}

func IsSystemDarkMode() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	val, _, err := k.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return false
	}
	return val == 0
}

func RedrawWindowFrame(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	_, _, _ = procSetWindowPos.Call(
		hwnd, 0,
		0, 0, 0, 0,
		uintptr(swpFrameChangedFlags),
	)
}

func ApplyTheme(hwnd uintptr, isDark bool) {
	SetWindowDarkMode(hwnd, isDark)
	UpdateWindowClassBrush(hwnd, isDark)
	InvalidateWindow(hwnd)
	RedrawWindowFrame(hwnd)
}
