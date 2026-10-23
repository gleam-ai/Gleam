//go:build windows

package systray

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	shell32              = syscall.NewLazyDLL("shell32.dll")
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	gdi32                = syscall.NewLazyDLL("gdi32.dll")
	comctl32             = syscall.NewLazyDLL("comctl32.dll")
	pShell_NotifyIconW   = shell32.NewProc("Shell_NotifyIconW")
	pCreateWindowExW     = user32.NewProc("CreateWindowExW")
	pDefWindowProcW      = user32.NewProc("DefWindowProcW")
	pDispatchMessageW    = user32.NewProc("DispatchMessageW")
	pGetMessageW         = user32.NewProc("GetMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pPostMessageW        = user32.NewProc("PostMessageW")
	pRegisterClassExW    = user32.NewProc("RegisterClassExW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pLoadIconMetric      = user32.NewProc("LoadIconMetric")
	pLoadCursorW         = user32.NewProc("LoadCursorW")
	pCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	pAppendMenuW         = user32.NewProc("AppendMenuW")
	pTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	pDestroyMenu         = user32.NewProc("DestroyMenu")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pDestroyWindow       = user32.NewProc("DestroyWindow")
	pGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	pLoadIconW           = user32.NewProc("LoadIconW")
	pLoadImageW          = user32.NewProc("LoadImageW")
	pRegisterWindowMsgW  = user32.NewProc("RegisterWindowMessageW")
)

const (
	imageIcon      = 1
	lrDefaultSize  = 0x00000040
	lrShared       = 0x00008000
	lrLoadFromFile = 0x00000010
)

const (
	wmApp          = 0x8000
	wmTrayCallback = wmApp + 1
	wmQuit         = wmApp + 2
	wmUpdateMenu   = wmApp + 3
	wmUpdateTip    = wmApp + 4
	wmUpdateIcon   = wmApp + 5

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifGuid    = 0x00000020

	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmCommand       = 0x0111

	mfString       = 0x00000000
	mfGrayed       = 0x00000001
	mfSep          = 0x00000800
	tpmBottomalign = 0x0020
	tpmRightalign  = 0x0008
	tpmLeftbutton  = 0x0000
	tpmReturncmd   = 0x0100

	csVRedraw     = 0x0001
	csHRedraw     = 0x0002
	csGlobalclass = 0x4000

	idiApplication = 32512
	idcArrow       = 32512
	colorWindow    = 5

	pmemMoveable   = 0x0002
	lrDefaultColor = 0x00000000
)

type point struct{ X, Y int32 }
type msg struct {
	Hwnd   uintptr
	Msg    uint32
	Wparam uintptr
	Lparam uintptr
	Time   int32
	Pt     point
}
type wndclassex struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	Hinstance     uintptr
	Hicon         uintptr
	Hcursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HiconSm       uintptr
}
type notifyicondata struct {
	CbSize           uint32
	Hwnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	Hicon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UTimeoutVersion  uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         guid
	HballoonIcon     uintptr
}
type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	theRunner   *Runner
	runnerMu    sync.Mutex
	menuCmds    = map[uint32]func(){}
	menuCounter = uint32(1000)
	hwnd        uintptr
	nidHicon    uintptr
	currentIcon []byte

	// After Explorer restarts the old icon disappears with the taskbar;
	// TaskbarCreated is the only signal to attach it again. Ignoring it leaves
	// the tray permanently blank and forces the user to kill the process.
	taskbarCreatedMsg uint32
)

func runPlatform(cfg Config) error {
	runnerMu.Lock()
	if theRunner != nil {
		runnerMu.Unlock()
		return nil
	}
	theRunner = &Runner{cfg: cfg, quit: make(chan struct{})}
	runnerMu.Unlock()

	startErr := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		instance, _, _ := pGetModuleHandleW.Call(0)
		if err := registerSystrayWindow(instance); err != nil {
			startErr <- err
			return
		}
		addNotifyIcon(cfg)
		startErr <- nil

		messageLoop()

		removeNotifyIcon()
	}()
	if err := <-startErr; err != nil {
		return err
	}

	<-theRunner.quit
	destroyMenuCmds()
	return nil
}

