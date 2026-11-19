//go:build windows
// +build windows

package edge

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"unsafe"

	"github.com/jchv/go-webview2/internal/w32"
	"golang.org/x/sys/windows"
)

type Chromium struct {
	AdditionalBrowserArguments string
	environmentOptions         *CoreWebView2EnvironmentOptions
	hwnd                       uintptr
	focusOnInit                bool
	controller                 *ICoreWebView2Controller
	webview                    *ICoreWebView2
	inited                     uintptr
	initFailed                 uintptr
	lastErr                    error
	envCompleted               *iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandler
	controllerCompleted        *iCoreWebView2CreateCoreWebView2ControllerCompletedHandler
	webMessageReceived         *iCoreWebView2WebMessageReceivedEventHandler
	permissionRequested        *iCoreWebView2PermissionRequestedEventHandler
	webResourceRequested       *iCoreWebView2WebResourceRequestedEventHandler
	acceleratorKeyPressed      *ICoreWebView2AcceleratorKeyPressedEventHandler
	navigationCompleted        *ICoreWebView2NavigationCompletedEventHandler
	newWindowRequested         *iCoreWebView2NewWindowRequestedEventHandler
	processFailed              *ICoreWebView2ProcessFailedEventHandler

	webMessageToken     _EventRegistrationToken
	permissionToken     _EventRegistrationToken
	webResourceToken    _EventRegistrationToken
	navigationToken     _EventRegistrationToken
	newWindowToken      _EventRegistrationToken
	processFailedToken  _EventRegistrationToken
	acceleratorKeyToken _EventRegistrationToken

	environment *ICoreWebView2Environment

	// Settings
	DataPath string
	DarkMode bool

	// permissions
	permissions      map[CoreWebView2PermissionKind]CoreWebView2PermissionState
	globalPermission *CoreWebView2PermissionState

	// Callbacks
	MessageCallback              func(string)
	WebResourceRequestedCallback func(request *ICoreWebView2WebResourceRequest, args *ICoreWebView2WebResourceRequestedEventArgs)
	NavigationCompletedCallback  func(sender *ICoreWebView2, args *ICoreWebView2NavigationCompletedEventArgs)
	AcceleratorKeyCallback       func(uint) bool
	NewWindowRequestedCallback   func(sender *ICoreWebView2, args *ICoreWebView2NewWindowRequestedEventArgs)
	ProcessFailedCallback        func(info ProcessFailedInfo)
}

func NewChromium() *Chromium {
	e := &Chromium{}
	/*
	 All these handlers are passed to native code through syscalls with 'uintptr(unsafe.Pointer(handler))' and we know
	 that a pointer to those will be kept in the native code. Furthermore these handlers als contain pointer to other Go
	 structs like the vtable.
	 This violates the unsafe.Pointer rule '(4) Conversion of a Pointer to a uintptr when calling syscall.Syscall.' because
	 theres no guarantee that Go doesn't move these objects.
	 AFAIK currently the Go runtime doesn't move HEAP objects, so we should be safe with these handlers. But they don't
	 guarantee it, because in the future Go might use a compacting GC.
	 There's a proposal to add a runtime.Pin function, to prevent moving pinned objects, which would allow to easily fix
	 this issue by just pinning the handlers. The https://go-review.googlesource.com/c/go/+/367296/ should land in Go 1.19.
	*/
	e.envCompleted = newICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler(e)
	e.controllerCompleted = newICoreWebView2CreateCoreWebView2ControllerCompletedHandler(e)
	e.webMessageReceived = newICoreWebView2WebMessageReceivedEventHandler(e)
	e.permissionRequested = newICoreWebView2PermissionRequestedEventHandler(e)
	e.webResourceRequested = newICoreWebView2WebResourceRequestedEventHandler(e)
	e.acceleratorKeyPressed = newICoreWebView2AcceleratorKeyPressedEventHandler(e)
	e.navigationCompleted = newICoreWebView2NavigationCompletedEventHandler(e)
	e.newWindowRequested = newICoreWebView2NewWindowRequestedEventHandler(e)
	e.processFailed = newICoreWebView2ProcessFailedEventHandler(e)
	e.permissions = make(map[CoreWebView2PermissionKind]CoreWebView2PermissionState)

	return e
}

