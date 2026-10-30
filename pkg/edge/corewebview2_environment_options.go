package edge

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modOle32           = windows.NewLazySystemDLL("ole32.dll")
	procCoTaskMemAlloc = modOle32.NewProc("CoTaskMemAlloc")

	// COM Interface GUIDs
	iidIUnknown                        = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidICoreWebView2EnvironmentOptions = windows.GUID{Data1: 0x2FDE08A8, Data2: 0x1E9A, Data3: 0x4766, Data4: [8]byte{0x8C, 0x05, 0x95, 0xA9, 0xCE, 0xB9, 0xD1, 0xC5}}
)

// allocCoTaskMemString allocates UTF-16 wide string in COM task memory for the caller to free.
func allocCoTaskMemString(str string) (*uint16, uintptr) {
	utf16Chars, err := windows.UTF16FromString(str)
	if err != nil {
		return nil, 0x80004005 // E_FAIL
	}

	byteLen := uintptr(len(utf16Chars) * 2)
	mem, _, _ := procCoTaskMemAlloc.Call(byteLen)
	if mem == 0 {
		return nil, 0x8007000E // E_OUTOFMEMORY
	}

	dst := (*[1 << 20]uint16)(unsafe.Pointer(mem))[:len(utf16Chars):len(utf16Chars)]
	copy(dst, utf16Chars)

	return (*uint16)(unsafe.Pointer(mem)), 0 // S_OK
}

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
	// 1. QueryInterface
	QueryInterface: windows.NewCallback(func(this unsafe.Pointer, riid *windows.GUID, ppvObject *unsafe.Pointer) uintptr {
		if ppvObject == nil {
			return 0x80004003 // E_POINTER
		}
		if *riid == iidIUnknown || *riid == iidICoreWebView2EnvironmentOptions {
			*ppvObject = this
			return 0 // S_OK
		}
		*ppvObject = nil
		return 0x80004002 // E_NOINTERFACE
	}),

	// 2. AddRef: Static singleton instance; keep reference count constant.
	AddRef: windows.NewCallback(func(this unsafe.Pointer) uintptr {
		return 1
	}),

	// 3. Release: Static singleton instance; keep reference count constant.
	Release: windows.NewCallback(func(this unsafe.Pointer) uintptr {
		return 1
	}),

	// 4. GetAdditionalBrowserArguments: Feed custom startup parameters to the WebView2 runtime.
	GetAdditionalBrowserArguments: windows.NewCallback(func(this unsafe.Pointer, value **uint16) uintptr {
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

	// 5. PutAdditionalBrowserArguments: Read-only context; no-op.
	PutAdditionalBrowserArguments: windows.NewCallback(func(this unsafe.Pointer, value *uint16) uintptr {
		return 0 // S_OK
	}),

	// 6. GetLanguage: Return nil to inherit system default display language.
	GetLanguage: windows.NewCallback(func(this unsafe.Pointer, value **uint16) uintptr {
		if value == nil {
			return 0x80004003 // E_POINTER
		}
		*value = nil
		return 0 // S_OK
	}),

	// 7. PutLanguage: Read-only context; no-op.
	PutLanguage: windows.NewCallback(func(this unsafe.Pointer, value *uint16) uintptr {
		return 0 // S_OK
	}),

	// 8. GetTargetCompatibleBrowserVersion: Provide official GA baseline version to satisfy loader contract.
	GetTargetCompatibleBrowserVersion: windows.NewCallback(func(this unsafe.Pointer, value **uint16) uintptr {
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

	// 9. PutTargetCompatibleBrowserVersion: Read-only context; no-op.
	PutTargetCompatibleBrowserVersion: windows.NewCallback(func(this unsafe.Pointer, value *uint16) uintptr {
		return 0 // S_OK
	}),

	// 10. GetAllowSingleSignOnUsingOSPrimaryAccount: Disable OS primary account SSO.
	GetAllowSingleSignOnUsingOSPrimaryAccount: windows.NewCallback(func(this unsafe.Pointer, allow *int32) uintptr {
		if allow == nil {
			return 0x80004003 // E_POINTER
		}
		*allow = 0 // FALSE
		return 0  // S_OK
	}),

	// 11. PutAllowSingleSignOnUsingOSPrimaryAccount: Read-only context; no-op.
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
