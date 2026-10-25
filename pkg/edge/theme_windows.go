package edge

import (
	"sync/atomic"
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
)

var (
	isDarkCache int32
	currentClassColor uint32 = 0xFFFFFFFF 
)

func init() {
	if err := procSetClassLongPtrW.Find(); err != nil {
		procSetClassLongPtrW = moduser32.NewProc("SetClassLongW")
	}
	RefreshSystemThemeCache()
}

func RefreshSystemThemeCache() (isDark bool, changed bool) {
	k, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		registry.QUERY_VALUE,
	)
	newDark := false
	if err == nil {
		val, _, errVal := k.GetIntegerValue("AppsUseLightTheme")
		k.Close()
		if errVal == nil && val == 0 {
			newDark = true
		}
	}

	oldDark := atomic.LoadInt32(&isDarkCache) == 1
	var newVal int32 = 0
	if newDark {
		newVal = 1
	}

	atomic.StoreInt32(&isDarkCache, newVal)
	return newDark, oldDark != newDark
}

func IsSystemDarkTheme() bool {
	return atomic.LoadInt32(&isDarkCache) == 1
}

func GetThemeColor(isDark bool) uint32 {
	if isDark {
		return DarkThemeColor
	}
	return LightThemeColor
}

func CreateThemeBrush(isDark bool) uintptr {
	brush, _, _ := procCreateSolidBrush.Call(uintptr(GetThemeColor(isDark)))
	return brush
}

func SyncWindowClassBrush(hwnd uintptr, isDark bool) {
	targetColor := GetThemeColor(isDark)
	if currentClassColor == targetColor {
		return
	}

	newBrush := CreateThemeBrush(isDark)
	oldBrush, _, _ := procSetClassLongPtrW.Call(hwnd, gclpHbrBackground, newBrush)
	if oldBrush != 0 && oldBrush != newBrush {
		_, _, _ = procDeleteObject.Call(oldBrush)
	}
	currentClassColor = targetColor
}

func SetWindowDarkMode(hwnd uintptr, isDark bool) {
	var darkMode int32 = 0
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

	brush := CreateThemeBrush(IsSystemDarkTheme())
	_, _, _ = procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
	_, _, _ = procDeleteObject.Call(brush)
}

func HandleThemeChange(hwnd uintptr) {
	isDark, _ := RefreshSystemThemeCache()
	SetWindowDarkMode(hwnd, isDark)
	SyncWindowClassBrush(hwnd, isDark)
	_, _, _ = procInvalidateRect.Call(hwnd, 0, 1)
}