func (e *Chromium) Embed(hwnd uintptr) bool {
	return e.EmbedErr(hwnd) == nil
}

func (e *Chromium) EmbedErr(hwnd uintptr) error {
	e.hwnd = hwnd
	e.lastErr = nil
	atomic.StoreUintptr(&e.inited, 0)
	atomic.StoreUintptr(&e.initFailed, 0)

	dataPath := e.DataPath
	if dataPath == "" {
		currentExePath := make([]uint16, windows.MAX_PATH)
		_, err := windows.GetModuleFileName(windows.Handle(0), &currentExePath[0], windows.MAX_PATH)
		if err != nil {
			e.lastErr = fmt.Errorf("failed to get module file name: %w", err)
			return e.lastErr
		}
		currentExeName := filepath.Base(windows.UTF16ToString(currentExePath))
		dataPath = filepath.Join(os.Getenv("AppData"), currentExeName)
	}

	var envOpts uintptr
	if e.AdditionalBrowserArguments != "" {
		e.environmentOptions = NewCoreWebView2EnvironmentOptions(e.AdditionalBrowserArguments)
		envOpts = uintptr(unsafe.Pointer(e.environmentOptions))
	}

	res, err := createCoreWebView2EnvironmentWithOptions(nil, windows.StringToUTF16Ptr(dataPath), envOpts, e.envCompleted)
	if err != nil {
		e.lastErr = newHRESULTError("CreateCoreWebView2EnvironmentWithOptions", res, err)
		return e.lastErr
	} else if int32(res) < 0 {
		e.lastErr = newHRESULTError("CreateCoreWebView2EnvironmentWithOptions", res, nil)
		return e.lastErr
	}

	var msg w32.Msg
	for {
		if atomic.LoadUintptr(&e.inited) != 0 {
			break
		}
		r, _, _ := w32.User32GetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0,
			0,
			0,
		)
		if int32(r) <= 0 {
			break
		}
		_, _, _ = w32.User32TranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = w32.User32DispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	if atomic.LoadUintptr(&e.initFailed) != 0 || e.controller == nil {
		if e.lastErr != nil {
			return e.lastErr
		}
		return newHRESULTError("Embed", 0x80004005, nil)
	}

	e.Init("window.external={invoke:s=>window.chrome.webview.postMessage(s)}")
	return nil
}

func removeEventToken(proc ComProc, obj uintptr, token _EventRegistrationToken) {
	if token.Value == 0 {
		return
	}
	if unsafe.Sizeof(uintptr(0)) == 4 {
		lo := uintptr(uint32(token.Value))
		hi := uintptr(uint32(uint64(token.Value) >> 32))
		_, _, _ = proc.Call(obj, lo, hi)
		return
	}
	_, _, _ = proc.Call(obj, uintptr(token.Value))
}

