package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gleam/internal/buildinfo"
)

const updateRepo = "gleam-ai/Gleam"

const updateLatestAPI = "https://api.github.com/repos/" + updateRepo + "/releases/latest"

const (
	updateMinBytes = 1 << 20
	// The engine is ~16 MB; the NSIS installer is ~265 MB. Keeping the ceiling
	// far below the installer makes a wrong asset fail before anything is
	// renamed or replaced.
	updateMaxBytes = 64 << 20
)

// inMacAppBundle reports whether exe lives inside a .app bundle. Replacing a
// file there breaks the bundle seal (ad-hoc signed on arm64), so the engine
// must not update itself in place; the desktop shell owns that channel.
func inMacAppBundle(exe, goos string) bool {
	if goos != "darwin" || strings.TrimSpace(exe) == "" {
		return false
	}
	p := strings.ToLower(filepath.ToSlash(filepath.Clean(exe)))
	return strings.Contains(p, ".app/contents/")
}

// inDesktopResources reports whether exe is the engine shipped inside an
// Electron app bundle (resources/bin/gleam[.exe]). The shell owns installation
// there; an engine-only rewrite leaves shell and sidecar on different versions
// and has already corrupted installs.
func inDesktopResources(exe string) bool {
	p := strings.ToLower(filepath.ToSlash(filepath.Clean(exe)))
	return strings.HasSuffix(p, "/resources/bin/gleam") ||
		strings.HasSuffix(p, "/resources/bin/gleam.exe")
}

func (s *Server) updateManaged() bool {
	if s.DisableSelfUpdate {
		return true
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return inMacAppBundle(exe, runtime.GOOS) || inDesktopResources(exe)
}

// assetNamesFor mirrors the no-date names produced by scripts/package.sh.
// The Electron NSIS installer (Gleam-Setup.exe) is deliberately absent: writing
// it over the engine binary corrupts the install (the desktop shell owns it).
func assetNamesFor(goos, goarch string) []string {
	switch goos {
	case "windows":
		if goarch == "amd64" {
			return []string{"gleam-windows-amd64.exe"}
		}
	case "linux":
		if goarch == "amd64" || goarch == "arm64" {
			return []string{"gleam-linux-" + goarch}
		}
	case "darwin":
		if goarch == "amd64" || goarch == "arm64" {
			return []string{"gleam-darwin-" + goarch}
		}
	}
	return nil
}

func compareVersions(a, b string) int {
	pa, preA := splitVersion(a)
	pb, preB := splitVersion(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		x, y := 0, 0
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return comparePrerelease(preA, preB)
}

// splitVersion returns numeric core parts and dot-separated prerelease
// identifiers. Build metadata after "+" is ignored, as SemVer requires.
func splitVersion(v string) ([]int, []string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	core, pre := v, ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		core, pre = v[:i], v[i+1:]
	}
	parts := strings.Split(core, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			n = 0
		}
		nums = append(nums, n)
	}
	var ids []string
	if pre != "" {
		ids = strings.Split(pre, ".")
	}
	return nums, ids
}

