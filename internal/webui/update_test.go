package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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

func TestTrustedAssetAPIURL(t *testing.T) {
	good := "https://api.github.com/repos/gleam-ai/Gleam/releases/assets/1"
	if !trustedAssetAPIURL(good) {
		t.Fatalf("%s should be trusted", good)
	}
	for _, raw := range []string{
		"http://api.github.com/repos/gleam-ai/Gleam/releases/assets/1",
		"https://api.github.com.evil.example/repos/gleam-ai/Gleam/releases/assets/1",
		"https://api.github.com/repos/other/repo/releases/assets/1",
		"https://api.github.com/repos/gleam-ai/Gleam/releases",
		"https://github.com/gleam-ai/Gleam/releases/download/v1/x",
		"",
	} {
		if trustedAssetAPIURL(raw) {
			t.Fatalf("%s must not be trusted", raw)
		}
	}
}

func TestReleaseAssetDownloadURL(t *testing.T) {
	api := "https://api.github.com/repos/gleam-ai/Gleam/releases/assets/1"
	web := "https://github.com/gleam-ai/Gleam/releases/download/v1/x"
	if got := (releaseAsset{APIURL: api, URL: web}).downloadURL(); got != api {
		t.Fatalf("API URL should win, got %q", got)
	}
	if got := (releaseAsset{URL: web}).downloadURL(); got != web {
		t.Fatalf("web URL should be the fallback, got %q", got)
	}
	if got := (releaseAsset{URL: "https://evil.example/x"}).downloadURL(); got != "" {
		t.Fatalf("untrusted URL accepted: %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func withFakeGitHub(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		handler(rec, r)
		return rec.Result(), nil
	})
	prevUpdate, prevDesktop := updateHTTPClient.Transport, desktopUpdateClient.Transport
	updateHTTPClient.Transport = rt
	desktopUpdateClient.Transport = rt
	t.Cleanup(func() {
		updateHTTPClient.Transport = prevUpdate
		desktopUpdateClient.Transport = prevDesktop
	})
}

func TestDesktopUpdateAssetProxy(t *testing.T) {
	body := []byte("version: 0.0.19\npath: Gleam-Setup.exe\n")
	var assetHits int
	withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/gleam-ai/Gleam/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.0.19",
				"assets": []map[string]any{{
					"name":                 "latest.yml",
					"url":                  "https://api.github.com/repos/gleam-ai/Gleam/releases/assets/1",
					"browser_download_url": "https://github.com/gleam-ai/Gleam/releases/download/v0.0.19/latest.yml",
					"size":                 len(body),
				}},
			})
		case "/repos/gleam-ai/Gleam/releases/assets/1":
			assetHits++
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				t.Errorf("Accept = %q, want application/octet-stream", got)
			}
			if r.Header.Get("Range") == "bytes=0-6" {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-6/%d", len(body)))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(body[:7])
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	})

	s := &Server{}
	fetch := func(name, rangeHeader string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/update/desktop/"+name, nil)
		req.SetPathValue("name", name)
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		rec := httptest.NewRecorder()
		s.handleDesktopUpdateAsset(rec, req)
		return rec
	}

	full := fetch("latest.yml", "")
	if full.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", full.Code, full.Body.String())
	}
	if full.Body.String() != string(body) {
		t.Fatalf("body = %q", full.Body.String())
	}
	if got := full.Header().Get("Content-Length"); got != strconv.Itoa(len(body)) {
		t.Fatalf("Content-Length = %q", got)
	}

	part := fetch("latest.yml", "bytes=0-6")
	if part.Code != http.StatusPartialContent || part.Body.String() != string(body[:7]) {
		t.Fatalf("range status = %d, body = %q", part.Code, part.Body.String())
	}
	if got := part.Header().Get("Content-Range"); !strings.HasPrefix(got, "bytes 0-6/") {
		t.Fatalf("Content-Range = %q", got)
	}

	before := assetHits
	if rec := fetch("evil.exe", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown asset status = %d", rec.Code)
	}
	if assetHits != before {
		t.Fatal("unknown asset reached the upstream")
	}
}

func TestDesktopUpdateAssetPinsReleaseAcrossDownload(t *testing.T) {
	release := "v0.0.21"
	withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/gleam-ai/Gleam/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": release,
				"assets": []map[string]any{
					{
						"name":                 "latest.yml",
						"url":                  "https://api.github.com/repos/gleam-ai/Gleam/releases/assets/" + release + "-meta",
						"browser_download_url": "https://github.com/gleam-ai/Gleam/releases/download/" + release + "/latest.yml",
						"size":                 32,
					},
					{
						"name":                 "Gleam-Setup.exe",
						"url":                  "https://api.github.com/repos/gleam-ai/Gleam/releases/assets/" + release + "-installer",
						"browser_download_url": "https://github.com/gleam-ai/Gleam/releases/download/" + release + "/Gleam-Setup.exe",
						"size":                 32,
					},
				},
			})
		default:
			// Serve by the requested asset URL, not the current "latest".
			body := "installer-v0.0.21"
			if strings.Contains(r.URL.Path, "v0.0.22") {
				body = "installer-v0.0.22"
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			_, _ = w.Write([]byte(body))
		}
	})

	s := &Server{}
	fetch := func(name string) string {
		req := httptest.NewRequest(http.MethodGet, "/api/update/desktop/"+name, nil)
		req.SetPathValue("name", name)
		rec := httptest.NewRecorder()
		s.handleDesktopUpdateAsset(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body = %s", name, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	if got := fetch("latest.yml"); got != "installer-v0.0.21" {
		t.Fatalf("metadata = %q", got)
	}
	// A release published between the check and the download must not swap the
	// installer: the digest the updater read belongs to the pinned release.
	release = "v0.0.22"
	if got := fetch("Gleam-Setup.exe"); got != "installer-v0.0.21" {
		t.Fatalf("release changed mid-download: %q", got)
	}
	// The next check refreshes the pin, and downloads follow the new release.
	if got := fetch("latest.yml"); got != "installer-v0.0.22" {
		t.Fatalf("metadata after re-check = %q", got)
	}
	if got := fetch("Gleam-Setup.exe"); got != "installer-v0.0.22" {
		t.Fatalf("new pin not applied: %q", got)
	}
}

func TestDesktopUpdateAssetRejectsUntrustedSource(t *testing.T) {
	var assetHits int
	withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/gleam-ai/Gleam/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.0.19",
				"assets": []map[string]any{{
					"name":                 "Gleam-Setup.exe",
					"url":                  "https://evil.example/asset",
					"browser_download_url": "https://evil.example/asset",
					"size":                 268435456,
				}},
			})
		default:
			assetHits++
			http.NotFound(w, r)
		}
	})
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/update/desktop/Gleam-Setup.exe", nil)
	req.SetPathValue("name", "Gleam-Setup.exe")
	rec := httptest.NewRecorder()
	s.handleDesktopUpdateAsset(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if assetHits != 0 {
		t.Fatal("untrusted asset was fetched")
	}
}