// Close detaches event handlers, closes the WebView2 controller, and releases COM references.
// Must be called on the UI thread; safe to call multiple times.
func (e *Chromium) Close() {
	if e.webview != nil {
		wv := uintptr(unsafe.Pointer(e.webview))
		removeEventToken(e.webview.vtbl.RemoveWebMessageReceived, wv, e.webMessageToken)
		removeEventToken(e.webview.vtbl.RemovePermissionRequested, wv, e.permissionToken)
		removeEventToken(e.webview.vtbl.RemoveWebResourceRequested, wv, e.webResourceToken)
		removeEventToken(e.webview.vtbl.RemoveNavigationCompleted, wv, e.navigationToken)
		removeEventToken(e.webview.vtbl.RemoveNewWindowRequested, wv, e.newWindowToken)
		removeEventToken(e.webview.vtbl.RemoveProcessFailed, wv, e.processFailedToken)
		e.webMessageToken = _EventRegistrationToken{}
		e.permissionToken = _EventRegistrationToken{}
		e.webResourceToken = _EventRegistrationToken{}
		e.navigationToken = _EventRegistrationToken{}
		e.newWindowToken = _EventRegistrationToken{}
		e.processFailedToken = _EventRegistrationToken{}
	}
	if e.controller != nil {
		removeEventToken(e.controller.vtbl.RemoveAcceleratorKeyPressed, uintptr(unsafe.Pointer(e.controller)), e.acceleratorKeyToken)
		e.acceleratorKeyToken = _EventRegistrationToken{}
		_ = e.controller.Close()
	}
	if e.webview != nil {
		_, _, _ = e.webview.vtbl.Release.Call(uintptr(unsafe.Pointer(e.webview)))
		e.webview = nil
	}
	if e.controller != nil {
		e.controller.Release()
		e.controller = nil
	}
	if e.environment != nil {
		_, _, _ = e.environment.vtbl.Release.Call(uintptr(unsafe.Pointer(e.environment)))
		e.environment = nil
	}
	e.environmentOptions = nil
}

func (e *Chromium) Navigate(url string) {
	if e.webview == nil {
		return
	}
	_, _, _ = e.webview.vtbl.Navigate.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(url))),
	)
}

func (e *Chromium) NavigateToString(htmlContent string) {
	if e.webview == nil {
		return
	}
	_, _, _ = e.webview.vtbl.NavigateToString.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(htmlContent))),
	)
}

func (e *Chromium) Init(script string) {
	if e.webview == nil {
		return
	}
	_, _, _ = e.webview.vtbl.AddScriptToExecuteOnDocumentCreated.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(script))),
		0,
	)
}

func (e *Chromium) Eval(script string) {
	if e.webview == nil {
		return
	}
	_script, err := windows.UTF16PtrFromString(script)
	if err != nil {
		log.Printf("Eval UTF16 conversion failed: %v", err)
		return
	}

	_, _, _ = e.webview.vtbl.ExecuteScript.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(_script)),
		0,
	)
}

func (e *Chromium) Show() error {
	if e.controller == nil {
		return nil
	}
	return e.controller.PutIsVisible(true)
}

func (e *Chromium) Hide() error {
	if e.controller == nil {
		return nil
	}
	return e.controller.PutIsVisible(false)
}

func (e *Chromium) QueryInterface(_, _ uintptr) uintptr {
	return 0
}

func (e *Chromium) AddRef() uintptr {
	return 1
}

func (e *Chromium) Release() uintptr {
	return 1
}

func (e *Chromium) EnvironmentCompleted(res uintptr, env *ICoreWebView2Environment) uintptr {
	if int32(res) < 0 || env == nil {
		e.lastErr = newHRESULTError("EnvironmentCompleted", res, nil)
		atomic.StoreUintptr(&e.initFailed, 1)
		atomic.StoreUintptr(&e.inited, 1)
		return 0
	}
	_, _, _ = env.vtbl.AddRef.Call(uintptr(unsafe.Pointer(env)))
	e.environment = env

	hr, _, _ := env.vtbl.CreateCoreWebView2Controller.Call(
		uintptr(unsafe.Pointer(env)),
		e.hwnd,
		uintptr(unsafe.Pointer(e.controllerCompleted)),
	)
	if int32(hr) < 0 {
		e.lastErr = newHRESULTError("CreateCoreWebView2Controller", hr, nil)
		atomic.StoreUintptr(&e.initFailed, 1)
		atomic.StoreUintptr(&e.inited, 1)
	}
	return 0
}

