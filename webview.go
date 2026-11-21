//go:build windows
// +build windows

package webview2

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/jchv/go-webview2/internal/w32"
	"github.com/jchv/go-webview2/pkg/edge"

	"golang.org/x/sys/windows"
)

var (
	windowContext     = map[uintptr]interface{}{}
	windowContextSync sync.RWMutex

	registerClassOnce sync.Once
	wndprocCallback   uintptr
)

func getWindowContext(wnd uintptr) interface{} {
	windowContextSync.RLock()
	defer windowContextSync.RUnlock()
	return windowContext[wnd]
}

func setWindowContext(wnd uintptr, data interface{}) {
	windowContextSync.Lock()
	defer windowContextSync.Unlock()
	if data == nil {
		delete(windowContext, wnd)
	} else {
		windowContext[wnd] = data
	}
}

type browser interface {
	Embed(hwnd uintptr) bool
	EmbedErr(hwnd uintptr) error
	Show() error
	Resize()
	Navigate(url string)
	NavigateToString(htmlContent string)
	Init(script string)
	Eval(script string)
	NotifyParentWindowPositionChanged() error
	Focus()
	SetDarkMode(isDark bool)
	Close()
}

type webview struct {
	hwnd                 uintptr
	mainthread           uintptr
	browser              browser
	autofocus            bool
	comInited            bool
	darkMode             bool
	customDarkMode       bool
	maxsz                w32.Point
	minsz                w32.Point
	m                    sync.Mutex
	bindings             map[string]interface{}
	dispatchq            []func()
	newWindowHandler     func(url string) bool
	processFailedHandler func(event ProcessFailedEvent)
}

type WindowOptions struct {
	Title    string
	Width    uint
	Height   uint
	IconId   uint
	Center   bool
	DarkMode bool
}

type WebViewOptions struct {
	Window unsafe.Pointer
	Debug  bool

	// DataPath specifies the datapath for the WebView2 runtime to use for the
	// browser instance.
	DataPath string

	// AutoFocus will try to keep the WebView2 widget focused when the window
	// is focused.
	AutoFocus bool

	AdditionalBrowserArguments string

	// WindowOptions customizes the window that is created to embed the
	// WebView2 widget.
	WindowOptions WindowOptions
}

// New creates a new webview in a new window.
func New(debug bool) WebView { return NewWithOptions(WebViewOptions{Debug: debug}) }

// NewWindow creates a new webview using an existing window.
//
// Deprecated: Use NewWithOptions.
func NewWindow(debug bool, window unsafe.Pointer) WebView {
	return NewWithOptions(WebViewOptions{Debug: debug, Window: window})
}

// NewWithOptions creates a new webview using the provided options.
func NewWithOptions(options WebViewOptions) WebView {
	w, err := NewWithOptionsErr(options)
	if err != nil {
		log.Printf("WebView2 initialization failed: %v", err)
		return nil
	}
	return w
}

