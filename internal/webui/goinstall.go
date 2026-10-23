package webui

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"gleam/internal/buildinfo"
)

const goToolchainVersion = "1.23.4"

var (
	goInstallMu sync.Mutex
	// A variable rather than a constant: tests lower the cap to verify that
	// oversized archives are rejected.
	goArchiveMaxBytes int64 = 512 << 20
)

func goToolsRoot(dataDir string) string {
	if strings.TrimSpace(dataDir) == "" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".gleam")
		}
		return ".gleam"
	}
	return dataDir
}

func goInstallDir(dataDir string) string {
	return filepath.Join(goToolsRoot(dataDir), "tools", "go")
}

func goArchiveName(goos, arch, ver string) string {
	if goos == "windows" {
		return fmt.Sprintf("go%s.%s-%s.zip", ver, goos, arch)
	}
	return fmt.Sprintf("go%s.%s-%s.tar.gz", ver, goos, arch)
}

func goMirrorURLs(goos, arch, ver string) []string {
	name := goArchiveName(goos, arch, ver)
	return []string{
		"https://mirrors.aliyun.com/golang/" + name,
		"https://golang.google.cn/dl/" + name,
		"https://go.dev/dl/" + name,
	}
}

var goChecksumEndpoints = []string{
	"https://golang.google.cn/dl/?mode=json&include=all",
	"https://go.dev/dl/?mode=json&include=all",
}

func goArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "amd64"
}

func (s *Server) handleGoInstall(w http.ResponseWriter, r *http.Request) {
	if st := s.managedGoStatus(); st.Found {
		writeJSON(w, 200, st)
		return
	}

	goos, arch := runtime.GOOS, goArch()
	name := goArchiveName(goos, arch, goToolchainVersion)
	dest := goInstallDir(s.Agent.DataDir())

	res := s.installGoToolchain(r.Context(), name, dest, goMirrorURLs(goos, arch, goToolchainVersion))
	if found, _ := res["found"].(bool); found {
		writeJSON(w, 200, res)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) installGoToolchain(ctx context.Context, name, dest string, urls []string) map[string]any {
	// Only one install at a time: two requests would each unpack and swap the
	// directory, and the later one would replace the files the first just
	// wrote, leaving half a toolchain.
	goInstallMu.Lock()
	defer goInstallMu.Unlock()

	out := map[string]any{
		"found":   false,
		"name":    name,
		"target":  dest,
		"sources": urls,
		"version": "go" + goToolchainVersion,
	}

	want, verified := fetchGoSHA256(ctx, name)
	out["verified"] = verified

	tmpDir := filepath.Join(goToolsRoot(s.Agent.DataDir()), "tools", ".download")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		out["message"] = "创建下载目录失败：" + err.Error()
		return out
	}
	// The temp file must keep the real extension: the extractor picks zip vs
	// tar.gz by suffix.
	pattern := "go-download-*.tar.gz"
	if strings.HasSuffix(name, ".zip") {
		pattern = "go-download-*.zip"
	}
	tmpFile, err := os.CreateTemp(tmpDir, pattern)
	if err != nil {
		out["message"] = "创建临时文件失败：" + err.Error()
		return out
	}
	archive := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(archive)
		out["message"] = "创建临时文件失败：" + err.Error()
		return out
	}
	defer os.Remove(archive)

	staging := dest + fmt.Sprintf(".staging-%d", time.Now().UnixNano())
	defer os.RemoveAll(staging)

	var lastErr string
	for _, u := range urls {
		n, err := s.fetchTo(ctx, u, archive)
		if err != nil {
			lastErr = fmt.Sprintf("%s：%v", hostOf(u), err)
			continue
		}
		if verified {
			got, err := sha256File(archive)
			if err != nil {
				lastErr = "算校验和失败：" + err.Error()
				continue
			}
			if !strings.EqualFold(got, want) {
				lastErr = fmt.Sprintf("%s 下到的文件 sha256 与官方清单不符，已丢弃", hostOf(u))
				_ = os.Remove(archive)
				continue
			}
		}
		out["bytes"] = n
		_ = os.RemoveAll(staging)
		if err := os.MkdirAll(staging, 0o755); err != nil {
			out["message"] = "创建暂存目录失败：" + err.Error()
			return out
		}
		if err := extractGoArchive(archive, staging); err != nil {
			out["message"] = "解压失败：" + err.Error()
			return out
		}
		stagedRoot := filepath.Join(staging, "go")
		if !DetectGo(goBinDir(stagedRoot)).Found {
			out["message"] = "展开完成但没找到 go 可执行文件，目录可能不完整"
			return out
		}
		if err := swapToolchainDir(stagedRoot, dest); err != nil {
			out["message"] = err.Error()
			return out
		}
		if st := DetectGo(goBinDir(dest)); st.Found {
			st.Source = "gleam-managed"
			out["found"] = true
			out["path"] = st.Path
			out["version"] = st.Version
			out["bin_dir"] = st.BinDir
			out["root"] = st.Root
			out["source"] = "gleam-managed"
			return out
		}
		out["message"] = "安装完成但没找到 go 可执行文件，目录可能不完整"
		return out
	}

	out["message"] = "下载失败，已试过所有镜像源：" + lastErr
	out["download"] = urls[0]
	return out
}