func (e *Chromium) CreateCoreWebView2ControllerCompleted(res uintptr, controller *ICoreWebView2Controller) uintptr {
	if int32(res) < 0 || controller == nil {
		e.lastErr = newHRESULTError("CreateCoreWebView2ControllerCompleted", res, nil)
		atomic.StoreUintptr(&e.initFailed, 1)
		atomic.StoreUintptr(&e.inited, 1)
		return 0
	}
	controller.AddRef()
	e.controller = controller

	e.SetDarkMode(e.DarkMode)

	// GetCoreWebView2 returns an already AddRef'd [out] interface pointer.
	hr, _, _ := controller.vtbl.GetCoreWebView2.Call(
		uintptr(unsafe.Pointer(controller)),
		uintptr(unsafe.Pointer(&e.webview)),
	)
	if int32(hr) < 0 || e.webview == nil {
		e.lastErr = newHRESULTError("GetCoreWebView2", hr, nil)
		atomic.StoreUintptr(&e.initFailed, 1)
		atomic.StoreUintptr(&e.inited, 1)
		return 0
	}

	_, _, _ = e.webview.vtbl.AddWebMessageReceived.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(e.webMessageReceived)),
		uintptr(unsafe.Pointer(&e.webMessageToken)),
	)
	_, _, _ = e.webview.vtbl.AddPermissionRequested.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(e.permissionRequested)),
		uintptr(unsafe.Pointer(&e.permissionToken)),
	)
	_, _, _ = e.webview.vtbl.AddWebResourceRequested.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(e.webResourceRequested)),
		uintptr(unsafe.Pointer(&e.webResourceToken)),
	)
	_, _, _ = e.webview.vtbl.AddNavigationCompleted.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(e.navigationCompleted)),
		uintptr(unsafe.Pointer(&e.navigationToken)),
	)
	_, _, _ = e.webview.vtbl.AddNewWindowRequested.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(e.newWindowRequested)),
		uintptr(unsafe.Pointer(&e.newWindowToken)),
	)
	_, _, _ = e.webview.vtbl.AddProcessFailed.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(e.processFailed)),
		uintptr(unsafe.Pointer(&e.processFailedToken)),
	)
	_ = e.controller.AddAcceleratorKeyPressed(e.acceleratorKeyPressed, &e.acceleratorKeyToken)

	atomic.StoreUintptr(&e.inited, 1)

	if e.focusOnInit {
		e.Focus()
	}

	return 0
}

func (e *Chromium) MessageReceived(sender *ICoreWebView2, args *iCoreWebView2WebMessageReceivedEventArgs) uintptr {
	var message *uint16
	_, _, _ = args.vtbl.TryGetWebMessageAsString.Call(
		uintptr(unsafe.Pointer(args)),
		uintptr(unsafe.Pointer(&message)),
	)
	if e.MessageCallback != nil {
		e.MessageCallback(w32.Utf16PtrToString(message))
	}
	_, _, _ = sender.vtbl.PostWebMessageAsString.Call(
		uintptr(unsafe.Pointer(sender)),
		uintptr(unsafe.Pointer(message)),
	)
	windows.CoTaskMemFree(unsafe.Pointer(message))
	return 0
}

func (e *Chromium) SetPermission(kind CoreWebView2PermissionKind, state CoreWebView2PermissionState) {
	e.permissions[kind] = state
}

func (e *Chromium) SetGlobalPermission(state CoreWebView2PermissionState) {
	e.globalPermission = &state
}

func (e *Chromium) PermissionRequested(_ *ICoreWebView2, args *iCoreWebView2PermissionRequestedEventArgs) uintptr {
	var kind CoreWebView2PermissionKind
	_, _, _ = args.vtbl.GetPermissionKind.Call(
		uintptr(unsafe.Pointer(args)),
		uintptr(unsafe.Pointer(&kind)),
	)
	var result CoreWebView2PermissionState
	if e.globalPermission != nil {
		result = *e.globalPermission
	} else {
		var ok bool
		result, ok = e.permissions[kind]
		if !ok {
			result = CoreWebView2PermissionStateDefault
		}
	}
	_, _, _ = args.vtbl.PutState.Call(
		uintptr(unsafe.Pointer(args)),
		uintptr(result),
	)
	return 0
}

