package webui

import (
	"bufio"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gleam/internal/agent"
	"gleam/pkg/types"
)

func (s *Server) handleSSHHosts(w http.ResponseWriter, _ *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"path": "", "exists": false, "hosts": []string{}})
		return
	}
	path := filepath.Join(home, ".ssh", "config")
	f, err := os.Open(path)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"path": path, "exists": false, "hosts": []string{}})
		return
	}
	defer f.Close()
	hosts := parseSSHHostNames(bufio.NewScanner(f))
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "exists": true, "hosts": hosts})
}

func parseSSHHostNames(sc *bufio.Scanner) []string {
	seen := map[string]bool{}
	out := []string{}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, rest := line, ""
		if i := strings.IndexAny(line, " \t="); i >= 0 {
			key, rest = line[:i], strings.TrimLeft(line[i:], " \t=")
		}
		if !strings.EqualFold(key, "Host") {
			continue
		}
		if j := strings.Index(rest, "#"); j >= 0 {
			rest = rest[:j]
		}
		for _, name := range strings.Fields(rest) {
			name = strings.Trim(name, `"`)
			if name == "" || strings.ContainsAny(name, "*?!") || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func (s *Server) handleNetworkInfo(w http.ResponseWriter, _ *http.Request) {
	env := map[string]string{}
	for _, k := range []string{"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "NO_PROXY"} {
		v := os.Getenv(k)
		if v == "" {
			v = os.Getenv(strings.ToLower(k))
		}
		if v == "" {
			continue
		}
		if k != "NO_PROXY" {
			v = redactProxy(v)
		}
		env[k] = v
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	host := ""
	if u, err := url.Parse(s.Agent.Cfg.LLM.BaseURL); err == nil {
		host = u.Host
	}
	netw := s.Agent.Cfg.Network
	mode := netw.ProxyModeOrDefault()
	manual := ""
	if mode == "manual" {
		manual = redactProxy(netw.ProxyURL)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"proxy_env": env, "proxy_keys": keys,
		"proxy_mode": mode, "proxy_url": manual,
		"llm_host": host, "llm_provider": s.Agent.Cfg.LLM.Provider,
	})
}

func redactProxy(v string) string {
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		if i := strings.LastIndex(v, "@"); i >= 0 {
			return "***@" + v[i+1:]
		}
		return v
	}
	if u.User != nil {
		u.User = url.User("***")
	}
	return u.String()
}

func (s *Server) handleGoalDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	path := agent.TaskArchivePath(s.Agent.Cfg.DataDir, id)
	if path == "" {
		writeErr(w, http.StatusBadRequest, "非法任务 id")
		return
	}
	s.mu.Lock()
	t, inMem := s.tasks[id]
	if inMem && t.Status == types.GoalRunning {
		s.mu.Unlock()
		writeErr(w, http.StatusConflict, "任务还在运行，先取消再删除")
		return
	}
	if inMem {
		delete(s.tasks, id)
	}
	s.mu.Unlock()
	err := os.Remove(path)
	onDisk := err == nil
	if err != nil && !os.IsNotExist(err) {
		writeErr(w, http.StatusInternalServerError, "删除归档失败：%v", err)
		return
	}
	if !inMem && !onDisk {
		writeErr(w, http.StatusNotFound, "任务 %q 不存在", id)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task_id": id, "deleted": true})
}
