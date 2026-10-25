//go:build windows
// +build windows

package edge

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modshell32        = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteW = modshell32.NewProc("ShellExecuteW")
)

func openDefaultBrowser(targetURL string) {
	verb, _ := windows.UTF16PtrFromString("open")
	target, _ := windows.UTF16PtrFromString(targetURL)
	_, _, _ = procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(target)),
		0,
		0,
		1, // SW_SHOWNORMAL
	)
}

// ==================== ICoreWebView2NewWindowRequestedEventArgs ====================

type iCoreWebView2NewWindowRequestedEventArgsVtbl struct {
	_IUnknownVtbl
	GetURI             ComProc
	PutNewWindow       ComProc
	GetNewWindow       ComProc
	PutHandled         ComProc
	GetHandled         ComProc
	GetIsUserInitiated ComProc
	GetDeferral        ComProc
	GetWindowFeatures  ComProc
}

type ICoreWebView2NewWindowRequestedEventArgs struct {
	vtbl *iCoreWebView2NewWindowRequestedEventArgsVtbl
}

func (i *ICoreWebView2NewWindowRequestedEventArgs) GetUri() (string, error) {
	var uri *uint16
	_, _, err := i.vtbl.GetURI.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(&uri)),
	)
	if err != windows.ERROR_SUCCESS {
		return "", err
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(uri))
	return windows.UTF16PtrToString(uri), nil
}

func (i *ICoreWebView2NewWindowRequestedEventArgs) PutHandled(handled bool) error {
	var h int32 = 0
	if handled {
		h = 1
	}
	_, _, err := i.vtbl.PutHandled.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(h),
	)
	if err != windows.ERROR_SUCCESS {
		return err
	}
	return nil
}

// ==================== ICoreWebView2NewWindowRequestedEventHandler ====================

type iCoreWebView2NewWindowRequestedEventHandlerImpl interface {
	NewWindowRequested(sender *ICoreWebView2, args *ICoreWebView2NewWindowRequestedEventArgs) uintptr
}

type iCoreWebView2NewWindowRequestedEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type iCoreWebView2NewWindowRequestedEventHandler struct {
	vtbl *iCoreWebView2NewWindowRequestedEventHandlerVtbl
	impl iCoreWebView2NewWindowRequestedEventHandlerImpl
}

func _ICoreWebView2NewWindowRequestedEventHandlerIUnknownQueryInterface(this *iCoreWebView2NewWindowRequestedEventHandler, refiid, object uintptr) uintptr {
	return 0
}

func _ICoreWebView2NewWindowRequestedEventHandlerIUnknownAddRef(this *iCoreWebView2NewWindowRequestedEventHandler) uintptr {
	return 1
}

func _ICoreWebView2NewWindowRequestedEventHandlerIUnknownRelease(this *iCoreWebView2NewWindowRequestedEventHandler) uintptr {
	return 1
}

func _ICoreWebView2NewWindowRequestedEventHandlerInvoke(this *iCoreWebView2NewWindowRequestedEventHandler, sender *ICoreWebView2, args *ICoreWebView2NewWindowRequestedEventArgs) uintptr {
	return this.impl.NewWindowRequested(sender, args)
}

var iCoreWebView2NewWindowRequestedEventHandlerFn = iCoreWebView2NewWindowRequestedEventHandlerVtbl{
	_IUnknownVtbl{
		NewComProc(_ICoreWebView2NewWindowRequestedEventHandlerIUnknownQueryInterface),
		NewComProc(_ICoreWebView2NewWindowRequestedEventHandlerIUnknownAddRef),
		NewComProc(_ICoreWebView2NewWindowRequestedEventHandlerIUnknownRelease),
	},
	NewComProc(_ICoreWebView2NewWindowRequestedEventHandlerInvoke),
}

func newICoreWebView2NewWindowRequestedEventHandler(impl iCoreWebView2NewWindowRequestedEventHandlerImpl) *iCoreWebView2NewWindowRequestedEventHandler {
	return &iCoreWebView2NewWindowRequestedEventHandler{
		vtbl: &iCoreWebView2NewWindowRequestedEventHandlerFn,
		impl: impl,
	}
}
