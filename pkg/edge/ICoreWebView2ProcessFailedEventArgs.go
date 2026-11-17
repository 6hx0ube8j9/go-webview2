//go:build windows
// +build windows

package edge

import (
	"fmt"
	"strings"
	"unsafe"

	"github.com/jchv/go-webview2/internal/w32"
	"golang.org/x/sys/windows"
)

type CoreWebView2ProcessFailedKind uint32

const (
	CoreWebView2ProcessFailedKindBrowserProcessExited       CoreWebView2ProcessFailedKind = 0
	CoreWebView2ProcessFailedKindRenderProcessExited        CoreWebView2ProcessFailedKind = 1
	CoreWebView2ProcessFailedKindRenderProcessUnresponsive  CoreWebView2ProcessFailedKind = 2
	CoreWebView2ProcessFailedKindFrameRenderProcessExited   CoreWebView2ProcessFailedKind = 3
	CoreWebView2ProcessFailedKindUtilityProcessExited       CoreWebView2ProcessFailedKind = 4
	CoreWebView2ProcessFailedKindSandboxHelperProcessExited CoreWebView2ProcessFailedKind = 5
	CoreWebView2ProcessFailedKindGpuProcessExited           CoreWebView2ProcessFailedKind = 6
	CoreWebView2ProcessFailedKindPpapiPluginProcessExited   CoreWebView2ProcessFailedKind = 7
	CoreWebView2ProcessFailedKindPpapiBrokerProcessExited   CoreWebView2ProcessFailedKind = 8
	CoreWebView2ProcessFailedKindUnknownProcessExited       CoreWebView2ProcessFailedKind = 9
)

func (k CoreWebView2ProcessFailedKind) String() string {
	switch k {
	case CoreWebView2ProcessFailedKindBrowserProcessExited:
		return "BrowserProcessExited"
	case CoreWebView2ProcessFailedKindRenderProcessExited:
		return "RenderProcessExited"
	case CoreWebView2ProcessFailedKindRenderProcessUnresponsive:
		return "RenderProcessUnresponsive"
	case CoreWebView2ProcessFailedKindFrameRenderProcessExited:
		return "FrameRenderProcessExited"
	case CoreWebView2ProcessFailedKindUtilityProcessExited:
		return "UtilityProcessExited"
	case CoreWebView2ProcessFailedKindSandboxHelperProcessExited:
		return "SandboxHelperProcessExited"
	case CoreWebView2ProcessFailedKindGpuProcessExited:
		return "GpuProcessExited"
	case CoreWebView2ProcessFailedKindPpapiPluginProcessExited:
		return "PpapiPluginProcessExited"
	case CoreWebView2ProcessFailedKindPpapiBrokerProcessExited:
		return "PpapiBrokerProcessExited"
	default:
		return fmt.Sprintf("UnknownProcessExited(%d)", uint32(k))
	}
}

type CoreWebView2ProcessFailedReason uint32

const (
	CoreWebView2ProcessFailedReasonUnexpected     CoreWebView2ProcessFailedReason = 0
	CoreWebView2ProcessFailedReasonUnresponsive   CoreWebView2ProcessFailedReason = 1
	CoreWebView2ProcessFailedReasonTerminated     CoreWebView2ProcessFailedReason = 2
	CoreWebView2ProcessFailedReasonCrashed        CoreWebView2ProcessFailedReason = 3
	CoreWebView2ProcessFailedReasonLaunchFailed   CoreWebView2ProcessFailedReason = 4
	CoreWebView2ProcessFailedReasonOutOfMemory    CoreWebView2ProcessFailedReason = 5
	CoreWebView2ProcessFailedReasonProfileDeleted CoreWebView2ProcessFailedReason = 6
)

func (r CoreWebView2ProcessFailedReason) String() string {
	switch r {
	case CoreWebView2ProcessFailedReasonUnexpected:
		return "Unexpected"
	case CoreWebView2ProcessFailedReasonUnresponsive:
		return "Unresponsive"
	case CoreWebView2ProcessFailedReasonTerminated:
		return "Terminated"
	case CoreWebView2ProcessFailedReasonCrashed:
		return "Crashed"
	case CoreWebView2ProcessFailedReasonLaunchFailed:
		return "LaunchFailed"
	case CoreWebView2ProcessFailedReasonOutOfMemory:
		return "OutOfMemory"
	case CoreWebView2ProcessFailedReasonProfileDeleted:
		return "ProfileDeleted"
	default:
		return fmt.Sprintf("UnknownReason(%d)", uint32(r))
	}
}