func comparePrerelease(a, b []string) int {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	if len(a) == 0 {
		return 1
	}
	if len(b) == 0 {
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := compareIdentifier(a[i], b[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

func compareIdentifier(a, b string) int {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		if an != bn {
			if an < bn {
				return -1
			}
			return 1
		}
		return 0
	case aErr == nil:
		return -1
	case bErr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

type releaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	APIURL string `json:"url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest,omitempty"`
}

type releaseInfo struct {
	TagName     string         `json:"tag_name"`
	HTMLURL     string         `json:"html_url"`
	Body        string         `json:"body"`
	PublishedAt string         `json:"published_at"`
	Draft       bool           `json:"draft"`
	Assets      []releaseAsset `json:"assets"`
}

var updateHTTPClient = &http.Client{Timeout: 12 * time.Second}

func trustedAssetURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com":
		return true
	}
	return false
}

// trustedAssetAPIURL reports whether raw is the release-asset API endpoint for
// this repo. That endpoint redirects to signed storage, which stays reachable
// on networks where github.com itself keeps getting reset.
func trustedAssetAPIURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	if !strings.EqualFold(u.Hostname(), "api.github.com") {
		return false
	}
	return strings.HasPrefix(u.Path, "/repos/"+updateRepo+"/releases/assets/")
}

func (a releaseAsset) downloadURL() string {
	if trustedAssetAPIURL(a.APIURL) {
		return a.APIURL
	}
	if trustedAssetURL(a.URL) {
		return a.URL
	}
	return ""
}

func fetchLatestRelease(ctx context.Context) (releaseInfo, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateLatestAPI, nil)
	if err != nil {
		return releaseInfo{}, 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gleam/"+buildinfo.Version)
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return releaseInfo{}, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return releaseInfo{}, resp.StatusCode, nil
	}
	var rel releaseInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return releaseInfo{}, resp.StatusCode, err
	}
	return rel, resp.StatusCode, nil
}

// selectUpdateAsset only accepts the exact engine asset for this platform.
// Prefix matching used to pick "Gleam-Setup.exe.blockmap" before the real
// asset, and on Windows it picked the NSIS installer itself.
func selectUpdateAsset(assets []releaseAsset, goos, goarch string) (releaseAsset, bool) {
	for _, want := range assetNamesFor(goos, goarch) {
		for _, a := range assets {
			if !strings.EqualFold(a.Name, want) || !trustedAssetURL(a.URL) {
				continue
			}
			if a.Size > 0 && (a.Size < updateMinBytes || a.Size > updateMaxBytes) {
				continue
			}
			return a, true
		}
	}
	return releaseAsset{}, false
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	cur := buildinfo.Version
	res := map[string]any{"current": cur, "repo": updateRepo}

	rel, status, err := fetchLatestRelease(r.Context())
	if err != nil {
		if status == 0 {
			res["error"] = "network"
		} else {
			res["error"] = "bad_json"
		}
		writeJSON(w, 200, res)
		return
	}
	switch {
	case status == http.StatusNotFound:
		res["error"] = "no_release"
		writeJSON(w, 200, res)
		return
	case status == http.StatusForbidden || status == http.StatusTooManyRequests:
		res["error"] = "rate_limit"
		writeJSON(w, 200, res)
		return
	case status != http.StatusOK:
		res["error"] = "http"
		res["status"] = status
		writeJSON(w, 200, res)
		return
	}

	latest := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	res["latest"] = latest
	res["notes_url"] = rel.HTMLURL
	res["published_at"] = rel.PublishedAt
	res["has_update"] = !rel.Draft && latest != "" && compareVersions(latest, cur) > 0
	res["notes"] = clipNotes(rel.Body)
	managed := s.updateManaged()
	if managed {
		res["managed"] = true
	}
	if !managed {
		if asset, ok := selectUpdateAsset(rel.Assets, runtime.GOOS, runtime.GOARCH); ok {
			res["asset"] = asset
		}
	}
	writeJSON(w, 200, res)
}

func clipNotes(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > 800 {
		return string(r[:800]) + "…"
	}
	return s
}