// swapToolchainDir replaces the whole directory and moves the old version back
// on failure. RemoveAll-then-extract would turn "an old Go is installed" into
// "nothing is installed" when extraction fails.
func swapToolchainDir(staged, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("创建目标目录失败: %w", err)
	}
	backup := dest + fmt.Sprintf(".old-%d", time.Now().UnixNano())
	hadOld := false
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, backup); err != nil {
			return fmt.Errorf("暂存旧工具链失败: %w", err)
		}
		hadOld = true
	}
	if err := os.Rename(staged, dest); err != nil {
		if hadOld {
			_ = os.Rename(backup, dest)
		}
		return fmt.Errorf("替换工具链目录失败: %w", err)
	}
	if hadOld {
		_ = os.RemoveAll(backup)
	}
	return nil
}

func goBinDir(root string) string { return filepath.Join(root, "bin") }

func (s *Server) managedGoStatus(customPath ...string) GoStatus {
	if len(customPath) == 0 && s.Agent != nil {
		if st := DetectGo(goBinDir(goInstallDir(s.Agent.DataDir()))); st.Found {
			st.Source = "gleam-managed"
			return st
		}
	}
	return DetectGo(customPath...)
}

func (s *Server) fetchTo(ctx context.Context, raw, dst string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "gleam/"+buildinfo.Version)
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > goArchiveMaxBytes {
		return 0, fmt.Errorf("归档 %d 字节，超过上限 %d 字节", resp.ContentLength, goArchiveMaxBytes)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	// Read one extra byte: a LimitReader capped at the limit cannot tell
	// "exactly 512 MiB" from "there is more", and would accept a truncated
	// download as success.
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, goArchiveMaxBytes+1))
	closeErr := f.Close()
	if copyErr != nil {
		return n, copyErr
	}
	if closeErr != nil {
		return n, closeErr
	}
	if n > goArchiveMaxBytes {
		return n, fmt.Errorf("归档超过上限 %d 字节", goArchiveMaxBytes)
	}
	if s.Agent != nil && s.Agent.Gate != nil {
		s.Agent.Gate.RecordEgress("go.toolchain", hostOf(raw), int(n))
	}
	return n, nil
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host
}

func fetchGoSHA256(ctx context.Context, filename string) (string, bool) {
	client := &http.Client{Timeout: 15 * time.Second}
	for _, src := range goChecksumEndpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "gleam/"+buildinfo.Version)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		var rels []struct {
			Files []struct {
				Filename string `json:"filename"`
				SHA256   string `json:"sha256"`
			} `json:"files"`
		}
		decErr := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rels)
		resp.Body.Close()
		if decErr != nil {
			continue
		}
		for _, rel := range rels {
			for _, f := range rel.Files {
				if f.Filename == filename && f.SHA256 != "" {
					return f.SHA256, true
				}
			}
		}
	}
	return "", false
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func extractGoArchive(archive, dstRoot string) error {
	if strings.HasSuffix(archive, ".zip") {
		return extractZip(archive, dstRoot)
	}
	return extractTarGz(archive, dstRoot)
}

func cleanEntry(name, dstRoot string) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimPrefix(name, "go/")
	if name == "" || name == "." || name == "go" {
		return "", false
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." || filepath.IsAbs(clean) {
		return "", false
	}
	return filepath.Join(dstRoot, "go", clean), true
}

func insideRoot(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if !filepath.IsAbs(target) {
		return false
	}
	return target != root && strings.HasPrefix(target, root+string(filepath.Separator))
}

func extractZip(archive, dstRoot string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		target, ok := cleanEntry(f.Name, dstRoot)
		if !ok || !insideRoot(dstRoot, target) {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		mode := f.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		if err := writeFileFrom(target, rc, mode); err != nil {
			rc.Close()
			return err
		}
		rc.Close()
	}
	return nil
}

func extractTarGz(archive, dstRoot string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target, ok := cleanEntry(hdr.Name, dstRoot)
		if !ok || !insideRoot(dstRoot, target) {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			if err := writeFileFrom(target, io.LimitReader(tr, hdr.Size), mode); err != nil {
				return err
			}
		default:
		}
	}
	return nil
}

func writeFileFrom(target string, src io.Reader, mode os.FileMode) error {
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		os.Remove(target)
		return err
	}
	return out.Close()
}