// ProcessFailedInfo summarizes a WebView2 sub-process failure event.
type ProcessFailedInfo struct {
	Kind        CoreWebView2ProcessFailedKind
	Reason      CoreWebView2ProcessFailedReason
	ExitCode    int32
	Description string
	IsFatal     bool
}

func (p ProcessFailedInfo) String() string {
	var parts []string
	parts = append(parts, "kind="+p.Kind.String())
	if p.Reason != 0 || p.ExitCode != 0 {
		parts = append(parts, "reason="+p.Reason.String())
		code := uint32(p.ExitCode)
		if label := describeExitCode(code); label != "" {
			parts = append(parts, fmt.Sprintf("exit_code=0x%08X(%s)", code, label))
		} else {
			parts = append(parts, fmt.Sprintf("exit_code=0x%08X", code))
		}
	}
	if p.Description != "" {
		parts = append(parts, "desc="+p.Description)
	}
	return fmt.Sprintf("webview2 process failed (%s)", strings.Join(parts, ", "))
}

// IID_ICoreWebView2ProcessFailedEventArgs2: {4DAB9422-46FA-4C3E-A5D2-41D2071D6870}
var iidICoreWebView2ProcessFailedEventArgs2 = windows.GUID{
	Data1: 0x4dab9422,
	Data2: 0x46fa,
	Data3: 0x4c3e,
	Data4: [8]byte{0xa5, 0xd2, 0x41, 0xd2, 0x07, 0x1d, 0x68, 0x70},
}

type _ICoreWebView2ProcessFailedEventArgsVtbl struct {
	_IUnknownVtbl
	GetProcessFailedKind ComProc
}

type ICoreWebView2ProcessFailedEventArgs struct {
	vtbl *_ICoreWebView2ProcessFailedEventArgsVtbl
}

type _ICoreWebView2ProcessFailedEventArgs2Vtbl struct {
	_ICoreWebView2ProcessFailedEventArgsVtbl
	GetReason                     ComProc
	GetExitCode                   ComProc
	GetProcessDescription         ComProc
	GetFrameInfosForFailedProcess ComProc
}

type ICoreWebView2ProcessFailedEventArgs2 struct {
	vtbl *_ICoreWebView2ProcessFailedEventArgs2Vtbl
}

func (i *ICoreWebView2ProcessFailedEventArgs) GetProcessFailedKind() (CoreWebView2ProcessFailedKind, error) {
	var kind CoreWebView2ProcessFailedKind
	hr, _, _ := i.vtbl.GetProcessFailedKind.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(&kind)),
	)
	if int32(hr) < 0 {
		return 0, newHRESULTError("GetProcessFailedKind", hr, nil)
	}
	return kind, nil
}

// ExtractInfo reads ProcessFailedKind and queries ICoreWebView2ProcessFailedEventArgs2
// for Reason, ExitCode, and ProcessDescription when supported by the runtime.
func (i *ICoreWebView2ProcessFailedEventArgs) ExtractInfo() ProcessFailedInfo {
	kind, _ := i.GetProcessFailedKind()
	info := ProcessFailedInfo{
		Kind: kind,
		IsFatal: kind == CoreWebView2ProcessFailedKindBrowserProcessExited ||
			kind == CoreWebView2ProcessFailedKindRenderProcessExited,
	}

	var args2 *ICoreWebView2ProcessFailedEventArgs2
	hr, _, _ := i.vtbl.QueryInterface.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(&iidICoreWebView2ProcessFailedEventArgs2)),
		uintptr(unsafe.Pointer(&args2)),
	)
	if int32(hr) < 0 || args2 == nil {
		return info
	}
	defer args2.vtbl.Release.Call(uintptr(unsafe.Pointer(args2)))

	var reason CoreWebView2ProcessFailedReason
	if rHr, _, _ := args2.vtbl.GetReason.Call(
		uintptr(unsafe.Pointer(args2)),
		uintptr(unsafe.Pointer(&reason)),
	); int32(rHr) >= 0 {
		info.Reason = reason
	}

	var exitCode int32
	if eHr, _, _ := args2.vtbl.GetExitCode.Call(
		uintptr(unsafe.Pointer(args2)),
		uintptr(unsafe.Pointer(&exitCode)),
	); int32(eHr) >= 0 {
		info.ExitCode = exitCode
	}

	var descPtr *uint16
	if dHr, _, _ := args2.vtbl.GetProcessDescription.Call(
		uintptr(unsafe.Pointer(args2)),
		uintptr(unsafe.Pointer(&descPtr)),
	); int32(dHr) >= 0 && descPtr != nil {
		info.Description = w32.Utf16PtrToString(descPtr)
		windows.CoTaskMemFree(unsafe.Pointer(descPtr))
	}

	return info
}