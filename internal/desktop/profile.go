package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"gleam/internal/buildinfo"
)

func profileDirPath() string {
	return profileDirPathFor(buildinfo.Version)
}

func profileDirPathFor(ver string) string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	ver = strings.TrimSpace(ver)
	if ver == "" {
		ver = "dev"
	}
	ver = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		default:
			return r
		}
	}, ver)
	return filepath.Join(home, ".gleam", "browser-profile", ver)
}

func alternateProfileDir(primary string) string {
	if primary == "" {
		return "browser-profile-alt"
	}
	return primary + "-alt"
}

func chromiumProfileLockNames() []string {
	return []string{"SingletonLock", "SingletonCookie", "lockfile"}
}

func profileLockPaths(dir string) []string {
	names := chromiumProfileLockNames()
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.Join(dir, n))
	}
	return out
}

func anyProfileLockPresent(dir string) bool {
	for _, p := range profileLockPaths(dir) {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func removeStaleProfileLocks(dir string) {
	for _, p := range profileLockPaths(dir) {
		_ = os.Remove(p)
	}
}

func waitUntil(timeout, interval time.Duration, cond func() bool) bool {
	if cond == nil {
		return false
	}
	if cond() {
		return true
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(interval)
		if cond() {
			return true
		}
	}
	return cond()
}

func chromeAppArgs(target, profile, appName string) []string {
	return []string{
		"--app=" + target,
		"--window-size=1280,840",
		"--app-name=" + appName,
		"--window-name=" + appName,
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=msWebOOUI,msPdfOOUI,msSmartScreenProtection,Translate",
		"--disable-session-crashed-bubble",
		"--disable-infobars",
	}
}

func gpuSoftFallbackFlags() []string {
	return []string{
		"--disable-gpu",
		"--disable-gpu-compositing",
	}
}

func chromeAppArgsWithGPUFallback(target, profile, appName string) []string {
	return append(chromeAppArgs(target, profile, appName), gpuSoftFallbackFlags()...)
}

func appShellReady(titledWindowCount int, readinessOK bool) bool {
	return readinessOK && titledWindowCount > 0
}

func shouldRetryAppLaunch(attempt int, titledWindowCount int, readinessOK bool) bool {
	return attempt == 0 && !appShellReady(titledWindowCount, readinessOK)
}

func preferEdgeOverChrome(paths []string) []string {
	if len(paths) <= 1 {
		return paths
	}
	var edge, rest []string
	for _, p := range paths {
		base := filepath.Base(p)
		if base == "msedge.exe" {
			edge = append(edge, p)
			continue
		}
		rest = append(rest, p)
	}
	return append(edge, rest...)
}