// NewWithOptionsErr creates a new webview and returns a structured error (including Win32 HRESULT) on failure.
func NewWithOptionsErr(options WebViewOptions) (WebView, error) {
	runtime.LockOSThread()

	const coinitApartmentThreaded = 2
	hr, _, _ := w32.Ole32CoInitializeEx.Call(0, coinitApartmentThreaded)
	// S_OK (0) or S_FALSE (1) increments the thread's COM init count and must be paired with CoUninitialize.
	comInited := uint32(hr) == 0 || uint32(hr) == 1

	w := &webview{
		bindings:  map[string]interface{}{},
		autofocus: options.AutoFocus,
		comInited: comInited,
	}

	chromium := edge.NewChromium()
	chromium.MessageCallback = w.msgcb
	chromium.DataPath = options.DataPath
	chromium.DarkMode = options.WindowOptions.DarkMode
	chromium.AdditionalBrowserArguments = options.AdditionalBrowserArguments
	chromium.SetPermission(edge.CoreWebView2PermissionKindClipboardRead, edge.CoreWebView2PermissionStateAllow)

	chromium.NewWindowRequestedCallback = func(sender *edge.ICoreWebView2, args *edge.ICoreWebView2NewWindowRequestedEventArgs) {
		uri, err := args.GetUri()
		if err != nil {
			return
		}

		_ = args.PutHandled(true)

		if uri == "" {
			return
		}

		w.m.Lock()
		handler := w.newWindowHandler
		w.m.Unlock()

		if handler != nil && handler(uri) {
			return
		}

		_ = edge.OpenDefaultBrowser(atomic.LoadUintptr(&w.hwnd), uri)
	}

	chromium.ProcessFailedCallback = func(info edge.ProcessFailedInfo) {
		w.m.Lock()
		handler := w.processFailedHandler
		w.m.Unlock()

		if handler != nil {
			handler(ProcessFailedEvent{
				Kind:        uint32(info.Kind),
				KindText:    info.Kind.String(),
				Reason:      uint32(info.Reason),
				ReasonText:  info.Reason.String(),
				ExitCode:    info.ExitCode,
				Description: info.Description,
				IsFatal:     info.IsFatal,
				Summary:     info.String(),
			})
		}
	}

	w.browser = chromium
	w.mainthread, _, _ = w32.Kernel32GetCurrentThreadID.Call()

	cleanupOnErr := func() {
		hwnd := atomic.SwapUintptr(&w.hwnd, 0)
		if hwnd != 0 {
			setWindowContext(hwnd, nil)
		}
		chromium.Close()
		if hwnd != 0 {
			_, _, _ = w32.User32DestroyWindow.Call(hwnd)
		}
		if w.comInited {
			_, _, _ = w32.Ole32CoUninitialize.Call()
			w.comInited = false
		}
		runtime.UnlockOSThread()
	}

	if err := w.createWithOptionsErr(options.WindowOptions); err != nil {
		cleanupOnErr()
		return nil, err
	}

	settings, err := chromium.GetSettings()
	if err != nil {
		cleanupOnErr()
		return nil, err
	}
	err = settings.PutAreDefaultContextMenusEnabled(options.Debug)
	if err == nil {
		err = settings.PutAreDevToolsEnabled(options.Debug)
	}
	settings.Release()
	if err != nil {
		cleanupOnErr()
		return nil, err
	}

	return w, nil
}

// HRESULTCode extracts the underlying Win32/COM HRESULT code from an error if available.
func HRESULTCode(err error) (uint32, bool) {
	var hrErr *edge.HRESULTError
	if errors.As(err, &hrErr) {
		return hrErr.Code, true
	}
	return 0, false
}