func registerSystrayWindow(instance uintptr) error {
	created, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	if m, _, _ := pRegisterWindowMsgW.Call(uintptr(unsafe.Pointer(created))); m != 0 {
		taskbarCreatedMsg = uint32(m)
	}
	className, _ := syscall.UTF16PtrFromString("GleamSystrayCls")
	windowName, _ := syscall.UTF16PtrFromString("GleamSystray")
	hc, _, _ := pLoadCursorW.Call(0, uintptr(idcArrow))
	wcx := wndclassex{
		CbSize:        uint32(unsafe.Sizeof(wndclassex{})),
		Style:         csHRedraw | csVRedraw,
		LpfnWndProc:   syscall.NewCallback(wndProc),
		Hinstance:     instance,
		Hcursor:       hc,
		HbrBackground: uintptr(colorWindow + 1),
		LpszClassName: className,
	}
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wcx)))
	ret, _, _ := pCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0, 0, 0, 0,
		0, 0, instance, 0,
	)
	if ret == 0 {
		return fmt.Errorf("systray: CreateWindowEx failed")
	}
	hwnd = ret
	return nil
}

func messageLoop() {
	var m msg
	for {
		ret, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func wndProc(hwndArg uintptr, msgID uint32, wparam, lparam uintptr) uintptr {
	switch msgID {
	case wmTrayCallback:
		switch uint32(lparam) {
		case wmLButtonUp, wmLButtonDblClk:
			runnerMu.Lock()
			fn := theRunner.cfg.OnActivate
			runnerMu.Unlock()
			if fn != nil {
				go fn()
			}
		case wmRButtonUp:
			showMenu()
		}
	case wmCommand:
		if fn, ok := menuCmds[uint32(wparam)]; ok {
			go fn()
		}
	case wmUpdateMenu:
		runnerMu.Lock()
		items := theRunner.cfg.MenuItems
		runnerMu.Unlock()
		rebuildMenuItems(items)
	case wmUpdateTip:
		runnerMu.Lock()
		tip := theRunner.cfg.Tooltip
		runnerMu.Unlock()
		modifyTip(tip)
	case wmUpdateIcon:
		runnerMu.Lock()
		b := currentIcon
		runnerMu.Unlock()
		modifyIcon(b)
	case wmQuit:
		removeNotifyIcon()
		pPostQuitMessage.Call(0)
	default:
		if taskbarCreatedMsg != 0 && msgID == taskbarCreatedMsg {
			runnerMu.Lock()
			cfg := theRunner.cfg
			runnerMu.Unlock()
			addNotifyIcon(cfg)
		}
	}
	ret, _, _ := pDefWindowProcW.Call(hwndArg, uintptr(msgID), wparam, lparam)
	return ret
}

func addNotifyIcon(cfg Config) {
	icon := loadIconFromBytes(cfg.IconBytes)
	nidHicon = icon
	nid := notifyicondata{
		CbSize:           uint32(unsafe.Sizeof(notifyicondata{})),
		Hwnd:             hwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmTrayCallback,
		Hicon:            icon,
	}
	copy(nid.SzTip[:], utf16Trunc(cfg.Tooltip, 127))
	pShell_NotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	rebuildMenuItems(cfg.MenuItems)
}

func removeNotifyIcon() {
	nid := notifyicondata{CbSize: uint32(unsafe.Sizeof(notifyicondata{})), Hwnd: hwnd, UID: 1}
	pShell_NotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

func modifyTip(tip string) {
	nid := notifyicondata{
		CbSize: uint32(unsafe.Sizeof(notifyicondata{})),
		Hwnd:   hwnd, UID: 1, UFlags: nifTip,
	}
	copy(nid.SzTip[:], utf16Trunc(tip, 127))
	pShell_NotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

func modifyIcon(b []byte) {
	icon := loadIconFromBytes(b)
	if icon == 0 {
		return
	}
	nidHicon = icon
	nid := notifyicondata{
		CbSize: uint32(unsafe.Sizeof(notifyicondata{})),
		Hwnd:   hwnd, UID: 1, UFlags: nifIcon, Hicon: icon,
	}
	pShell_NotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

func loadIconFromBytes(ico []byte) uintptr {
	if len(ico) >= 6 && ico[0] == 0 && ico[1] == 0 && ico[2] == 1 && ico[3] == 0 {
		// LoadImageW only accepts a disk path; deleting the temp file is safe
		// because the icon data has already been copied into the HICON.
		if f, err := os.CreateTemp("", "gleam-tray-*.ico"); err == nil {
			name := f.Name()
			_, werr := f.Write(ico)
			cerr := f.Close()
			if werr == nil && cerr == nil {
				p, _ := syscall.UTF16PtrFromString(name)
				if icon, _, _ := pLoadImageW.Call(0, uintptr(unsafe.Pointer(p)), imageIcon, 0, 0,
					lrLoadFromFile|lrDefaultSize); icon != 0 {
					_ = os.Remove(name)
					return icon
				}
			}
			_ = os.Remove(name)
		}
	}
	if h, _, _ := pGetModuleHandleW.Call(0); h != 0 {
		if icon, _, _ := pLoadImageW.Call(h, uintptr(1), imageIcon, 0, 0, lrDefaultSize|lrShared); icon != 0 {
			return icon
		}
	}
	h, _, _ := pLoadIconW.Call(0, uintptr(32512))
	return h
}

func showMenu() {
	hMenu, _, _ := pCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	runnerMu.Lock()
	items := theRunner.cfg.MenuItems
	runnerMu.Unlock()
	buildMenu(hMenu, items)
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(hwnd)
	ret, _, _ := pTrackPopupMenu.Call(
		hMenu,
		uintptr(tpmBottomalign|tpmRightalign|tpmLeftbutton|tpmReturncmd),
		uintptr(pt.X), uintptr(pt.Y),
		0, hwnd, 0,
	)
	if ret != 0 {
		if fn, ok := menuCmds[uint32(ret)]; ok {
			go fn()
		}
	}
	pDestroyMenu.Call(hMenu)
}

func buildMenu(hMenu uintptr, items []MenuItem) {
	for _, it := range items {
		menuCounter++
		id := menuCounter
		menuCmds[id] = it.OnClick
		flags := uintptr(mfString)
		if it.Disabled {
			flags |= mfGrayed
		}
		textPtr, _ := syscall.UTF16PtrFromString(it.Title)
		pAppendMenuW.Call(hMenu, flags, uintptr(id), uintptr(unsafe.Pointer(textPtr)))
		if it.SepAfter {
			pAppendMenuW.Call(hMenu, mfSep, 0, 0)
		}
	}
}

func rebuildMenuItems(items []MenuItem) {
	runnerMu.Lock()
	theRunner.cfg.MenuItems = items
	runnerMu.Unlock()
}

func destroyMenuCmds() {
	menuCmds = map[uint32]func(){}
}

func quitPlatform() {
	if hwnd != 0 {
		pPostMessageW.Call(hwnd, wmQuit, 0, 0)
	}
	runnerMu.Lock()
	if theRunner != nil && !theRunner.closed {
		theRunner.closed = true
		close(theRunner.quit)
	}
	runnerMu.Unlock()
}

func setTooltipPlatform(tip string) {
	runnerMu.Lock()
	if theRunner == nil {
		runnerMu.Unlock()
		return
	}
	theRunner.cfg.Tooltip = tip
	runnerMu.Unlock()
	if hwnd != 0 {
		pPostMessageW.Call(hwnd, wmUpdateTip, 0, 0)
	}
}

func setIconPlatform(ico []byte) {
	runnerMu.Lock()
	if theRunner == nil {
		runnerMu.Unlock()
		return
	}
	currentIcon = ico
	runnerMu.Unlock()
	if hwnd != 0 {
		pPostMessageW.Call(hwnd, wmUpdateIcon, 0, 0)
	}
}

func updateMenuPlatform(items []MenuItem) {
	runnerMu.Lock()
	if theRunner == nil {
		runnerMu.Unlock()
		return
	}
	theRunner.cfg.MenuItems = items
	runnerMu.Unlock()
}

func utf16Trunc(s string, n int) []uint16 {
	u, _ := syscall.UTF16FromString(s)
	if len(u) > n {
		u = u[:n]
		u[len(u)-1] = 0
	}
	return u
}
