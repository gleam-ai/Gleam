//go:build windows

package desktop

import (
	"fmt"
	"os/exec"
	"slices"
	"testing"
	"time"
)

type fakeWindowProc func(...uintptr) (uintptr, uintptr, error)

func (f fakeWindowProc) Call(args ...uintptr) (uintptr, uintptr, error) { return f(args...) }

type nativeWindow struct {
	pid     uint32
	title   string
	visible bool
	iconic  bool
}

type nativeRecorder struct {
	windows     map[uintptr]nativeWindow
	messageHWND uintptr
	messagePID  uint32
	shown       []uintptr
	active      []uintptr
	styled      []uintptr
	closed      []uintptr
	killed      []string
	stopped     []*exec.Cmd
	locks       []string
}

func stubNativeWindows(t *testing.T) *nativeRecorder {
	t.Helper()
	r := &nativeRecorder{windows: make(map[uintptr]nativeWindow)}
	mu.Lock()
	oldProcs, oldLaunching := procs, launching
	procs, launching = nil, false
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		procs, launching = oldProcs, oldLaunching
		mu.Unlock()
	})
	bind := func(dst *windowProc, f fakeWindowProc) {
		old := *dst
		*dst = f
		t.Cleanup(func() { *dst = old })
	}
	noop := fakeWindowProc(func(...uintptr) (uintptr, uintptr, error) { return 1, 0, nil })
	bind(&pEnumWindows, func(...uintptr) (uintptr, uintptr, error) {
		for hwnd := range r.windows {
			enumProc(hwnd, 0)
		}
		return 1, 0, nil
	})
	bind(&pFindWindowExW, func(...uintptr) (uintptr, uintptr, error) {
		return r.messageHWND, 0, nil
	})
	bind(&pIsWindow, func(a ...uintptr) (uintptr, uintptr, error) {
		if _, ok := r.windows[a[0]]; ok {
			return 1, 0, nil
		}
		return 0, 0, nil
	})
	bind(&pIsWindowVisible, func(a ...uintptr) (uintptr, uintptr, error) {
		if r.windows[a[0]].visible {
			return 1, 0, nil
		}
		return 0, 0, nil
	})
	bind(&pIsIconic, func(a ...uintptr) (uintptr, uintptr, error) {
		if r.windows[a[0]].iconic {
			return 1, 0, nil
		}
		return 0, 0, nil
	})
	bind(&pShowWindow, func(a ...uintptr) (uintptr, uintptr, error) {
		r.shown = append(r.shown, a[0])
		return 1, 0, nil
	})
	bind(&pSetForegroundWindow, func(a ...uintptr) (uintptr, uintptr, error) {
		r.active = append(r.active, a[0])
		return 1, 0, nil
	})
	bind(&pBringWindowToTop, noop)
	bind(&pGetForegroundWindow, func(...uintptr) (uintptr, uintptr, error) { return 0, 0, nil })
	bind(&pGetCurrentThreadId, noop)
	bind(&pAttachThreadInput, noop)
	bind(&pDwmSetWindowAttribute, func(a ...uintptr) (uintptr, uintptr, error) {
		r.styled = append(r.styled, a[0])
		return 1, 0, nil
	})
	bind(&pPostMessageW, func(a ...uintptr) (uintptr, uintptr, error) {
		if a[1] != wmClose {
			t.Fatalf("unexpected message: %v", a)
		}
		r.closed = append(r.closed, a[0])
		return 1, 0, nil
	})
	oldTitle, oldPID := readWindowTitle, readWindowPID
	readWindowTitle = func(hwnd uintptr) string { return r.windows[hwnd].title }
	readWindowPID = func(hwnd uintptr) uint32 {
		if hwnd == r.messageHWND {
			return r.messagePID
		}
		return r.windows[hwnd].pid
	}
	oldWait, oldKill, oldStart, oldReap, oldStop, oldLocks := waitForWindow, runTaskkill, startBrowser, waitBrowser, stopBrowser, clearLocks
	waitForWindow = func(_, _ time.Duration, cond func() bool) bool { return cond() }
	runTaskkill = func(cmd *exec.Cmd) error {
		r.killed = append(r.killed, cmd.Args[len(cmd.Args)-1])
		return nil
	}
	startBrowser = func(*exec.Cmd) error { t.Error("unexpected browser launch"); return fmt.Errorf("stub") }
	waitBrowser = func(*exec.Cmd) error { return nil }
	stopBrowser = func(cmd *exec.Cmd) { r.stopped = append(r.stopped, cmd) }
	clearLocks = func(profile string) { r.locks = append(r.locks, profile) }
	t.Cleanup(func() {
		readWindowTitle, readWindowPID = oldTitle, oldPID
		waitForWindow, runTaskkill, startBrowser, waitBrowser, stopBrowser, clearLocks = oldWait, oldKill, oldStart, oldReap, oldStop, oldLocks
	})
	return r
}

func TestNativeForeignTitleNeverOwnsWindow(t *testing.T) {
	r := stubNativeWindows(t)
	r.windows[101] = nativeWindow{pid: 4242, title: windowTitle + " - Google Chrome", iconic: true}
	if got := findGleamWindows(); len(got) != 0 {
		t.Errorf("foreign window selected: %v", got)
	}
	if windowOpenPlatform() {
		t.Error("foreign window reported as open")
	}
	activateWindow(101)
	closeWindowPlatform()
	if len(r.shown)+len(r.active)+len(r.styled)+len(r.closed)+len(r.killed) != 0 {
		t.Fatalf("foreign window affected: shown=%v active=%v styled=%v WM_CLOSE=%v taskkill=%v", r.shown, r.active, r.styled, r.closed, r.killed)
	}
}

func TestNativeOnlyOwnedProfileWindowIsClosed(t *testing.T) {
	r := stubNativeWindows(t)
	r.messageHWND, r.messagePID = 900, 777
	r.windows[900] = nativeWindow{pid: 777}
	r.windows[101] = nativeWindow{pid: 777, title: windowTitle}
	r.windows[102] = nativeWindow{pid: 888, title: windowTitle}

	if got := findGleamWindows(); !slices.Equal(got, []uintptr{101}) {
		t.Fatalf("owned windows=%v, want [101]", got)
	}
	closeWindowPlatform()
	if !slices.Equal(r.closed, []uintptr{101}) {
		t.Fatalf("WM_CLOSE targets=%v, want [101]", r.closed)
	}
	if !slices.Equal(r.killed, []string{"777", "777"}) {
		t.Fatalf("taskkill targets=%v, want primary+alternate profile pid", r.killed)
	}
}
