//go:build windows
// +build windows

package edge

import (
	"unsafe"

	"github.com/jchv/go-webview2/internal/w32"
	"golang.org/x/sys/windows"
)

var (
	iidIUnknown                        = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidICoreWebView2EnvironmentOptions = windows.GUID{Data1: 0x2FDE08A8, Data2: 0x1E9A, Data3: 0x4766, Data4: [8]byte{0x8C, 0x05, 0x95, 0xA9, 0xCE, 0xB9, 0xD1, 0xC5}}
)

// allocCoTaskMemString allocates a UTF-16 string in COM task memory.
func allocCoTaskMemString(str string) (*uint16, uintptr) {
	utf16Chars, err := windows.UTF16FromString(str)
	if err != nil {
		return nil, 0x80004005 // E_FAIL
	}
	if len(utf16Chars) > 1<<20 {
		return nil, 0x80070057 // E_INVALIDARG
	}

	byteLen := uintptr(len(utf16Chars) * 2)
	mem, _, _ := w32.Ole32CoTaskMemAlloc.Call(byteLen)
	if mem == 0 {
		return nil, 0x8007000E // E_OUTOFMEMORY
	}

	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&mem))
	dst := (*[1 << 20]uint16)(ptr)[:len(utf16Chars):len(utf16Chars)]
	copy(dst, utf16Chars)

	return (*uint16)(ptr), 0 // S_OK
}

type CoreWebView2EnvironmentOptions struct {
	vtbl           *iCoreWebView2EnvironmentOptionsVtbl
	additionalArgs string
}

type iCoreWebView2EnvironmentOptionsVtbl struct {
	_IUnknownVtbl
	GetAdditionalBrowserArguments             ComProc
	PutAdditionalBrowserArguments             ComProc
	GetLanguage                               ComProc
	PutLanguage                               ComProc
	GetTargetCompatibleBrowserVersion         ComProc
	PutTargetCompatibleBrowserVersion         ComProc
	GetAllowSingleSignOnUsingOSPrimaryAccount ComProc
	PutAllowSingleSignOnUsingOSPrimaryAccount ComProc
}

var environmentOptionsVtbl = iCoreWebView2EnvironmentOptionsVtbl{
	_IUnknownVtbl: _IUnknownVtbl{
		QueryInterface: NewComProc(func(this unsafe.Pointer, riid *windows.GUID, ppvObject *unsafe.Pointer) uintptr {
			if ppvObject == nil {
				return 0x80004003 // E_POINTER
			}
			if riid == nil {
				*ppvObject = nil
				return 0x80070057 // E_INVALIDARG
			}
			if *riid == iidIUnknown || *riid == iidICoreWebView2EnvironmentOptions {
				*ppvObject = this
				return 0 // S_OK
			}
			*ppvObject = nil
			return 0x80004002 // E_NOINTERFACE
		}),
		AddRef: NewComProc(func(this unsafe.Pointer) uintptr {
			return 1
		}),
		Release: NewComProc(func(this unsafe.Pointer) uintptr {
			return 1
		}),
	},

	GetAdditionalBrowserArguments: NewComProc(func(this unsafe.Pointer, value **uint16) uintptr {
		if value == nil {
			return 0x80004003 // E_POINTER
		}
		opts := (*CoreWebView2EnvironmentOptions)(this)
		if opts.additionalArgs == "" {
			*value = nil
			return 0 // S_OK
		}
		ptr, hr := allocCoTaskMemString(opts.additionalArgs)
		if hr != 0 {
			return hr
		}
		*value = ptr
		return 0 // S_OK
	}),
	PutAdditionalBrowserArguments: NewComProc(func(this unsafe.Pointer, value *uint16) uintptr {
		return 0 // S_OK
	}),

	GetLanguage: NewComProc(func(this unsafe.Pointer, value **uint16) uintptr {
		if value == nil {
			return 0x80004003 // E_POINTER
		}
		*value = nil
		return 0 // S_OK
	}),
	PutLanguage: NewComProc(func(this unsafe.Pointer, value *uint16) uintptr {
		return 0 // S_OK
	}),

	// TargetCompatibleBrowserVersion (GA baseline required by WebView2Loader)
	GetTargetCompatibleBrowserVersion: NewComProc(func(this unsafe.Pointer, value **uint16) uintptr {
		if value == nil {
			return 0x80004003 // E_POINTER
		}
		ptr, hr := allocCoTaskMemString("86.0.616.0")
		if hr != 0 {
			return hr
		}
		*value = ptr
		return 0 // S_OK
	}),
	PutTargetCompatibleBrowserVersion: NewComProc(func(this unsafe.Pointer, value *uint16) uintptr {
		return 0 // S_OK
	}),

	GetAllowSingleSignOnUsingOSPrimaryAccount: NewComProc(func(this unsafe.Pointer, allow *int32) uintptr {
		if allow == nil {
			return 0x80004003 // E_POINTER
		}
		*allow = 0 // FALSE
		return 0   // S_OK
	}),
	PutAllowSingleSignOnUsingOSPrimaryAccount: NewComProc(func(this unsafe.Pointer, allow int32) uintptr {
		return 0 // S_OK
	}),
}

func NewCoreWebView2EnvironmentOptions(additionalArgs string) *CoreWebView2EnvironmentOptions {
	return &CoreWebView2EnvironmentOptions{
		vtbl:           &environmentOptionsVtbl,
		additionalArgs: additionalArgs,
	}
}