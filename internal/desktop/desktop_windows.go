//go:build windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type windowProc interface {
	Call(...uintptr) (uintptr, uintptr, error)
}

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")

	pEnumWindows              windowProc = user32.NewProc("EnumWindows")
	pFindWindowExW            windowProc = user32.NewProc("FindWindowExW")
	pGetWindowTextW           windowProc = user32.NewProc("GetWindowTextW")
	pGetWindowTextLengthW     windowProc = user32.NewProc("GetWindowTextLengthW")
	pIsWindow                 windowProc = user32.NewProc("IsWindow")
	pIsWindowVisible          windowProc = user32.NewProc("IsWindowVisible")
	pIsIconic                 windowProc = user32.NewProc("IsIconic")
	pGetWindowThreadProcessId windowProc = user32.NewProc("GetWindowThreadProcessId")
	pGetForegroundWindow      windowProc = user32.NewProc("GetForegroundWindow")
	pAttachThreadInput        windowProc = user32.NewProc("AttachThreadInput")
	pSetForegroundWindow      windowProc = user32.NewProc("SetForegroundWindow")
	pShowWindow               windowProc = user32.NewProc("ShowWindow")
	pBringWindowToTop         windowProc = user32.NewProc("BringWindowToTop")
	pPostMessageW             windowProc = user32.NewProc("PostMessageW")
	pGetCurrentThreadId       windowProc = kernel32.NewProc("GetCurrentThreadId")
	pDwmSetWindowAttribute    windowProc = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	swRestore = 9
	swShow    = 5
	wmClose   = 0x0010

	dwmwaUseImmersiveDarkMode = 33
	dwmwaBorderColor          = 34
	dwmwaCaptionColor         = 35
	dwmwaTextColor            = 36

	appLaunchReadyTimeout = 8 * time.Second
	appCloseWaitTimeout   = 5 * time.Second
	pollInterval          = 200 * time.Millisecond
)

const windowTitle = "Gleam · 微光"

var (
	mu        sync.Mutex
	procs     []*exec.Cmd
	launching bool

	waitForWindow = waitUntil
	runTaskkill   = func(cmd *exec.Cmd) error { return cmd.Run() }
	startBrowser  = func(cmd *exec.Cmd) error { return cmd.Start() }
	waitBrowser   = func(cmd *exec.Cmd) error { return cmd.Wait() }
	stopBrowser   = func(cmd *exec.Cmd) { _ = cmd.Process.Kill() }
	clearLocks    = removeStaleProfileLocks

	readWindowTitle = windowTitleOf
	readWindowPID   = windowPIDOf
)

func windowTitleOf(hwnd uintptr) string {
	n, _, _ := pGetWindowTextLengthW.Call(hwnd)
	if n <= 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	got, _, _ := pGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if got <= 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:got])
}

func windowPIDOf(hwnd uintptr) uint32 {
	var pid uint32
	if tid, _, _ := pGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid))); tid == 0 {
		return 0
	}
	return pid
}

