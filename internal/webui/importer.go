package webui

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gleam/internal/agent"
)

type memCandidate struct {
	ID      string `json:"id"`
	App     string `json:"app"`
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Bytes   int    `json:"bytes"`
	Entries int    `json:"entries"`
	Preview string `json:"preview"`
}

type mcpCandidate struct {
	ID        string   `json:"id"`
	App       string   `json:"app"`
	Name      string   `json:"name"`
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	EnvKeys   []string `json:"env_keys"`
	Path      string   `json:"path"`
	Installed bool     `json:"installed"`
}

type skillCandidate struct {
	ID        string `json:"id"`
	App       string `json:"app"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Bytes     int    `json:"bytes"`
	Preview   string `json:"preview"`
	Installed bool   `json:"installed"`
}

func candID(parts ...string) string {
	h := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:8])
}

func configRoots() []string {
	var out []string
	if v := os.Getenv("APPDATA"); v != "" {
		out = append(out, v)
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, "Library", "Application Support"))
		out = append(out, filepath.Join(home, ".config"))
		out = append(out, home)
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		out = append(out, v)
	}
	return out
}

func readHead(path string, limit int) (string, int, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return "", 0, false
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, false
	}
	defer f.Close()
	buf := make([]byte, limit)
	n, _ := f.Read(buf)
	s := strings.TrimSpace(string(buf[:n]))
	s = strings.Join(strings.Fields(s), " ")
	return s, int(info.Size()), true
}

func countEntries(text string) int {
	blocks := regexp.MustCompile(`\n\s*\n`).Split(text, -1)
	n := 0
	for _, b := range blocks {
		if strings.TrimSpace(b) != "" {
			n++
		}
	}
	if n == 0 {
		return 1
	}
	return n
}

type mdSource struct{ App, Kind, Path, Title string }

func markdownSources(workspace string) []mdSource {
	home, _ := os.UserHomeDir()
	j := func(parts ...string) string { return filepath.Join(append([]string{home}, parts...)...) }
	out := []mdSource{
		{"Claude Code", "记忆", j(".claude", "CLAUDE.md"), "CLAUDE.md"},
		{"Codex CLI", "记忆", j(".codex", "AGENTS.md"), "AGENTS.md"},
		{"Gemini CLI", "记忆", j(".gemini", "GEMINI.md"), "GEMINI.md"},
		{"CC Switch", "记忆", j(".agents", "AGENTS.md"), "AGENTS.md"},
		{"OpenClaw", "记忆", j(".openclaw", "workspace", "AGENTS.md"), "AGENTS.md"},
		{"Kimi OpenClaw", "记忆", j(".kimi_openclaw", "workspace", "AGENTS.md"), "AGENTS.md"},
		{"Windsurf", "记忆", j(".codeium", "windsurf", "memories"), "memories"},
	}
	for _, root := range configRoots() {
		out = append(out, mdSource{"Claude Desktop", "记忆", filepath.Join(root, "Claude", "CLAUDE.md"), "CLAUDE.md"})
	}
	if workspace != "" {
		out = append(out,
			mdSource{"当前工作区", "记忆", filepath.Join(workspace, "CLAUDE.md"), "CLAUDE.md"},
			mdSource{"当前工作区", "记忆", filepath.Join(workspace, "AGENTS.md"), "AGENTS.md"},
			mdSource{"当前工作区", "规则", filepath.Join(workspace, ".cursorrules"), ".cursorrules"},
			mdSource{"当前工作区", "规则", filepath.Join(workspace, ".github", "copilot-instructions.md"), "copilot-instructions.md"},
		)
	}
	return out
}

func projectRoots(workspace string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	out := []string{}
	if strings.TrimSpace(workspace) != "" {
		out = append(out, workspace)
	}
	for _, name := range []string{"Documents", "Desktop", "dev", "Downloads", "projects", "code", "repos", "src", "work", "workspace"} {
		p := filepath.Join(home, name)
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

var projectRuleNames = []string{"CLAUDE.md", "AGENTS.md", "GEMINI.md", ".cursorrules", "copilot-instructions.md"}

const (
	projectScanDepth   = 3
	projectScanMaxDirs = 4000
	projectScanMaxHits = 80
)

func scanProjectMemory(workspace string, seen map[string]bool) []memCandidate {
	type item struct {
		dir   string
		depth int
	}
	roots := projectRoots(workspace)
	queue := make([]item, 0, len(roots))
	for _, r := range roots {
		queue = append(queue, item{r, 0})
	}
	skip := map[string]bool{"node_modules": true, "vendor": true, "dist": true, "build": true,
		"target": true, "__pycache__": true, ".git": true, ".venv": true, "venv": true}
	out := []memCandidate{}
	visited := 0
	for len(queue) > 0 && visited < projectScanMaxDirs && len(out) < projectScanMaxHits {
		it := queue[0]
		queue = queue[1:]
		visited++
		entries, err := os.ReadDir(it.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			full := filepath.Join(it.dir, e.Name())
			if e.IsDir() {
				hidden := strings.HasPrefix(e.Name(), ".")
				if it.depth >= projectScanDepth || skip[e.Name()] || (hidden && e.Name() != ".github" && e.Name() != ".cursor") {
					continue
				}
				queue = append(queue, item{full, it.depth + 1})
				continue
			}
			matched := false
			for _, n := range projectRuleNames {
				if strings.EqualFold(e.Name(), n) {
					matched = true
					break
				}
			}
			if !matched || seen[full] {
				continue
			}
			head, size, ok := readHead(full, 400)
			if !ok {
				continue
			}
			seen[full] = true
			body, _ := os.ReadFile(full)
			out = append(out, memCandidate{
				ID: candID("项目", full), App: "项目 · " + filepath.Base(it.dir), Kind: "记忆",
				Path: full, Title: e.Name(), Bytes: size, Entries: countEntries(string(body)), Preview: head,
			})
			if len(out) >= projectScanMaxHits {
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func scanMemory(workspace string) []memCandidate {
	out := []memCandidate{}
	seen := map[string]bool{}
	add := func(app, kind, path, title string) {
		path = filepath.Clean(path)
		if seen[path] {
			return
		}
		head, size, ok := readHead(path, 400)
		if !ok {
			return
		}
		seen[path] = true
		full, _ := os.ReadFile(path)
		out = append(out, memCandidate{
			ID: candID(app, path), App: app, Kind: kind, Path: path, Title: title,
			Bytes: size, Entries: countEntries(string(full)), Preview: head,
		})
	}
	for _, s := range markdownSources(workspace) {
		if info, err := os.Stat(s.Path); err == nil && info.IsDir() {
			if ents, err := os.ReadDir(s.Path); err == nil {
				for _, e := range ents {
					if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
						add(s.App, s.Kind, filepath.Join(s.Path, e.Name()), e.Name())
					}
				}
			}
			continue
		}
		add(s.App, s.Kind, s.Path, s.Title)
	}
	for _, root := range configRoots() {
		dir := filepath.Join(root, ".cursor", "rules")
		if ents, err := os.ReadDir(dir); err == nil {
			for _, e := range ents {
				if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".mdc") {
					continue
				}
				add("Cursor", "规则", filepath.Join(dir, e.Name()), e.Name())
			}
		}
	}
	out = append(out, scanProjectMemory(workspace, seen)...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].App != out[j].App {
			return out[i].App < out[j].App
		}
		return out[i].Path < out[j].Path
	})
	return out
}

type mcpJSONFile struct{ App, Path string }

func mcpJSONFiles() []mcpJSONFile {
	home, _ := os.UserHomeDir()
	out := []mcpJSONFile{
		{"Claude Code", filepath.Join(home, ".claude.json")},
		{"Cursor", filepath.Join(home, ".cursor", "mcp.json")},
		{"Gemini CLI", filepath.Join(home, ".gemini", "settings.json")},
		{"Windsurf", filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")},
		{"CodeBuddy", filepath.Join(home, ".codebuddy", "mcp.json")},
	}
	for _, root := range configRoots() {
		out = append(out,
			mcpJSONFile{"Claude Desktop", filepath.Join(root, "Claude", "claude_desktop_config.json")},
			mcpJSONFile{"VS Code", filepath.Join(root, "Code", "User", "mcp.json")},
		)
	}
	return out
}

func parseMCPJSON(raw []byte) map[string]struct {
	Command string
	Args    []string
	EnvKeys []string
} {
	var doc struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	out := map[string]struct {
		Command string
		Args    []string
		EnvKeys []string
	}{}
	if json.Unmarshal(raw, &doc) != nil {
		return out
	}
	for name, s := range doc.MCPServers {
		keys := make([]string, 0, len(s.Env))
		for k := range s.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out[name] = struct {
			Command string
			Args    []string
			EnvKeys []string
		}{s.Command, s.Args, keys}
	}
	return out
}

func parseCodexTOML(text string) map[string]struct {
	Command string
	Args    []string
} {
	out := map[string]struct {
		Command string
		Args    []string
	}{}
	var cur string
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			cur = ""
			if m := regexp.MustCompile(`^\[mcp_servers\.([^\]\.]+)\]$`).FindStringSubmatch(t); m != nil {
				cur = strings.Trim(m[1], `"'`)
				out[cur] = struct {
					Command string
					Args    []string
				}{}
			}
			continue
		}
		if cur == "" || t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		key, val, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		e := out[cur]
		switch key {
		case "command":
			e.Command = strings.Trim(val, `"'`)
		case "args":
			for _, a := range strings.Split(strings.Trim(val, "[]"), ",") {
				a = strings.Trim(strings.TrimSpace(a), `"'`)
				if a != "" {
					e.Args = append(e.Args, a)
				}
			}
		}
		out[cur] = e
	}
	return out
}