type rpcMessage struct {
	ID     int               `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

func jsString(v interface{}) string { b, _ := json.Marshal(v); return string(b) }

func (w *webview) msgcb(msg string) {
	d := rpcMessage{}
	if err := json.Unmarshal([]byte(msg), &d); err != nil {
		log.Printf("invalid RPC message: %v", err)
		return
	}

	id := strconv.Itoa(d.ID)
	if res, err := w.callbinding(d); err != nil {
		w.Dispatch(func() {
			w.Eval("window._rpc[" + id + "].reject(" + jsString(err.Error()) + "); window._rpc[" + id + "] = undefined")
		})
	} else if b, err := json.Marshal(res); err != nil {
		w.Dispatch(func() {
			w.Eval("window._rpc[" + id + "].reject(" + jsString(err.Error()) + "); window._rpc[" + id + "] = undefined")
		})
	} else {
		w.Dispatch(func() {
			w.Eval("window._rpc[" + id + "].resolve(" + string(b) + "); window._rpc[" + id + "] = undefined")
		})
	}
}

func (w *webview) callbinding(d rpcMessage) (interface{}, error) {
	w.m.Lock()
	f, ok := w.bindings[d.Method]
	w.m.Unlock()
	if !ok {
		return nil, nil
	}

	v := reflect.ValueOf(f)
	isVariadic := v.Type().IsVariadic()
	numIn := v.Type().NumIn()
	if (isVariadic && len(d.Params) < numIn-1) || (!isVariadic && len(d.Params) != numIn) {
		return nil, errors.New("function arguments mismatch")
	}
	args := []reflect.Value{}
	for i := range d.Params {
		var arg reflect.Value
		if isVariadic && i >= numIn-1 {
			arg = reflect.New(v.Type().In(numIn - 1).Elem())
		} else {
			arg = reflect.New(v.Type().In(i))
		}
		if err := json.Unmarshal(d.Params[i], arg.Interface()); err != nil {
			return nil, err
		}
		args = append(args, arg.Elem())
	}

	errorType := reflect.TypeOf((*error)(nil)).Elem()
	res := v.Call(args)
	switch len(res) {
	case 0:
		// No results from the function, just return nil
		return nil, nil

	case 1:
		// One result may be a value, or an error
		if res[0].Type().Implements(errorType) {
			if res[0].Interface() != nil {
				return nil, res[0].Interface().(error)
			}
			return nil, nil
		}
		return res[0].Interface(), nil

	case 2:
		// Two results: first one is value, second is error
		if !res[1].Type().Implements(errorType) {
			return nil, errors.New("second return value must be an error")
		}
		if res[1].Interface() == nil {
			return res[0].Interface(), nil
		}
		return res[0].Interface(), res[1].Interface().(error)

	default:
		return nil, errors.New("unexpected number of return values")
	}
}

func (w *webview) SetDarkMode(isDark bool) {
	w.Dispatch(func() {
		hwnd := atomic.LoadUintptr(&w.hwnd)
		if hwnd == 0 {
			return
		}
		w.customDarkMode = true
		w.darkMode = isDark
		edge.ApplyWindowTheme(hwnd, isDark)
		if w.browser != nil {
			w.browser.SetDarkMode(isDark)
		}
	})
}

func (w *webview) flushDispatchQueue() {
	for {
		w.m.Lock()
		if len(w.dispatchq) == 0 {
			w.m.Unlock()
			return
		}
		q := w.dispatchq
		w.dispatchq = nil
		w.m.Unlock()
		for _, v := range q {
			v()
		}
	}
}

func wndproc(hwnd, msg, wp, lp uintptr) uintptr {
	if w, ok := getWindowContext(hwnd).(*webview); ok {
		switch msg {
		case w32.WMDPIChanged:
			suggestedRect := *(**w32.Rect)(unsafe.Pointer(&lp))
			newWidth := suggestedRect.Right - suggestedRect.Left
			newHeight := suggestedRect.Bottom - suggestedRect.Top

			_, _, _ = w32.User32SetWindowPos.Call(
				hwnd, 0,
				uintptr(suggestedRect.Left),
				uintptr(suggestedRect.Top),
				uintptr(newWidth),
				uintptr(newHeight),
				w32.SWPNoZOrder|w32.SWPFrameChanged|w32.SWPNoActivate,
			)

			if w.browser != nil {
				w.browser.Resize()
			}
			return 0

		case w32.WMEraseBkgnd:
			edge.EraseWindowBackground(hwnd, wp, w.darkMode)
			return 1

		case w32.WMSettingChange, w32.WMThemeChanged:
			if msg == w32.WMSettingChange {
				if lp == 0 || w32.Utf16PtrToString(*(**uint16)(unsafe.Pointer(&lp))) != "ImmersiveColorSet" {
					r, _, _ := w32.User32DefWindowProcW.Call(hwnd, msg, wp, lp)
					return r
				}
			}
			if !w.customDarkMode {
				if isDark := edge.IsSystemDarkMode(); isDark != w.darkMode {
					w.darkMode = isDark
					edge.ApplyWindowTheme(hwnd, isDark)
					if w.browser != nil {
						w.browser.SetDarkMode(isDark)
					}
				}
			}
			r, _, _ := w32.User32DefWindowProcW.Call(hwnd, msg, wp, lp)
			return r

		case w32.WMMove, w32.WMMoving:
			_ = w.browser.NotifyParentWindowPositionChanged()

		case w32.WMNCLButtonDown:
			_, _, _ = w32.User32SetFocus.Call(hwnd)
			r, _, _ := w32.User32DefWindowProcW.Call(hwnd, msg, wp, lp)
			return r

		case w32.WMSize:
			w.browser.Resize()

		case w32.WMActivate:
			if wp == w32.WAInactive {
				break
			}
			if w.autofocus {
				w.browser.Focus()
			}

		case w32.WMClose:
			_, _, _ = w32.User32DestroyWindow.Call(hwnd)
			return 0
			
		case w32.WMApp:
			w.flushDispatchQueue()
			return 0

		case w32.WMDestroy:
			setWindowContext(hwnd, nil)
			atomic.StoreUintptr(&w.hwnd, 0)
			w.m.Lock()
			w.dispatchq = nil
			w.m.Unlock()
			if w.browser != nil {
				w.browser.Close()
			}
			w.Terminate()
			return 0

		case w32.WMGetMinMaxInfo:
			lpmmi := *(**w32.MinMaxInfo)(unsafe.Pointer(&lp))
			if w.maxsz.X > 0 && w.maxsz.Y > 0 {
				lpmmi.PtMaxSize = w.maxsz
				lpmmi.PtMaxTrackSize = w.maxsz
			}
			if w.minsz.X > 0 && w.minsz.Y > 0 {
				lpmmi.PtMinTrackSize = w.minsz
			}

		default:
			r, _, _ := w32.User32DefWindowProcW.Call(hwnd, msg, wp, lp)
			return r
		}
		return 0
	}
	r, _, _ := w32.User32DefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

func centerOnCursorMonitor(width, height int) (int, int, bool) {
	var pt w32.Point
	if ret, _, _ := w32.User32GetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); ret == 0 {
		return 0, 0, false
	}

	r := w32.Rect{
		Left:   pt.X,
		Top:    pt.Y,
		Right:  pt.X + 1,
		Bottom: pt.Y + 1,
	}
	hMonitor, _, _ := w32.User32MonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), w32.MonitorDefaultToNearest)
	if hMonitor == 0 {
		return 0, 0, false
	}

	var mi w32.MonitorInfo
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if ret, _, _ := w32.User32GetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&mi))); ret == 0 {
		return 0, 0, false
	}

	workW := int(mi.RcWork.Right - mi.RcWork.Left)
	workH := int(mi.RcWork.Bottom - mi.RcWork.Top)
	if workW <= 0 || workH <= 0 {
		return 0, 0, false
	}

	x := int(mi.RcWork.Left) + (workW-width)/2
	y := int(mi.RcWork.Top) + (workH-height)/2
	if x < int(mi.RcWork.Left) {
		x = int(mi.RcWork.Left)
	}
	if y < int(mi.RcWork.Top) {
		y = int(mi.RcWork.Top)
	}
	return x, y, true
}

func (w *webview) Create(debug bool, window unsafe.Pointer) bool {
	// This function signature stopped making sense a long time ago.
	// It is but legacy cruft at this point.
	return w.CreateWithOptions(WindowOptions{})
}

func (w *webview) CreateWithOptions(opts WindowOptions) bool {
	return w.createWithOptionsErr(opts) == nil
}

func (w *webview) createWithOptionsErr(opts WindowOptions) error {
	isDark := opts.DarkMode
	w.darkMode = isDark

	var hinstance windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &hinstance)

	className, _ := windows.UTF16PtrFromString("webview")
	registerClassOnce.Do(func() {
		var icon uintptr
		if opts.IconId == 0 {
			// load default icon
			icow, _, _ := w32.User32GetSystemMetrics.Call(w32.SystemMetricsCxIcon)
			icoh, _, _ := w32.User32GetSystemMetrics.Call(w32.SystemMetricsCyIcon)
			icon, _, _ = w32.User32LoadImageW.Call(0, 32512, 1, icow, icoh, w32.LR_SHARED)
		} else {
			// load icon from resource
			icon, _, _ = w32.User32LoadImageW.Call(uintptr(hinstance), uintptr(opts.IconId), 1, 0, 0, w32.LR_DEFAULTSIZE|w32.LR_SHARED)
		}

		wndprocCallback = windows.NewCallback(wndproc)
		wc := w32.WndClassExW{
			CbSize:        uint32(unsafe.Sizeof(w32.WndClassExW{})),
			HInstance:     hinstance,
			LpszClassName: className,
			HIcon:         windows.Handle(icon),
			HIconSm:       windows.Handle(icon),
			HbrBackground: windows.Handle(edge.ThemeBrush(isDark)),
			LpfnWndProc:   wndprocCallback,
		}
		_, _, _ = w32.User32RegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	})

	windowName, _ := windows.UTF16PtrFromString(opts.Title)

	windowWidth := opts.Width
	if windowWidth == 0 {
		windowWidth = 640
	}
	windowHeight := opts.Height
	if windowHeight == 0 {
		windowHeight = 480
	}

	var posX, posY uintptr
	var targetX, targetY int
	if opts.Center {
		var ok bool
		if targetX, targetY, ok = centerOnCursorMonitor(int(windowWidth), int(windowHeight)); !ok {
			// fallback to primary screen size
			screenWidth, _, _ := w32.User32GetSystemMetrics.Call(w32.SM_CXSCREEN)
			screenHeight, _, _ := w32.User32GetSystemMetrics.Call(w32.SM_CYSCREEN)
			targetX = (int(screenWidth) - int(windowWidth)) / 2
			targetY = (int(screenHeight) - int(windowHeight)) / 2
		}
		posX = uintptr(int32(targetX))
		posY = uintptr(int32(targetY))
	} else {
		// use default position
		posX = w32.CW_USEDEFAULT
		posY = w32.CW_USEDEFAULT
	}

	hwnd, _, createErr := w32.User32CreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0xCF0000, // WS_OVERLAPPEDWINDOW
		posX,
		posY,
		uintptr(windowWidth),
		uintptr(windowHeight),
		0,
		0,
		uintptr(hinstance),
		0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW failed: %w", createErr)
	}
	atomic.StoreUintptr(&w.hwnd, hwnd)

	setWindowContext(hwnd, w)
	edge.ApplyWindowTheme(hwnd, isDark)

	if err := w.browser.EmbedErr(hwnd); err != nil {
		setWindowContext(hwnd, nil)
		w.browser.Close()
		_, _, _ = w32.User32DestroyWindow.Call(hwnd)
		atomic.StoreUintptr(&w.hwnd, 0)
		return err
	}

	w.showWindowAtFront(targetX, targetY, int(windowWidth), int(windowHeight), opts.Center)

	return nil
}

func (w *webview) SetProcessFailedHandler(handler func(event ProcessFailedEvent)) {
	w.m.Lock()
	defer w.m.Unlock()
	w.processFailedHandler = handler
}

func (w *webview) showWindowAtFront(x, y, width, height int, move bool) {
	hwnd := atomic.LoadUintptr(&w.hwnd)
	if hwnd == 0 {
		return
	}

	flags := uintptr(w32.SWPFrameChanged | w32.SWPNoActivate)
	if !move {
		flags |= w32.SWPNoMove | w32.SWPNoSize
	}
	_, _, _ = w32.User32SetWindowPos.Call(
		hwnd, w32.HWNDTopMost,
		uintptr(int32(x)), uintptr(int32(y)),
		uintptr(width), uintptr(height),
		flags,
	)
	_, _, _ = w32.User32ShowWindow.Call(hwnd, w32.SWShow)

	_ = w.browser.Show()
	w.browser.Resize()
	_ = w.browser.NotifyParentWindowPositionChanged()
	_, _, _ = w32.User32UpdateWindow.Call(hwnd)

	_, _, _ = w32.User32SetWindowPos.Call(
		hwnd, w32.HWNDNoTopMost,
		0, 0, 0, 0,
		w32.SWPNoMove|w32.SWPNoSize|w32.SWPNoActivate,
	)

	if !w.autofocus {
		return
	}

	_, _, _ = w32.User32BringWindowToTop.Call(hwnd)
	if ret, _, _ := w32.User32SetForegroundWindow.Call(hwnd); ret == 0 {
		fgHwnd, _, _ := w32.User32GetForegroundWindow.Call()
		if fgHwnd != 0 && fgHwnd != hwnd {
			fgTid, _, _ := w32.User32GetWindowThreadProcessId.Call(fgHwnd, 0)
			if fgTid != 0 && fgTid != w.mainthread {
				if attached, _, _ := w32.User32AttachThreadInput.Call(w.mainthread, fgTid, 1); attached != 0 {
					_, _, _ = w32.User32BringWindowToTop.Call(hwnd)
					_, _, _ = w32.User32SetForegroundWindow.Call(hwnd)
					_, _, _ = w32.User32AttachThreadInput.Call(w.mainthread, fgTid, 0)
				}
			}
		}
	}

	_, _, _ = w32.User32SetFocus.Call(hwnd)
	w.browser.Focus()
}

func (w *webview) Destroy() {
	hwnd := atomic.LoadUintptr(&w.hwnd)
	if hwnd == 0 {
		return
	}
	_, _, _ = w32.User32PostMessageW.Call(hwnd, w32.WMClose, 0, 0)
}

func (w *webview) Run() {
	defer func() {
		if w.browser != nil {
			w.browser.Close()
		}
		if w.comInited {
			_, _, _ = w32.Ole32CoUninitialize.Call()
			w.comInited = false
		}
		runtime.UnlockOSThread()
	}()

	var msg w32.Msg
	for {
		r, _, _ := w32.User32GetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0,
			0,
			0,
		)
		if int32(r) <= 0 {
			return
		}

		r, _, _ = w32.User32GetAncestor.Call(uintptr(msg.Hwnd), w32.GARoot)
		r, _, _ = w32.User32IsDialogMessage.Call(r, uintptr(unsafe.Pointer(&msg)))
		if r != 0 {
			continue
		}
		_, _, _ = w32.User32TranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = w32.User32DispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (w *webview) Terminate() {
	_, _, _ = w32.User32PostQuitMessage.Call(0)
}

func (w *webview) Window() unsafe.Pointer {
	hwnd := atomic.LoadUintptr(&w.hwnd)
	return *(*unsafe.Pointer)(unsafe.Pointer(&hwnd))
}

func (w *webview) Navigate(url string) {
	w.browser.Navigate(url)
}

func (w *webview) SetHtml(html string) {
	w.browser.NavigateToString(html)
}

func (w *webview) SetTitle(title string) {
	hwnd := atomic.LoadUintptr(&w.hwnd)
	if hwnd == 0 {
		return
	}
	_title, err := windows.UTF16FromString(title)
	if err != nil {
		_title, _ = windows.UTF16FromString("")
	}
	_, _, _ = w32.User32SetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&_title[0])))
}

func (w *webview) SetSize(width int, height int, hints Hint) {
	hwnd := atomic.LoadUintptr(&w.hwnd)
	if hwnd == 0 {
		return
	}
	index := w32.GWLStyle
	style := w32.GetWindowLong(hwnd, index)
	if hints == HintFixed {
		style &^= (w32.WSThickFrame | w32.WSMaximizeBox)
	} else {
		style |= (w32.WSThickFrame | w32.WSMaximizeBox)
	}
	w32.SetWindowLong(hwnd, index, style)

	if hints == HintMax {
		w.maxsz.X = int32(width)
		w.maxsz.Y = int32(height)
	} else if hints == HintMin {
		w.minsz.X = int32(width)
		w.minsz.Y = int32(height)
	} else {
		r := w32.Rect{}
		r.Left = 0
		r.Top = 0
		r.Right = int32(width)
		r.Bottom = int32(height)
		_, _, _ = w32.User32AdjustWindowRect.Call(uintptr(unsafe.Pointer(&r)), w32.WSOverlappedWindow, 0)
		_, _, _ = w32.User32SetWindowPos.Call(
			hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top),
			w32.SWPNoZOrder|w32.SWPNoActivate|w32.SWPNoMove|w32.SWPFrameChanged)
		w.browser.Resize()
	}
}

func (w *webview) Init(js string) {
	w.browser.Init(js)
}

func (w *webview) Eval(js string) {
	w.browser.Eval(js)
}

func (w *webview) Dispatch(f func()) {
	hwnd := atomic.LoadUintptr(&w.hwnd)
	if hwnd == 0 {
		return
	}
	w.m.Lock()
	w.dispatchq = append(w.dispatchq, f)
	w.m.Unlock()
	_, _, _ = w32.User32PostMessageW.Call(hwnd, w32.WMApp, 0, 0)
}

func (w *webview) Bind(name string, f interface{}) error {
	v := reflect.ValueOf(f)
	if v.Kind() != reflect.Func {
		return errors.New("only functions can be bound")
	}
	if n := v.Type().NumOut(); n > 2 {
		return errors.New("function may only return a value or a value+error")
	}
	w.m.Lock()
	w.bindings[name] = f
	w.m.Unlock()

	w.Init("(function() { var name = " + jsString(name) + ";" + `
		var RPC = window._rpc = (window._rpc || {nextSeq: 1});
		window[name] = function() {
		  var seq = RPC.nextSeq++;
		  var promise = new Promise(function(resolve, reject) {
			RPC[seq] = {
			  resolve: resolve,
			  reject: reject,
			};
		  });
		  window.external.invoke(JSON.stringify({
			id: seq,
			method: name,
			params: Array.prototype.slice.call(arguments),
		  }));
		  return promise;
		}
	})()`)

	return nil
}

func (w *webview) SetNewWindowHandler(handler func(url string) bool) {
	w.m.Lock()
	defer w.m.Unlock()
	w.newWindowHandler = handler
}
