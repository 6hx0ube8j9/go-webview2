//go:build windows
// +build windows

package edge

import (
	"fmt"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// HRESULTError represents a structured Win32/COM HRESULT failure from WebView2.
type HRESULTError struct {
	Stage   string
	Code    uint32
	Message string
	Err     error
}

func (e *HRESULTError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("webview2 %s failed (HRESULT 0x%08X: %s)", e.Stage, e.Code, e.Message)
	}
	if e.Err != nil {
		if e.Code == 0 {
			return fmt.Sprintf("webview2 %s failed: %v", e.Stage, e.Err)
		}
		return fmt.Sprintf("webview2 %s failed (HRESULT 0x%08X: %v)", e.Stage, e.Code, e.Err)
	}
	return fmt.Sprintf("webview2 %s failed (HRESULT 0x%08X)", e.Stage, e.Code)
}

func (e *HRESULTError) Unwrap() error {
	return e.Err
}

func newHRESULTError(stage string, hr uintptr, cause error) error {
	code := uint32(hr)
	if code == 0 && cause != nil {
		if errno, ok := cause.(syscall.Errno); ok && errno != 0 {
			if int32(errno) > 0 {
				code = (uint32(errno) & 0x0000FFFF) | 0x80070000
			} else {
				code = uint32(errno)
			}
		}
	}
	return &HRESULTError{
		Stage:   stage,
		Code:    code,
		Message: describeHRESULT(code),
		Err:     cause,
	}
}

func describeHRESULT(code uint32) string {
	switch code {
	case 0x80070002:
		return "ERROR_FILE_NOT_FOUND: WebView2 runtime or core binary not found"
	case 0x80070003:
		return "ERROR_PATH_NOT_FOUND: WebView2 runtime or data path does not exist"
	case 0x80070005:
		return "E_ACCESSDENIED: access denied to WebView2 data path or file in use"
	case 0x8007000E:
		return "E_OUTOFMEMORY: out of memory"
	case 0x80070057:
		return "E_INVALIDARG: invalid WebView2 initialization argument"
	case 0x8007139F:
		return "ERROR_INVALID_STATE: conflicting browser arguments for the same data path or invalid state"
	case 0x80010106:
		return "RPC_E_CHANGED_MODE: COM apartment mode (STA/MTA) mismatch on current thread"
	case 0x80040154:
		return "REGDB_E_CLASSNOTREG: WebView2 COM class not registered"
	case 0x80004002:
		return "E_NOINTERFACE: COM interface not supported by installed WebView2 runtime"
	case 0x80004005:
		return "E_FAIL: unspecified WebView2 failure"
	}

	var win32Code uint32
	if (code & 0xFFFF0000) == 0x80070000 {
		win32Code = code & 0x0000FFFF
	} else {
		win32Code = code
	}

	var buf [512]uint16
	n, err := windows.FormatMessage(
		windows.FORMAT_MESSAGE_FROM_SYSTEM|windows.FORMAT_MESSAGE_IGNORE_INSERTS,
		0,
		win32Code,
		0,
		buf[:],
		nil,
	)
	if err == nil && n > 0 {
		return strings.TrimSpace(windows.UTF16ToString(buf[:n]))
	}
	return ""
}

func describeExitCode(code uint32) string {
	switch code {
	case 0xC0000005:
		return "STATUS_ACCESS_VIOLATION"
	case 0xC000001D:
		return "STATUS_ILLEGAL_INSTRUCTION"
	case 0xC00000FD:
		return "STATUS_STACK_OVERFLOW"
	case 0xC000013A:
		return "STATUS_CONTROL_C_EXIT"
	case 0xC0000409:
		return "STATUS_STACK_BUFFER_OVERRUN"
	case 0x80000003:
		return "STATUS_BREAKPOINT"
	case 0xE0000008:
		return "CHROMIUM_OUT_OF_MEMORY"
	case 0x40010004:
		return "DBG_TERMINATE_PROCESS"
	default:
		return ""
	}
}