func scanMCP(installed map[string]bool) ([]mcpCandidate, int) {
	out := []mcpCandidate{}
	seen := map[string]bool{}
	skipped := 0
	push := func(app, path, name, command string, args, envKeys []string) {
		key := app + "|" + name
		if seen[key] {
			return
		}
		if strings.TrimSpace(command) == "" {
			seen[key] = true
			skipped++
			return
		}
		seen[key] = true
		out = append(out, mcpCandidate{
			ID: candID(app, path, name), App: app, Name: name, Command: command,
			Args: args, EnvKeys: envKeys, Path: path, Installed: installed[name],
		})
	}
	for _, f := range mcpJSONFiles() {
		raw, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		for name, s := range parseMCPJSON(raw) {
			push(f.App, f.Path, name, s.Command, s.Args, s.EnvKeys)
		}
	}
	home, _ := os.UserHomeDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if raw, err := os.ReadFile(codexPath); err == nil {
		for name, s := range parseCodexTOML(string(raw)) {
			push("Codex CLI", codexPath, name, s.Command, s.Args, nil)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].App != out[j].App {
			return out[i].App < out[j].App
		}
		return out[i].Name < out[j].Name
	})
	return out, skipped
}

func scanSkills(installed map[string]bool) []skillCandidate {
	home, _ := os.UserHomeDir()
	out := []skillCandidate{}
	dirs := []struct{ App, Dir string }{
		{"共享技能库 (~/.agents)", filepath.Join(home, ".agents", "skills")},
		{"CC Switch", filepath.Join(home, ".cc-switch", "skills")},
		{"Claude Code", filepath.Join(home, ".claude", "skills")},
		{"Codex CLI", filepath.Join(home, ".codex", "skills")},
		{"Cursor", filepath.Join(home, ".cursor", "skills")},
		{"Windsurf", filepath.Join(home, ".codeium", "windsurf", "skills")},
		{"Copilot", filepath.Join(home, ".copilot", "skills")},
	}
	for _, d := range dirs {
		ents, err := os.ReadDir(d.Dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(d.Dir, e.Name(), "SKILL.md")
			head, size, ok := readHead(p, 300)
			if !ok {
				continue
			}
			out = append(out, skillCandidate{
				ID: candID(d.App, p), App: d.App, Name: e.Name(), Path: p,
				Bytes: size, Preview: head, Installed: installed[e.Name()],
			})
		}
	}
	return out
}