func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if s.updateManaged() {
		writeErr(w, 409, "桌面版由安装包统一更新；请使用桌面端的更新入口")
		return
	}
	if !s.updateMu.TryLock() {
		writeErr(w, 409, "已有一次更新正在进行，等它结束再试")
		return
	}
	defer s.updateMu.Unlock()

	cur := buildinfo.Version
	exe, err := os.Executable()
	if err != nil {
		writeErr(w, 400, "拿不到当前程序路径：%v", err)
		return
	}
	exe, _ = filepath.EvalSymlinks(exe)

	asset, err := s.pickUpdateAsset(r)
	if err != nil {
		writeErr(w, 400, "%v", err)
		return
	}
	if asset.downloadURL() == "" {
		writeErr(w, 400, "这个平台在最新版里没有对应的引擎程序")
		return
	}

	f, err := os.CreateTemp(filepath.Dir(exe), "."+filepath.Base(exe)+".new-*")
	if err != nil {
		writeErr(w, 400, "创建临时文件失败：%v", err)
		return
	}
	tmp := f.Name()
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}
	if _, err := s.downloadTo(r.Context(), asset, f); err != nil {
		cleanup()
		writeErr(w, 400, "下载失败：%v", err)
		return
	}
	if err := f.Sync(); err != nil {
		cleanup()
		writeErr(w, 400, "写入磁盘失败：%v", err)
		return
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		writeErr(w, 400, "写入磁盘失败：%v", err)
		return
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmp, 0o755); err != nil {
			_ = os.Remove(tmp)
			writeErr(w, 400, "设置可执行权限失败：%v", err)
			return
		}
	}
	if err := verifyUpdateBinary(tmp, asset, runtime.GOOS); err != nil {
		_ = os.Remove(tmp)
		writeErr(w, 400, "下载到的文件不像可执行程序，这次更新已中止：%v", err)
		return
	}

	backup := exe + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(exe, backup); err != nil {
		_ = os.Remove(tmp)
		writeErr(w, 400, "备份当前程序失败（%v）：为避免把程序弄坏，这次更新已中止", err)
		return
	}
	if err := os.Rename(tmp, exe); err != nil {
		rollbackErr := os.Rename(backup, exe)
		_ = os.Remove(tmp)
		if rollbackErr != nil {
			writeErr(w, 500, "替换失败（%v），回滚也失败（%v）。原程序在 %s，请手动改回原名", err, rollbackErr, backup)
			return
		}
		writeErr(w, 400, "替换失败（%v），已回滚到原版本", err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "from": cur, "to": asset.Name,
		"backup": backup, "restart": true,
	})
}

func (s *Server) pickUpdateAsset(r *http.Request) (releaseAsset, error) {
	rel, status, err := fetchLatestRelease(r.Context())
	if err != nil {
		return releaseAsset{}, fmt.Errorf("连不上更新源：%w", err)
	}
	if status != http.StatusOK {
		return releaseAsset{}, fmt.Errorf("更新源返回 %d", status)
	}
	if compareVersions(strings.TrimPrefix(rel.TagName, "v"), buildinfo.Version) <= 0 {
		return releaseAsset{}, fmt.Errorf("已经是最新版本了")
	}
	asset, ok := selectUpdateAsset(rel.Assets, runtime.GOOS, runtime.GOARCH)
	if !ok {
		return releaseAsset{}, nil
	}
	return asset, nil
}

func (s *Server) downloadTo(ctx context.Context, a releaseAsset, w io.Writer) (int64, error) {
	src := a.downloadURL()
	if src == "" {
		return 0, fmt.Errorf("没有可信的下载地址")
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "gleam/"+buildinfo.Version)
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("下载地址返回 %d", resp.StatusCode)
	}
	limit := int64(updateMaxBytes)
	if a.Size > 0 {
		limit = a.Size
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return n, err
	}
	if n > limit {
		return n, fmt.Errorf("下载内容超过预期大小（%d 字节）", limit)
	}
	if a.Size > 0 && n != a.Size {
		return n, fmt.Errorf("下到的字节数 %d 与发布信息里的 %d 不一致", n, a.Size)
	}
	if n < updateMinBytes {
		return n, fmt.Errorf("文件只有 %d 字节，不像引擎程序", n)
	}
	if s.Agent != nil && s.Agent.Gate != nil {
		s.Agent.Gate.RecordEgress("update", hostOf(src), int(n))
	}
	return n, nil
}

// desktopUpdateClient streams installers (265 MB) and blockmaps, so it has no
// overall timeout. The per-phase limits still drop a stalled upstream instead
// of pinning the connection forever.
var desktopUpdateClient = func() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: t}
}()

// A release pin only exists to keep one check+download cycle on one release.
// latest.yml refreshes it on every check, so the TTL only bounds a stale
// direct asset request.
const desktopReleasePinTTL = 2 * time.Hour