func browserPaths() []string {
	if env := os.Getenv("GLEAM_BROWSER"); env != "" {
		return []string{env}
	}
	local := os.Getenv("LOCALAPPDATA")
	programs64 := os.Getenv("ProgramFiles")
	programsX86 := os.Getenv("ProgramFiles(x86)")
	raw := []string{
		filepath.Join(local, "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(programs64, "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(programsX86, "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(local, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(programs64, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(programsX86, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(local, "Chromium", "Application", "chrome.exe"),
		filepath.Join(local, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
	}
	return preferEdgeOverChrome(raw)
}

func profileDir() string { return profileDirPath() }

func profileBrowserPID(profile string) uint32 {
	if profile == "" {
		return 0
	}
	name, err := syscall.UTF16PtrFromString(profile)
	if err != nil {
		return 0
	}
	// Chromium's ProcessSingleton names its message-only window after user-data-dir.
	class, _ := syscall.UTF16PtrFromString("Chrome_MessageWindow")
	hwnd, _, _ := pFindWindowExW.Call(^uintptr(2), 0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(name)))
	if hwnd == 0 {
		return 0
	}
	return readWindowPID(hwnd)
}

func ownsBrowserPID(pid uint32) bool {
	if pid == 0 {
		return false
	}
	primary := profileDir()
	return pid == profileBrowserPID(primary) || pid == profileBrowserPID(alternateProfileDir(primary))
}

func gleamWindowPID(hwnd uintptr) uint32 {
	pid := readWindowPID(hwnd)
	if !ownsBrowserPID(pid) {
		return 0
	}
	return pid
}

func clearUnusedProfileLocks(profile string) {
	if profileBrowserPID(profile) == 0 {
		clearLocks(profile)
	}
}

func resolveBrowserExe() string {
	for _, p := range browserPaths() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func openWindowPlatform(target string, onClose func()) bool {
	if hwnds := findGleamWindows(); len(hwnds) > 0 {
		for _, h := range hwnds {
			activateWindow(h)
		}
		return true
	}

	mu.Lock()
	if launching {
		mu.Unlock()
		return true
	}
	launching = true
	mu.Unlock()
	clearLaunching := func() {
		mu.Lock()
		launching = false
		mu.Unlock()
	}

	profile := profileDir()
	_ = os.MkdirAll(profile, 0o755)

	if anyProfileLockPresent(profile) {
		if !waitForWindow(2*time.Second, pollInterval, func() bool {
			return !anyProfileLockPresent(profile) || len(findGleamWindows()) > 0
		}) {
			if hwnds := findGleamWindows(); len(hwnds) > 0 {
				for _, h := range hwnds {
					activateWindow(h)
				}
				clearLaunching()
				return true
			}
		}
		if hwnds := findGleamWindows(); len(hwnds) > 0 {
			for _, h := range hwnds {
				activateWindow(h)
			}
			clearLaunching()
			return true
		}
		clearUnusedProfileLocks(profile)
	}

	exe := resolveBrowserExe()
	if exe == "" {
		clearLaunching()
		return false
	}

	ok, pid := launchAppAttempt(exe, chromeAppArgs(target, profile, windowTitle), onClose)
	if ok {
		ready := waitForWindow(appLaunchReadyTimeout, pollInterval, func() bool {
			return len(findGleamWindows()) > 0
		})
		hwnds := findGleamWindows()
		if appShellReady(len(hwnds), ready) {
			clearLaunching()
			return true
		}
		teardownAppSpawn(pid, profile)
		if !shouldRetryAppLaunch(0, len(hwnds), ready) {
			clearLaunching()
			return false
		}
	} else if pid > 0 {
		teardownAppSpawn(pid, profile)
	}

	alt := alternateProfileDir(profile)
	_ = os.MkdirAll(alt, 0o755)
	clearUnusedProfileLocks(alt)
	ok, pid = launchAppAttempt(exe, chromeAppArgsWithGPUFallback(target, alt, windowTitle), onClose)
	if !ok {
		clearLaunching()
		return false
	}
	ready := waitForWindow(appLaunchReadyTimeout, pollInterval, func() bool {
		return len(findGleamWindows()) > 0
	})
	hwnds := findGleamWindows()
	if appShellReady(len(hwnds), ready) {
		clearLaunching()
		return true
	}
	teardownAppSpawn(pid, alt)
	clearLaunching()
	return false
}

func launchAppAttempt(exe string, args []string, onClose func()) (bool, int) {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := startBrowser(cmd); err != nil {
		return false, 0
	}
	mu.Lock()
	procs = append(procs, cmd)
	pid := cmd.Process.Pid
	mu.Unlock()

	go reapAppProcess(cmd, onClose)
	return true, pid
}

func reapAppProcess(cmd *exec.Cmd, onClose func()) {
	_ = waitBrowser(cmd)
	mu.Lock()
	for i, p := range procs {
		if p == cmd {
			procs = append(procs[:i], procs[i+1:]...)
			break
		}
	}
	mu.Unlock()
	if onClose != nil {
		for windowOpenPlatform() {
			time.Sleep(pollInterval)
		}
		onClose()
	}
}

func teardownAppSpawn(pid int, profile string) {
	mu.Lock()
	var target *exec.Cmd
	for i, p := range procs {
		if p.Process != nil && p.Process.Pid == pid {
			target = p
			procs = append(procs[:i], procs[i+1:]...)
			break
		}
	}
	mu.Unlock()
	if target != nil {
		stopBrowser(target)
	}
	killProcessTree(profileBrowserPID(profile))
	clearUnusedProfileLocks(profile)
}

func closeWindowPlatform() {
	hwnds := findGleamWindows()
	for _, h := range hwnds {
		if gleamWindowPID(h) != 0 {
			pPostMessageW.Call(h, wmClose, 0, 0)
		}
	}

	_ = waitForWindow(appCloseWaitTimeout, pollInterval, func() bool {
		return len(findGleamWindows()) == 0
	})

	primary := profileDir()
	killProcessTree(profileBrowserPID(primary))
	killProcessTree(profileBrowserPID(alternateProfileDir(primary)))

	mu.Lock()
	targets := append([]*exec.Cmd(nil), procs...)
	procs = nil
	launching = false
	mu.Unlock()
	for _, c := range targets {
		if c.Process != nil {
			// Use the launch handle, not a PID that may have been reused after handoff.
			stopBrowser(c)
		}
	}

	_ = waitForWindow(2*time.Second, pollInterval, func() bool {
		return len(findGleamWindows()) == 0
	})

	if len(findGleamWindows()) == 0 {
		clearUnusedProfileLocks(primary)
		clearUnusedProfileLocks(alternateProfileDir(primary))
	}
}

func killProcessTree(pid uint32) {
	if !ownsBrowserPID(pid) {
		return
	}
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = runTaskkill(cmd)
}

func windowOpenPlatform() bool {
	primary := profileDir()
	if profileBrowserPID(primary) != 0 || profileBrowserPID(alternateProfileDir(primary)) != 0 {
		return true
	}
	mu.Lock()
	defer mu.Unlock()
	return launching || len(procs) > 0
}

var (
	enumMu    sync.Mutex
	enumFound []uintptr
	enumCb    = syscall.NewCallback(enumProc)
)

func findGleamWindows() []uintptr {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumFound = enumFound[:0]
	pEnumWindows.Call(enumCb, 0)
	out := append([]uintptr(nil), enumFound...)
	enumFound = enumFound[:0]
	return out
}

func enumProc(hwnd uintptr, _ uintptr) uintptr {
	alive, _, _ := pIsWindow.Call(hwnd)
	if alive == 0 || gleamWindowPID(hwnd) == 0 {
		return 1
	}
	title := readWindowTitle(hwnd)
	if title == "" || !strings.Contains(title, windowTitle) {
		return 1
	}
	vis, _, _ := pIsWindowVisible.Call(hwnd)
	iconic, _, _ := pIsIconic.Call(hwnd)
	if vis == 0 || iconic != 0 {
		pShowWindow.Call(hwnd, swRestore)
		pShowWindow.Call(hwnd, swShow)
	}
	applyDarkTitlebar(hwnd)
	enumFound = append(enumFound, hwnd)
	return 1
}

func applyDarkTitlebar(hwnd uintptr) {
	var (
		useDark int32  = 1
		caption uint32 = 0x00120D0B
		text    uint32 = 0x00F2F2F2
	)
	pDwmSetWindowAttribute.Call(hwnd, uintptr(dwmwaUseImmersiveDarkMode),
		uintptr(unsafe.Pointer(&useDark)), unsafe.Sizeof(useDark))
	pDwmSetWindowAttribute.Call(hwnd, uintptr(dwmwaCaptionColor),
		uintptr(unsafe.Pointer(&caption)), unsafe.Sizeof(caption))
	pDwmSetWindowAttribute.Call(hwnd, uintptr(dwmwaTextColor),
		uintptr(unsafe.Pointer(&text)), unsafe.Sizeof(text))
}

func activateWindow(hwnd uintptr) {
	if gleamWindowPID(hwnd) == 0 {
		return
	}
	pShowWindow.Call(hwnd, swRestore)
	pShowWindow.Call(hwnd, swShow)

	curTid, _, _ := pGetCurrentThreadId.Call()
	fg, _, _ := pGetForegroundWindow.Call()
	fgTid := uintptr(0)
	if fg != 0 && fg != hwnd {
		r, _, _ := pGetWindowThreadProcessId.Call(fg, 0)
		fgTid = r
	}

	attached := false
	if fgTid != 0 && fgTid != curTid {
		if ok, _, _ := pAttachThreadInput.Call(curTid, fgTid, 1); ok != 0 {
			attached = true
		}
	}
	pSetForegroundWindow.Call(hwnd)
	pBringWindowToTop.Call(hwnd)
	if attached {
		pAttachThreadInput.Call(curTid, fgTid, 0)
	}
}