func localImportScan(a *agent.Agent) map[string]any {
	instMCP := map[string]bool{}
	for _, m := range a.MCPList() {
		if n, ok := m["name"].(string); ok {
			instMCP[n] = true
		}
	}
	instSkill := map[string]bool{}
	for _, s := range a.SkillList() {
		instSkill[s.Name] = true
	}
	workspace, _ := a.WorkspaceView()["workspace"].(string)
	mem := scanMemory(workspace)
	mcp, mcpSkipped := scanMCP(instMCP)
	skills := scanSkills(instSkill)
	return map[string]any{
		"memory": mem, "mcp": mcp, "skills": skills,
		"mcp_skipped": mcpSkipped,
		"counts":      map[string]int{"memory": len(mem), "mcp": len(mcp), "skills": len(skills)},
	}
}

func (s *Server) handleImportScan(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, localImportScan(s.Agent))
}

func (s *Server) handleImportApply(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Memory []string `json:"memory"`
		MCP    []string `json:"mcp"`
		Skills []string `json:"skills"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "参数解析失败")
		return
	}
	snap := localImportScan(s.Agent)

	memByID := map[string]memCandidate{}
	for _, m := range snap["memory"].([]memCandidate) {
		memByID[m.ID] = m
	}
	mcpByID := map[string]mcpCandidate{}
	for _, m := range snap["mcp"].([]mcpCandidate) {
		mcpByID[m.ID] = m
	}

	res := map[string]any{}
	importedMem, skippedMem := 0, 0
	for _, id := range body.Memory {
		c, ok := memByID[id]
		if !ok {
			skippedMem++
			continue
		}
		raw, err := os.ReadFile(c.Path)
		if err != nil {
			skippedMem++
			continue
		}
		n := 0
		for _, block := range regexp.MustCompile(`\n\s*\n`).Split(string(raw), -1) {
			block = strings.TrimSpace(block)
			if block == "" {
				continue
			}
			if _, err := s.Agent.MemorySave(block, []string{"导入", c.App}); err == nil {
				n++
			}
		}
		if n > 0 {
			importedMem++
		} else {
			skippedMem++
		}
	}
	res["memory"] = map[string]int{"imported": importedMem, "skipped": skippedMem}

	instMCP, dupMCP, failMCP := 0, 0, 0
	for _, id := range body.MCP {
		c, ok := mcpByID[id]
		if !ok {
			failMCP++
			continue
		}
		_, err := s.Agent.MCPInstallCustom(c.Name, c.Command, c.Args, nil, "user_approved", false)
		switch {
		case err == nil:
			instMCP++
		case strings.Contains(err.Error(), agent.ErrAlreadyInstalled.Error()):
			dupMCP++
		default:
			failMCP++
		}
	}
	res["mcp"] = map[string]int{"imported": instMCP, "already": dupMCP, "failed": failMCP}

	writeJSON(w, 200, res)
}