func isDesktopUpdateMetadata(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "latest") &&
		(strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml"))
}

func (s *Server) pinDesktopRelease(rel releaseInfo) {
	s.desktopPinMu.Lock()
	s.desktopPin = &rel
	s.desktopPinAt = time.Now()
	s.desktopPinMu.Unlock()
}

func (s *Server) pinnedDesktopAsset(name string) (releaseAsset, bool) {
	s.desktopPinMu.Lock()
	defer s.desktopPinMu.Unlock()
	if s.desktopPin == nil || time.Since(s.desktopPinAt) > desktopReleasePinTTL {
		return releaseAsset{}, false
	}
	for _, a := range s.desktopPin.Assets {
		if strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return releaseAsset{}, false
}

// handleDesktopUpdateFeed serves the latest.yml feed to Electron updater.
// It queries the latest GitHub release and returns a YAML document with
// provider, files, sha512, url, and notes fields that electron-updater expects.
func (s *Server) handleDesktopUpdateFeed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-yaml")
	rel, status, err := fetchLatestRelease(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "连不上更新源：%v", err)
		return
	}
	if status != http.StatusOK {
		writeErr(w, http.StatusBadGateway, "更新源返回 %d", status)
		return
	}
	if rel.Draft {
		writeErr(w, http.StatusNotFound, "当前版本尚未发布")
		return
	}

	// Pick the best asset for this platform. electron-updater uses this as the
	// sole download target in the generic provider model.
	var best releaseAsset
	for _, a := range rel.Assets {
		if isDesktopUpdateMetadata(a.Name) {
			continue // Skip latest.yml files
		}
		ggo, garch, _ := strings.Cut(a.Name, "-")
		if ggo == runtime.GOOS && garch == runtime.GOARCH {
			best = a
			break
		}
	}
	// Fallback: if no exact match, try same-OS any-arch (e.g., darwin/amd64 -> darwin/arm64).
	if best.Name == "" {
		for _, a := range rel.Assets {
			if isDesktopUpdateMetadata(a.Name) {
				continue
			}
			ggo, _, _ := strings.Cut(a.Name, "-")
			if ggo == runtime.GOOS {
				best = a
				break
			}
		}
	}
	if best.Name == "" {
		writeErr(w, http.StatusNotFound, "当前平台没有可用资产")
		return
	}

	sha512 := best.Digest
	if !strings.HasPrefix(strings.ToLower(sha512), "sha512:") {
		// If only SHA-256 is present, we still report it under sha512 key because
		// electron-updater/generic provider only looks at sha512. We keep the value
		// as-is so the checksum verification can still work downstream.
		if strings.HasPrefix(strings.ToLower(sha512), "sha256:") {
			sha512 = strings.TrimPrefix(sha512, "sha256:")
			sha512 = "sha512:" + sha512
		}
	}

	yamlLines := []string{
		"provider: generic",
		fmt.Sprintf("version: %s", buildinfo.Version),
		fmt.Sprintf("sha512: %s", strings.TrimSpace(sha512)),
		fmt.Sprintf("url: %s/%s/releases/download/v%s/%s", rel.HTMLURL[:len(rel.HTMLURL)-len("/releases/tag/"+rel.TagName)], updateRepo, rel.TagName[1:], best.Name),
		fmt.Sprintf("notes: |\n%s", escapeYAMLNotes(rel.Body)),
	}
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintln(w, strings.Join(yamlLines, "\n"))
}

// escapeYAMLNotes indents multi-line notes by two spaces and escapes special chars.
func escapeYAMLNotes(body string) string {
	// Escape backticks and dollar signs that could be interpreted in triple-quoted YAML.
	body = strings.ReplaceAll(body, "`", "\\`")
	body = strings.ReplaceAll(body, "$", "\\$")
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		lines[i] = "  " + line
	}
	return strings.Join(lines, "\n")
}