func (e *Chromium) WebResourceRequested(sender *ICoreWebView2, args *ICoreWebView2WebResourceRequestedEventArgs) uintptr {
	req, err := args.GetRequest()
	if err != nil {
		log.Printf("WebResourceRequested GetRequest failed: %v", err)
		return 0
	}
	if e.WebResourceRequestedCallback != nil {
		e.WebResourceRequestedCallback(req, args)
	}
	return 0
}

func (e *Chromium) AddWebResourceRequestedFilter(filter string, ctx COREWEBVIEW2_WEB_RESOURCE_CONTEXT) {
	if e.webview == nil {
		return
	}
	if err := e.webview.AddWebResourceRequestedFilter(filter, ctx); err != nil {
		log.Printf("AddWebResourceRequestedFilter failed: %v", err)
	}
}

func (e *Chromium) Environment() *ICoreWebView2Environment {
	return e.environment
}

// AcceleratorKeyPressed is called when an accelerator key is pressed.
// If the AcceleratorKeyCallback method has been set, it will defer handling of the keypress
// to the callback. That callback returns a bool indicating if the event was handled.
func (e *Chromium) AcceleratorKeyPressed(sender *ICoreWebView2Controller, args *ICoreWebView2AcceleratorKeyPressedEventArgs) uintptr {
	if e.AcceleratorKeyCallback == nil {
		return 0
	}
	eventKind, _ := args.GetKeyEventKind()
	if eventKind == COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN ||
		eventKind == COREWEBVIEW2_KEY_EVENT_KIND_SYSTEM_KEY_DOWN {
		virtualKey, _ := args.GetVirtualKey()
		status, _ := args.GetPhysicalKeyStatus()
		if !status.WasKeyDown {
			_ = args.PutHandled(e.AcceleratorKeyCallback(virtualKey))
			return 0
		}
	}
	_ = args.PutHandled(false)
	return 0
}

func (e *Chromium) GetSettings() (*ICoreWebViewSettings, error) {
	if e.webview == nil {
		return nil, newHRESULTError("GetSettings", 0x8007139F, nil)
	}
	return e.webview.GetSettings()
}

func (e *Chromium) GetController() *ICoreWebView2Controller {
	return e.controller
}

func boolToInt(input bool) int {
	if input {
		return 1
	}
	return 0
}

func (e *Chromium) NavigationCompleted(sender *ICoreWebView2, args *ICoreWebView2NavigationCompletedEventArgs) uintptr {
	if e.NavigationCompletedCallback != nil {
		e.NavigationCompletedCallback(sender, args)
	}
	return 0
}

func (e *Chromium) ProcessFailed(_ *ICoreWebView2, args *ICoreWebView2ProcessFailedEventArgs) uintptr {
	if args == nil {
		return 0
	}
	info := args.Info()
	if e.ProcessFailedCallback != nil {
		e.ProcessFailedCallback(info)
	}
	return 0
}

func (e *Chromium) NotifyParentWindowPositionChanged() error {
	// It looks like the wndproc function is called before the controller initialization is complete.
	// Because of this the controller is nil.
	if e.controller == nil {
		return nil
	}
	return e.controller.NotifyParentWindowPositionChanged()
}

func (e *Chromium) Focus() {
	if e.controller == nil {
		e.focusOnInit = true
		return
	}
	_ = e.controller.MoveFocus(COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC)
}

func (e *Chromium) NewWindowRequested(sender *ICoreWebView2, args *ICoreWebView2NewWindowRequestedEventArgs) uintptr {
	if e.NewWindowRequestedCallback != nil {
		e.NewWindowRequestedCallback(sender, args)
	}
	return 0
}

func (e *Chromium) SetDarkMode(isDark bool) {
	e.DarkMode = isDark
	if e.controller == nil {
		return
	}
	ctrl2 := e.controller.GetICoreWebView2Controller2()
	if ctrl2 == nil {
		return
	}
	defer ctrl2.vtbl.Release.Call(uintptr(unsafe.Pointer(ctrl2)))
	_ = ctrl2.PutDefaultBackgroundColor(themeBackgroundColor(isDark))
}