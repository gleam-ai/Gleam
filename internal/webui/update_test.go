package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareVersionsSemVer(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"v0.0.16", "0.0.17", -1},
		{"1.10.0", "1.9.9", 1},
		{"1.0.0-rc.2", "1.0.0-rc.10", -1},
		{"1.0.0-rc.10", "1.0.0", -1},
		{"1.0.0-beta", "1.0.0-alpha", 1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0+20261022", "1.0.0+20261001", 0},
		{"1.0.0-rc.1+build.5", "1.0.0-rc.1+build.6", 0},
	}
	for _, tc := range cases {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestAssetNamesForNeverReturnsInstaller(t *testing.T) {
	for _, tc := range []struct{ goos, goarch string }{
		{"windows", "amd64"},
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"darwin", "amd64"},
		{"darwin", "arm64"},
	} {
		for _, name := range assetNamesFor(tc.goos, tc.goarch) {
			if strings.Contains(strings.ToLower(name), "setup") {
				t.Fatalf("%s/%s: installer %q must not be an update target", tc.goos, tc.goarch, name)
			}
		}
	}
	if names := assetNamesFor("windows", "amd64"); len(names) != 1 || names[0] != "gleam-windows-amd64.exe" {
		t.Fatalf("windows asset names = %v", names)
	}
}

func TestSelectUpdateAssetExactMatch(t *testing.T) {
	assets := []releaseAsset{
		{Name: "Gleam-Setup.exe", URL: "https://github.com/gleam-ai/Gleam/releases/download/v1/Gleam-Setup.exe", Size: 260 << 20},
		{Name: "gleam-windows-amd64.exe.blockmap", URL: "https://github.com/x/y", Size: 1024},
		{Name: "gleam-windows-amd64-20261022.exe", URL: "https://github.com/x/y", Size: 16 << 20},
		{Name: "gleam-windows-amd64.exe", URL: "https://github.com/gleam-ai/Gleam/releases/download/v1/gleam-windows-amd64.exe", Size: 16 << 20},
	}
	got, ok := selectUpdateAsset(assets, "windows", "amd64")
	if !ok || got.Name != "gleam-windows-amd64.exe" {
		t.Fatalf("picked %+v (ok=%v)", got, ok)
	}
}

func TestSelectUpdateAssetRejectsUntrustedHostAndOversize(t *testing.T) {
	for _, a := range []releaseAsset{
		{Name: "gleam-linux-amd64", URL: "https://evil.example/gleam-linux-amd64", Size: 16 << 20},
		{Name: "gleam-linux-amd64", URL: "http://github.com/gleam-linux-amd64", Size: 16 << 20},
		{Name: "gleam-linux-amd64", URL: "https://github.com/gleam-linux-amd64", Size: 512 << 20},
		{Name: "gleam-linux-amd64", URL: "https://github.com/gleam-linux-amd64", Size: 1024},
	} {
		if got, ok := selectUpdateAsset([]releaseAsset{a}, "linux", "amd64"); ok {
			t.Fatalf("accepted bad asset %+v", got)
		}
	}
	if _, ok := selectUpdateAsset(nil, "windows", "arm64"); ok {
		t.Fatal("unsupported platform must not select an asset")
	}
}

func TestSelectUpdateAssetRejectsInstallerSizedEngine(t *testing.T) {
	asset := releaseAsset{
		Name: "gleam-windows-amd64.exe",
		URL:  "https://github.com/gleam-ai/Gleam/releases/download/v1/gleam-windows-amd64.exe",
		Size: 265059619, // Gleam-Setup.exe 0.0.17
	}
	if got, ok := selectUpdateAsset([]releaseAsset{asset}, "windows", "amd64"); ok {
		t.Fatalf("installer-sized asset accepted: %+v", got)
	}
}

func TestInDesktopResources(t *testing.T) {
	cases := []struct {
		exe  string
		want bool
	}{
		{`C:\Software\Gleam\resources\bin\gleam.exe`, true},
		{"/Applications/Gleam.app/Contents/Resources/bin/gleam", true},
		{`D:\PersonalProject\Gleam\desktop\.sidecar\win32-x64\gleam.exe`, false},
		{`C:\Tools\gleam.exe`, false},
		{"/usr/local/bin/gleam", false},
		{`C:\Software\Gleam\resources\bin\other.exe`, false},
	}
	for _, tc := range cases {
		if got := inDesktopResources(tc.exe); got != tc.want {
			t.Errorf("inDesktopResources(%q) = %v, want %v", tc.exe, got, tc.want)
		}
	}
}

