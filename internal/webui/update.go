package webui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func assetNamesFor() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"Gleam-Setup.exe", "Gleam.Setup.exe", "Gleam Setup.exe"}
	case "linux":
		return []string{"gleam-linux-amd64", "Gleam-Linux-x86_64"}
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return []string{"gleam-darwin-arm64", "Gleam-macOS-AppleSilicon-arm64"}
		}
		return []string{"gleam-darwin-amd64", "Gleam-macOS-Intel-x86_64"}
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
	if preA == preB {
		return 0
	}
	if preA == "" {
		return 1
	}
	if preB == "" {
		return -1
	}
	return strings.Compare(preA, preB)
}

func splitVersion(v string) ([]int, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	core, pre := v, ""
	if i := strings.IndexAny(v, "-+"); i >= 0 {
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
	return nums, pre
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

func trustedAssetURL(raw string) bool {
	for _, host := range []string{"https://github.com/", "https://objects.githubusercontent.com/", "https://release-assets.githubusercontent.com/"} {
		if strings.HasPrefix(raw, host) {
			return true
		}
	}
	return false
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	cur := buildinfo.Version
	res := map[string]any{"current": cur, "repo": updateRepo}

	client := &http.Client{Timeout: 12 * time.Second}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, updateLatestAPI, nil)
	if err != nil {
		res["error"] = "request"
		writeJSON(w, 200, res)
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gleam/"+cur)

	resp, err := client.Do(req)
	if err != nil {
		res["error"] = "network"
		writeJSON(w, 200, res)
		return
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		res["error"] = "no_release"
		writeJSON(w, 200, res)
		return
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		res["error"] = "rate_limit"
		writeJSON(w, 200, res)
		return
	case resp.StatusCode != http.StatusOK:
		res["error"] = "http"
		res["status"] = resp.StatusCode
		writeJSON(w, 200, res)
		return
	}

	var rel struct {
		TagName     string         `json:"tag_name"`
		HTMLURL     string         `json:"html_url"`
		Body        string         `json:"body"`
		PublishedAt string         `json:"published_at"`
		Draft       bool           `json:"draft"`
		Assets      []releaseAsset `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		res["error"] = "bad_json"
		writeJSON(w, 200, res)
		return
	}

	latest := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	res["latest"] = latest
	res["notes_url"] = rel.HTMLURL
	res["published_at"] = rel.PublishedAt
	res["has_update"] = !rel.Draft && latest != "" && compareVersions(latest, cur) > 0
	res["notes"] = clipNotes(rel.Body)

	for _, want := range assetNamesFor() {
		for _, a := range rel.Assets {
			if strings.HasPrefix(a.Name, want) && trustedAssetURL(a.URL) {
				res["asset"] = releaseAsset{Name: a.Name, URL: a.URL, Size: a.Size}
				break
			}
		}
		if _, ok := res["asset"]; ok {
			break
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
	if asset.URL == "" {
		writeErr(w, 400, "这个平台在最新版里没有对应的安装包")
		return
	}

	tmp := filepath.Join(filepath.Dir(exe), "."+filepath.Base(exe)+".new")
	if err := s.downloadTo(r, asset, tmp); err != nil {
		_ = os.Remove(tmp)
		writeErr(w, 400, "下载失败：%v", err)
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
		_ = os.Rename(backup, exe)
		_ = os.Remove(tmp)
		writeErr(w, 400, "替换失败（%v），已回滚到原版本", err)
		return
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(exe, 0o755)
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "from": cur, "to": asset.Name,
		"backup": backup, "restart": true,
	})
}

func (s *Server) pickUpdateAsset(r *http.Request) (releaseAsset, error) {
	client := &http.Client{Timeout: 12 * time.Second}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, updateLatestAPI, nil)
	if err != nil {
		return releaseAsset{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gleam/"+buildinfo.Version)
	resp, err := client.Do(req)
	if err != nil {
		return releaseAsset{}, fmt.Errorf("连不上更新源：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return releaseAsset{}, fmt.Errorf("更新源返回 %d", resp.StatusCode)
	}
	var rel struct {
		TagName string         `json:"tag_name"`
		Assets  []releaseAsset `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return releaseAsset{}, fmt.Errorf("更新源响应读不动：%w", err)
	}
	if compareVersions(strings.TrimPrefix(rel.TagName, "v"), buildinfo.Version) <= 0 {
		return releaseAsset{}, fmt.Errorf("已经是最新版本了")
	}
	for _, want := range assetNamesFor() {
		for _, a := range rel.Assets {
			if strings.HasPrefix(a.Name, want) && trustedAssetURL(a.URL) {
				return a, nil
			}
		}
	}
	return releaseAsset{}, nil
}

func (s *Server) downloadTo(r *http.Request, a releaseAsset, dst string) error {
	client := &http.Client{Timeout: 10 * time.Minute}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "gleam/"+buildinfo.Version)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载地址返回 %d", resp.StatusCode)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, 512<<20))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if a.Size > 0 && n != a.Size {
		return fmt.Errorf("下到的字节数 %d 与发布信息里的 %d 不一致", n, a.Size)
	}
	if s.Agent != nil && s.Agent.Gate != nil {
		s.Agent.Gate.RecordEgress("update", hostOf(a.URL), int(n))
	}
	return nil
}