// handleDesktopUpdateAsset serves a release asset to the Electron updater.
// electron-updater's GitHub provider talks to github.com, which some networks
// keep resetting; the shell now points its generic feed here and this handler
// fetches through api.github.com, which stays reachable. Only assets that
// belong to the latest release can be streamed, and Range requests pass
// through so the updater can resume.
func (s *Server) handleDesktopUpdateAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || name != filepath.Base(name) {
		writeErr(w, http.StatusNotFound, "没有这个更新文件")
		return
	}
	// The updater reads latest.yml first and then downloads by asset name. Pin
	// that release for the rest of the download: resolving "latest" again per
	// asset lets a release published mid-download swap the installer under the
	// SHA512 the updater already accepted, wasting the whole transfer.
	var asset releaseAsset
	meta := isDesktopUpdateMetadata(name)
	if !meta {
		asset, _ = s.pinnedDesktopAsset(name)
	}
	if asset.Name == "" {
		rel, status, err := fetchLatestRelease(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, "连不上更新源：%v", err)
			return
		}
		if status != http.StatusOK {
			writeErr(w, http.StatusBadGateway, "更新源返回 %d", status)
			return
		}
		for _, a := range rel.Assets {
			if a.Name == name {
				asset = a
				break
			}
		}
		if asset.Name == "" {
			writeErr(w, http.StatusNotFound, "最新版里没有 %s", name)
			return
		}
		s.pinDesktopRelease(rel)
	}
	src := asset.downloadURL()
	if src == "" {
		writeErr(w, http.StatusBadGateway, "更新文件的下载地址不可信")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, src, nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "下载请求建不起来：%v", err)
		return
	}
	req.Header.Set("User-Agent", "gleam/"+buildinfo.Version)
	req.Header.Set("Accept", "application/octet-stream")
	if rg := r.Header.Get("Range"); rg != "" {
		req.Header.Set("Range", rg)
	}
	resp, err := desktopUpdateClient.Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "下载失败：%v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		writeErr(w, http.StatusBadGateway, "下载地址返回 %d", resp.StatusCode)
		return
	}
	h := w.Header()
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := resp.Header.Get(k); v != "" {
			h.Set(k, v)
		}
	}
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	n, err := io.Copy(w, resp.Body)
	if err != nil || n == 0 {
		return
	}
	if s.Agent != nil && s.Agent.Gate != nil {
		s.Agent.Gate.RecordEgress("update", hostOf(src), int(n))
	}
}

func verifyUpdateBinary(path string, a releaseAsset, goos string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() < updateMinBytes || info.Size() > updateMaxBytes {
		return fmt.Errorf("大小 %d 字节超出合理范围", info.Size())
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 8)
	if _, err := io.ReadFull(f, head); err != nil {
		return fmt.Errorf("读不到文件头：%w", err)
	}
	if !executableMagic(head, goos) {
		return fmt.Errorf("文件头 %x 不是 %s 可执行格式", head[:4], goos)
	}
	if !strings.HasPrefix(strings.ToLower(a.Digest), "sha256:") {
		return nil
	}
	want := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(a.Digest), "sha256:"))
	sum, err := sha256File(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(sum, want) {
		return fmt.Errorf("SHA-256 校验不一致")
	}
	return nil
}

func executableMagic(head []byte, goos string) bool {
	if len(head) < 4 {
		return false
	}
	switch goos {
	case "windows":
		return head[0] == 'M' && head[1] == 'Z'
	case "linux":
		return head[0] == 0x7f && head[1] == 'E' && head[2] == 'L' && head[3] == 'F'
	case "darwin":
		m := [4]byte{head[0], head[1], head[2], head[3]}
		switch m {
		case [4]byte{0xfe, 0xed, 0xfa, 0xce}, [4]byte{0xfe, 0xed, 0xfa, 0xcf},
			[4]byte{0xce, 0xfa, 0xed, 0xfe}, [4]byte{0xcf, 0xfa, 0xed, 0xfe},
			[4]byte{0xca, 0xfe, 0xba, 0xbe}, [4]byte{0xbe, 0xba, 0xfe, 0xca}:
			return true
		}
	}
	return false
}
