//go:build windows
// +build windows

package edge

import (
	"sync"
	"unsafe"

	"github.com/jchv/go-webview2/internal/w32"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	gclpHbrBackground         = ^uintptr(9) // GCLP_HBRBACKGROUND (-10)
	dwmwaImmersiveDarkMode    = 20
	dwmwaImmersiveDarkModeOld = 19

	darkThemeColor  uint32 = 0x00202020 // BGR: #202020
	lightThemeColor uint32 = 0x00FFFFFF // BGR: #FFFFFF
)

var (
	modgdi32                  = windows.NewLazySystemDLL("gdi32.dll")
	procCreateSolidBrush      = modgdi32.NewProc("CreateSolidBrush")
	moddwmapi                 = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttribute = moddwmapi.NewProc("DwmSetWindowAttribute")

	setClassLongOnce sync.Once
	procSetClassLong *windows.LazyProc

	themeBrushesOnce sync.Once
	darkThemeBrush   uintptr
	lightThemeBrush  uintptr
)

func themeBackgroundColor(isDark bool) COREWEBVIEW2_COLOR {
	if isDark {
		return COREWEBVIEW2_COLOR{A: 255, R: 32, G: 32, B: 32}
	}
	return COREWEBVIEW2_COLOR{A: 255, R: 255, G: 255, B: 255}
}

func getSetClassLongProc() *windows.LazyProc {
	setClassLongOnce.Do(func() {
		if err := w32.User32SetClassLongPtrW.Find(); err == nil {
			procSetClassLong = w32.User32SetClassLongPtrW
		} else {
			procSetClassLong = w32.User32SetClassLongW
		}
	})
	return procSetClassLong
}

// ThemeBrush returns a cached process-lifetime HBRUSH for the given theme.
// Callers must not delete the returned brush.
func ThemeBrush(isDark bool) uintptr {
	themeBrushesOnce.Do(func() {
		darkThemeBrush, _, _ = procCreateSolidBrush.Call(uintptr(darkThemeColor))
		lightThemeBrush, _, _ = procCreateSolidBrush.Call(uintptr(lightThemeColor))
	})
	if isDark {
		return darkThemeBrush
	}
	return lightThemeBrush
}

func updateWindowClassBrush(hwnd uintptr, isDark bool) {
	if hwnd == 0 {
		return
	}
	_, _, _ = getSetClassLongProc().Call(hwnd, gclpHbrBackground, ThemeBrush(isDark))
}

func SetWindowDarkMode(hwnd uintptr, isDark bool) {
	if hwnd == 0 {
		return
	}
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

func EraseWindowBackground(hwnd, hdc uintptr, isDark bool) {
	if hwnd == 0 || hdc == 0 {
		return
	}
	var rc w32.Rect
	_, _, _ = w32.User32GetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	_, _, _ = w32.User32FillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), ThemeBrush(isDark))
}

func invalidateWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	_, _, _ = w32.User32InvalidateRect.Call(hwnd, 0, 1)
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

func redrawWindowFrame(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	_, _, _ = w32.User32SetWindowPos.Call(
		hwnd, 0,
		0, 0, 0, 0,
		w32.SWPNoSize|w32.SWPNoMove|w32.SWPNoZOrder|w32.SWPNoActivate|w32.SWPFrameChanged,
	)
}

func ApplyWindowTheme(hwnd uintptr, isDark bool) {
	if hwnd == 0 {
		return
	}
	SetWindowDarkMode(hwnd, isDark)
	updateWindowClassBrush(hwnd, isDark)
	invalidateWindow(hwnd)
	redrawWindowFrame(hwnd)
}