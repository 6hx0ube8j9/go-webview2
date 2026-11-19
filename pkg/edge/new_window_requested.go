//go:build windows
// +build windows

package edge

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modshell32        = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteW = modshell32.NewProc("ShellExecuteW")
)

func OpenDefaultBrowser(hwnd uintptr, targetURL string) error {
	trimmed := strings.TrimSpace(targetURL)
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "http://") &&
		!strings.HasPrefix(lower, "https://") &&
		!strings.HasPrefix(lower, "mailto:") {
		return errors.New("blocked unsafe URL scheme")
	}

	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(trimmed)
	if err != nil {
		return err
	}

	ret, _, _ := procShellExecuteW.Call(
		hwnd,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(target)),
		0,
		0,
		1, // SW_SHOWNORMAL
	)
	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW failed with code %d", ret)
	}
	return nil
}

// ICoreWebView2NewWindowRequestedEventArgs

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
	r, _, _ := i.vtbl.GetURI.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(&uri)),
	)
	if int32(r) < 0 {
		return "", windows.Errno(r)
	}
	if uri == nil {
		return "", nil
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(uri))
	return windows.UTF16PtrToString(uri), nil
}

func (i *ICoreWebView2NewWindowRequestedEventArgs) PutHandled(handled bool) error {
	r, _, _ := i.vtbl.PutHandled.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(boolToInt(handled)),
	)
	if int32(r) < 0 {
		return windows.Errno(r)
	}
	return nil
}

// ICoreWebView2NewWindowRequestedEventHandler

type iCoreWebView2NewWindowRequestedEventHandlerImpl interface {
	_IUnknownImpl
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
	return this.impl.QueryInterface(refiid, object)
}

func _ICoreWebView2NewWindowRequestedEventHandlerIUnknownAddRef(this *iCoreWebView2NewWindowRequestedEventHandler) uintptr {
	return this.impl.AddRef()
}

func _ICoreWebView2NewWindowRequestedEventHandlerIUnknownRelease(this *iCoreWebView2NewWindowRequestedEventHandler) uintptr {
	return this.impl.Release()
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