func TestExecutableMagic(t *testing.T) {
	cases := []struct {
		goos string
		head []byte
		want bool
	}{
		{"windows", []byte{'M', 'Z', 0x90, 0x00}, true},
		{"windows", []byte{0x7f, 'E', 'L', 'F'}, false},
		{"linux", []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0}, true},
		{"linux", []byte{'M', 'Z', 0, 0}, false},
		{"darwin", []byte{0xcf, 0xfa, 0xed, 0xfe, 7, 0, 0, 1}, true},
		{"darwin", []byte{0xca, 0xfe, 0xba, 0xbe, 0, 0, 0, 2}, true},
		{"darwin", []byte{0x7f, 'E', 'L', 'F'}, false},
	}
	for _, tc := range cases {
		if got := executableMagic(tc.head, tc.goos); got != tc.want {
			t.Fatalf("%s %x: got %v want %v", tc.goos, tc.head, got, tc.want)
		}
	}
}

func TestVerifyUpdateBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gleam.exe")
	body := make([]byte, updateMinBytes+1024)
	copy(body, []byte{'M', 'Z'})
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	asset := releaseAsset{Name: "gleam-windows-amd64.exe", Size: int64(len(body)), Digest: "sha256:" + hex.EncodeToString(digest[:])}
	if err := verifyUpdateBinary(path, asset, "windows"); err != nil {
		t.Fatalf("valid binary rejected: %v", err)
	}

	if err := verifyUpdateBinary(path, releaseAsset{Digest: "sha256:" + strings.Repeat("0", 64)}, "windows"); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	if err := verifyUpdateBinary(path, asset, "linux"); err == nil {
		t.Fatal("wrong platform magic accepted")
	}

	small := filepath.Join(dir, "small.exe")
	if err := os.WriteFile(small, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyUpdateBinary(small, releaseAsset{}, "windows"); err == nil {
		t.Fatal("undersized binary accepted")
	}
}

func TestUpdateApplyDisabledInDesktop(t *testing.T) {
	s := &Server{DisableSelfUpdate: true}
	rec := httptest.NewRecorder()
	s.handleUpdateApply(rec, httptest.NewRequest(http.MethodPost, "/api/update/apply", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestInMacAppBundle(t *testing.T) {
	cases := []struct {
		exe  string
		goos string
		want bool
	}{
		{"/Applications/Gleam.app/Contents/Resources/bin/gleam", "darwin", true},
		{"/Applications/Gleam.APP/Contents/Resources/bin/gleam", "darwin", true},
		{"/Users/me/Gleam.app/Contents/MacOS/gleam", "darwin", true},
		{"/usr/local/bin/gleam", "darwin", false},
		{"/tmp/Gleam.app/Contents/bin/gleam", "linux", false},
		{"/tmp/Gleam.app/Contents/bin/gleam.exe", "windows", false},
		{"", "darwin", false},
	}
	for _, tc := range cases {
		if got := inMacAppBundle(tc.exe, tc.goos); got != tc.want {
			t.Errorf("inMacAppBundle(%q, %q) = %v, want %v", tc.exe, tc.goos, got, tc.want)
		}
	}
}

func TestTrustedAssetURL(t *testing.T) {
	for _, raw := range []string{
		"https://github.com/gleam-ai/Gleam/releases/download/v1/x",
		"https://objects.githubusercontent.com/x",
		"https://release-assets.githubusercontent.com/x",
	} {
		if !trustedAssetURL(raw) {
			t.Fatalf("%s should be trusted", raw)
		}
	}
	for _, raw := range []string{
		"http://github.com/x",
		"https://github.com.evil.example/x",
		"https://evil.example/github.com/x",
		"file:///etc/passwd",
	} {
		if trustedAssetURL(raw) {
			t.Fatalf("%s must not be trusted", raw)
		}
	}
}
