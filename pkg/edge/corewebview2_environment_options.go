package edge

import (
	"unsafe"
	"os"
	"fmt"

	"golang.org/x/sys/windows"
)

var (
	modOle32           = windows.NewLazySystemDLL("ole32.dll")
	procCoTaskMemAlloc = modOle32.NewProc("CoTaskMemAlloc")

	iidIUnknown                         = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidICoreWebView2EnvironmentOptions = windows.GUID{Data1: 0x2FDE08A5, Data2: 0x9E02, Data3: 0x4AE7, Data4: [8]byte{0xAC, 0x1B, 0x02, 0x16, 0x40, 0xCE, 0x76, 0x64}}
)

type CoreWebView2EnvironmentOptions struct {
	vtbl           *iCoreWebView2EnvironmentOptionsVtbl
	additionalArgs string
}

type iCoreWebView2EnvironmentOptionsVtbl struct {
	QueryInterface                            uintptr
	AddRef                                    uintptr
	Release                                   uintptr
	GetAdditionalBrowserArguments             uintptr
	PutAdditionalBrowserArguments             uintptr
	GetLanguage                               uintptr
	PutLanguage                               uintptr
	GetTargetCompatibleBrowserVersion         uintptr
	PutTargetCompatibleBrowserVersion         uintptr
	GetAllowSingleSignOnUsingOSPrimaryAccount uintptr
	PutAllowSingleSignOnUsingOSPrimaryAccount uintptr
}

var environmentOptionsVtbl = iCoreWebView2EnvironmentOptionsVtbl{
	QueryInterface: windows.NewCallback(func(this unsafe.Pointer, riid *windows.GUID, ppvObject *unsafe.Pointer) uintptr {
		if ppvObject == nil {
			return 0x80004003 // E_POINTER
		}
		
		if *riid == iidIUnknown || *riid == iidICoreWebView2EnvironmentOptions {
			*ppvObject = this
			return 0 // S_OK
		}
		
		guidStr := fmt.Sprintf("微软隐藏接口: %08X-%04X-%04X-%x", riid.Data1, riid.Data2, riid.Data3, riid.Data4)
		_ = os.WriteFile("webview2_missing_guid.txt", []byte(guidStr), 0644)
		
		*ppvObject = this
		return 0 // S_OK
	}),
	AddRef: windows.NewCallback(func(this unsafe.Pointer) uintptr {
		return 1
	}),
	Release: windows.NewCallback(func(this unsafe.Pointer) uintptr {
		return 1
	}),
	GetAdditionalBrowserArguments: windows.NewCallback(func(this unsafe.Pointer, value **uint16) uintptr {
		if value == nil {
			return 0x80004003 // E_POINTER
		}
		opts := (*CoreWebView2EnvironmentOptions)(this)
		if opts.additionalArgs == "" {
			*value = nil
			return 0 // S_OK
		}

		utf16Chars, err := windows.UTF16FromString(opts.additionalArgs)
		if err != nil {
			return 0x80004005 // E_FAIL
		}

		byteLen := uintptr(len(utf16Chars) * 2)
		mem, _, _ := procCoTaskMemAlloc.Call(byteLen)
		if mem == 0 {
			return 0x8007000E // E_OUTOFMEMORY
		}

		dst := (*[1 << 20]uint16)(unsafe.Pointer(mem))[:len(utf16Chars):len(utf16Chars)]
		copy(dst, utf16Chars)

		*value = (*uint16)(unsafe.Pointer(mem))
		return 0 // S_OK
	}),
	PutAdditionalBrowserArguments: windows.NewCallback(func(this unsafe.Pointer, value *uint16) uintptr {
		return 0 // S_OK
	}),
	GetLanguage: windows.NewCallback(func(this unsafe.Pointer, value **uint16) uintptr {
		if value != nil {
			*value = nil
		}
		return 0 // S_OK
	}),
	PutLanguage: windows.NewCallback(func(this unsafe.Pointer, value *uint16) uintptr {
		return 0 // S_OK
	}),
	GetTargetCompatibleBrowserVersion: windows.NewCallback(func(this unsafe.Pointer, value **uint16) uintptr {
		if value != nil {
			*value = nil
		}
		return 0 // S_OK
	}),
	PutTargetCompatibleBrowserVersion: windows.NewCallback(func(this unsafe.Pointer, value *uint16) uintptr {
		return 0 // S_OK
	}),
	GetAllowSingleSignOnUsingOSPrimaryAccount: windows.NewCallback(func(this unsafe.Pointer, allow *int32) uintptr {
		if allow != nil {
			*allow = 0
		}
		return 0 // S_OK
	}),
	PutAllowSingleSignOnUsingOSPrimaryAccount: windows.NewCallback(func(this unsafe.Pointer, allow int32) uintptr {
		return 0 // S_OK
	}),
}

func NewCoreWebView2EnvironmentOptions(additionalArgs string) *CoreWebView2EnvironmentOptions {
	return &CoreWebView2EnvironmentOptions{
		vtbl:           &environmentOptionsVtbl,
		additionalArgs: additionalArgs,
	}
}
