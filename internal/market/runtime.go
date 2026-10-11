package market

import (
	"os/exec"
	"strings"
)

type RuntimeStatus struct {
	Name  string `json:"name"`
	Found bool   `json:"found"`
	Path  string `json:"path,omitempty"`
	Why   string `json:"why"`
}

var runtimeSpec = []struct {
	name string
	why  string
}{
	{"npx", "npx 随 Node.js 一起装：https://nodejs.org（装完重开 Gleam）"},
	{"uvx", "uvx 随 uv 一起装：https://docs.astral.sh/uv/（或 pip install uv）"},
	{"docker", "docker 需要 Docker Desktop：https://www.docker.com/products/docker-desktop/"},
	{"cargo", "cargo 随 Rust 一起装：https://rustup.rs"},
	{"go", "go 可以在 设置 → 电脑 里一键安装"},
}

func Runtimes() []RuntimeStatus {
	out := make([]RuntimeStatus, 0, len(runtimeSpec))
	for _, spec := range runtimeSpec {
		st := RuntimeStatus{Name: spec.name, Why: spec.why}
		if p, err := exec.LookPath(spec.name); err == nil {
			st.Found, st.Path = true, p
		}
		out = append(out, st)
	}
	return out
}

func RuntimeFor(command string) string {
	base := strings.ToLower(strings.TrimSpace(command))
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	for _, spec := range runtimeSpec {
		if base == spec.name {
			return spec.name
		}
	}
	return ""
}

func RuntimeMissing(command string) (RuntimeStatus, bool) {
	name := RuntimeFor(command)
	if name == "" {
		return RuntimeStatus{}, false
	}
	for _, st := range Runtimes() {
		if st.Name == name {
			if st.Found {
				return RuntimeStatus{}, false
			}
			return st, true
		}
	}
	return RuntimeStatus{}, false
}

func RankRemote(in []RemotePreset, q string) []RemotePreset {
	q = strings.ToLower(strings.TrimSpace(q))
	score := func(p RemotePreset) int {
		s := 0
		if p.Installable {
			s += 100
		}
		name := strings.ToLower(p.ID + " " + p.Name)
		if q != "" {
			switch {
			case strings.EqualFold(strings.ToLower(p.Name), q), strings.EqualFold(strings.ToLower(p.ID), q):
				s += 30
			case strings.HasPrefix(name, q):
				s += 20
			case strings.Contains(name, q):
				s += 10
			}
		}
		return s
	}
	out := append([]RemotePreset(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && score(out[j]) > score(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
