package webui

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

type GoStatus struct {
	Found   bool   `json:"found"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	BinDir  string `json:"bin_dir,omitempty"`
	Root    string `json:"root,omitempty"`
	Source  string `json:"source,omitempty"`
}

func DetectGo(customPath ...string) GoStatus {
	for _, p := range customPath {
		if p == "" {
			continue
		}
		if st := checkGoAt(p); st.Found {
			st.Source = "user-specified"
			return st
		}
		if st := checkGoAt(filepath.Join(p, "bin")); st.Found {
			st.Source = "user-specified"
			return st
		}
		return GoStatus{Found: false, Source: "user-specified"}
	}

	if path, err := exec.LookPath("go"); err == nil {
		v, _ := exec.Command(path, "version").Output()
		st := GoStatus{
			Found:   true,
			Path:    path,
			Version: strings.TrimSpace(string(v)),
			BinDir:  filepath.Dir(path),
			Root:    filepath.Dir(filepath.Dir(path)),
			Source:  "PATH",
		}
		return st
	}

	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = []string{
			"C:\\Go\\bin",
			"C:\\Program Files\\Go\\bin",
			"C:\\Program Files (x86)\\Go\\bin",
		}
	} else {
		candidates = []string{
			"/usr/local/go/bin",
			"/usr/lib/go/bin",
			"/opt/go/bin",
			"/home/linuxbrew/.linuxbrew/bin",
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if runtime.GOOS == "windows" {
			candidates = append(candidates, filepath.Join(home, "go", "bin"))
		} else {
			candidates = append(candidates, filepath.Join(home, ".go", "bin"))
			candidates = append(candidates, filepath.Join(home, "go", "bin"))
		}
	}
	for _, dir := range candidates {
		if st := checkGoAt(dir); st.Found {
			st.Source = "common-path"
			return st
		}
	}

	if goroot := os.Getenv("GOROOT"); goroot != "" {
		if st := checkGoAt(filepath.Join(goroot, "bin")); st.Found {
			st.Source = "GOROOT"
			return st
		}
	}

	return GoStatus{Found: false}
}

// goDirRe restricts candidate directories to a conservative charset: the path
// arrives over HTTP, so anything outside ordinary filesystem characters is
// rejected before it touches the filesystem.
var goDirRe = regexp.MustCompile(`^[A-Za-z0-9 _().+\-/\\:~]+$`)

func checkGoAt(dir string) GoStatus {
	if dir == "" || !filepath.IsAbs(dir) || !goDirRe.MatchString(dir) {
		return GoStatus{}
	}
	binaryName := "go"
	if runtime.GOOS == "windows" {
		binaryName = "go.exe"
	}
	goPath := filepath.Join(dir, binaryName)
	if st, err := os.Stat(goPath); err != nil || st.IsDir() {
		return GoStatus{}
	}
	root := filepath.Dir(dir)
	return GoStatus{
		Found:   true,
		Path:    goPath,
		Version: goVersionFromFile(root),
		BinDir:  dir,
		Root:    root,
	}
}

// goVersionFromFile reads GOROOT/VERSION instead of executing the candidate:
// the directory comes from an HTTP request, and running whatever binary sits
// there would turn "detect Go" into "execute a caller-chosen program".
func goVersionFromFile(root string) string {
	b, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
}
