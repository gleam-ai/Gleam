// Package mdskill is the native runtime for markdown instruction skills
// (SKILL.md).
//
// It complements the JSON step skills in harness/skill (replayable fixed
// tool sequences): an md skill carries an instruction document -- the model
// opens it and acts on the instructions itself. Discovery uses two-level
// progressive disclosure: List() hands out only summaries (name /
// description / whenToUse), Open(name) hands out the full text.
//
// Deliberately no cache: every call rescans the disk (the skill directories
// are small, milliseconds), which buys zero invalidation and live-on-save --
// consistent with the positioning that skill files are hand-edited documents.
package mdskill

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Skill is one discovered md skill. Content is only filled in by Open.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	WhenToUse   string `json:"when_to_use,omitempty"`
	Path        string `json:"path"`
	Source      string `json:"source"`    // root label, e.g. "workspace" or "user"
	UserOnly    bool   `json:"user_only"` // disable-model-invocation: not for the model to call on its own
	Content     string `json:"content,omitempty"`
}

// Root is one discovery root. Label shows the origin in listings; roots are
// ranked by position (earlier roots shadow later ones on name conflicts).
type Root struct {
	Dir   string
	Label string
}

var kebabRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const skillFile = "SKILL.md"

// List scans all roots and returns deduplicated skill summaries in
// discovery order (first one wins on the same name). A missing or empty
// directory is not an error; a single broken entry is skipped
// (fail-closed per entry) -- nothing broken is ever exposed.
func List(roots []Root) []Skill {
	seen := map[string]bool{}
	var out []Skill
	for _, r := range roots {
		for _, sk := range scanRoot(r) {
			if seen[sk.Name] {
				continue
			}
			seen[sk.Name] = true
			out = append(out, sk)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Open loads a skill by name and fills in the full text. Discovery order is
// the priority: the first entry with that name wins.
func Open(roots []Root, name string) (Skill, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Skill{}, fmt.Errorf("技能名不能为空")
	}
	if !kebabRe.MatchString(name) {
		return Skill{}, fmt.Errorf("技能名 %q 不是合法的 kebab-case", name)
	}
	for _, r := range roots {
		for _, sk := range scanRoot(r) {
			if sk.Name == name {
				raw, err := os.ReadFile(sk.Path)
				if err != nil {
					return Skill{}, fmt.Errorf("读取技能文件失败: %w", err)
				}
				sk.Content = string(raw)
				return sk, nil
			}
		}
	}
	return Skill{}, fmt.Errorf("技能 %q 不存在；先用 skill.list 查看可用技能", name)
}

// scanRoot scans a single root, supporting both layouts:
// <root>/<name>/SKILL.md and the flat <root>/<name>.md. One level deep, no
// recursion -- skills are a flat list, not a directory tree.
func scanRoot(r Root) []Skill {
	ents, err := os.ReadDir(r.Dir)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, e := range ents {
		var p string
		fallbackName := ""
		switch {
		case e.IsDir():
			p = filepath.Join(r.Dir, e.Name(), skillFile)
			fallbackName = e.Name()
		case strings.EqualFold(filepath.Ext(e.Name()), ".md"):
			p = filepath.Join(r.Dir, e.Name())
			fallbackName = strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		default:
			continue
		}
		sk, err := parseFile(p, r.Label, fallbackName)
		if err != nil {
			continue // an invalid frontmatter drops the whole entry: better fewer than broken
		}
		out = append(out, sk)
	}
	return out
}

// parseFile parses one SKILL.md frontmatter. name/description are required;
// an absent name falls back to the directory/file name (which must still be
// kebab-case).
func parseFile(path, source, fallbackName string) (Skill, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	meta, err := parseFrontmatter(string(raw))
	if err != nil {
		return Skill{}, err
	}
	name := meta["name"]
	if name == "" {
		name = fallbackName
	}
	if !kebabRe.MatchString(name) {
		return Skill{}, fmt.Errorf("技能名 %q 不是 kebab-case", name)
	}
	desc := meta["description"]
	if strings.TrimSpace(desc) == "" {
		return Skill{}, fmt.Errorf("技能 %s 缺少 description", name)
	}
	return Skill{
		Name:        name,
		Description: desc,
		WhenToUse:   meta["when_to_use"],
		Path:        path,
		Source:      source,
		UserOnly:    meta["disable-model-invocation"] == "true",
	}, nil
}

// parseFrontmatter parses the `---`-wrapped block at the top of the file,
// taking only top-level `key: value` lines; everything else (nesting,
// lists, body) is ignored. It does not aim for the full YAML grammar --
// skill metadata should stay simple.
func parseFrontmatter(raw string) (map[string]string, error) {
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, fmt.Errorf("缺少 frontmatter")
	}
	meta := map[string]string{}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			return meta, nil
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || strings.ContainsAny(key, " \t") {
			continue // indented nested keys are not metadata
		}
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if _, exists := meta[key]; !exists {
			meta[key] = val
		}
	}
	return nil, fmt.Errorf("frontmatter 未闭合")
}
