package skill

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gleam/internal/atomicfile"
	"gleam/internal/config"
	"gleam/pkg/types"
)

type Skill struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Version     int          `json:"version"`
	Params      []string     `json:"params,omitempty"`
	Steps       []types.Step `json:"steps"`
	Runs        int          `json:"runs"`
	Successes   int          `json:"successes"`
	LastUsed    *time.Time   `json:"last_used,omitempty"`
	Disabled    bool         `json:"disabled,omitempty"`
}

type Store struct {
	mu  sync.Mutex
	dir string
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

var nameRe = regexp.MustCompile(`^[a-zA-Z0-9_\-\p{Han}]{1,64}$`)

func (s *Skill) Validate() error {
	if !nameRe.MatchString(s.Name) {
		return fmt.Errorf("技能名 %q 非法（仅限字母数字下划线连字符与中文，≤64 字符）", s.Name)
	}
	if len(s.Steps) == 0 {
		return fmt.Errorf("技能至少需要一个步骤")
	}
	steps := append([]types.Step(nil), s.Steps...)
	for i := range steps {
		if steps[i].Tool == "" {
			return fmt.Errorf("步骤 %d 缺少 tool", i+1)
		}
		if steps[i].ID == "" {
			steps[i].ID = fmt.Sprintf("s%d", i+1)
		}
	}
	if err := types.ValidateStepGraph(steps); err != nil {
		return err
	}
	s.Steps = steps
	return nil
}

func (st *Store) Save(s Skill) (int, error) {
	if err := s.Validate(); err != nil {
		return 0, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	existing, err := st.loadLocked(s.Name)
	if err == nil && existing != nil {
		s.Version = existing.Version + 1
		s.Runs = existing.Runs
		s.Successes = existing.Successes
		s.LastUsed = existing.LastUsed
		s.Disabled = existing.Disabled
	} else {
		s.Version = 1
	}
	if err := st.writeLocked(s); err != nil {
		return 0, err
	}
	return s.Version, nil
}

func (st *Store) writeLocked(s Skill) error {
	data, err := marshalSkill(s)
	if err != nil {
		return err
	}
	return atomicfile.Write(st.pathFor(s.Name), data, 0o644)
}

func (st *Store) Get(name string) (*Skill, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.loadLocked(name)
}

func (st *Store) loadLocked(name string) (*Skill, error) {
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("技能名 %q 非法", name)
	}
	data, err := os.ReadFile(st.pathFor(name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("技能 %q 不存在", name)
		}
		return nil, err
	}
	return unmarshalSkill(data)
}

func (st *Store) Delete(name string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if !nameRe.MatchString(name) {
		return fmt.Errorf("技能名 %q 非法", name)
	}
	path := st.pathFor(name)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("技能 %q 不存在", name)
	}
	return os.Remove(path)
}

func (st *Store) List() []Skill {
	st.mu.Lock()
	defer st.mu.Unlock()
	entries, _ := os.ReadDir(st.dir)
	out := make([]Skill, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(st.dir, e.Name()))
		if err != nil {
			continue
		}
		if s, err := unmarshalSkill(data); err == nil {
			out = append(out, *s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (st *Store) RecordRun(name string, ok bool) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, err := st.loadLocked(name)
	if err != nil {
		return err
	}
	s.Runs++
	if ok {
		s.Successes++
	}
	now := time.Now()
	s.LastUsed = &now
	return st.writeLocked(*s)
}

func (st *Store) SetDisabled(name string, disabled bool) (Skill, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, err := st.loadLocked(name)
	if err != nil {
		return Skill{}, err
	}
	if s.Disabled == disabled {
		return *s, nil
	}
	s.Disabled = disabled
	if err := st.writeLocked(*s); err != nil {
		return Skill{}, err
	}
	return *s, nil
}

func (st *Store) ListSummaries() []struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     int    `json:"version"`
	Runs        int    `json:"runs"`
	Successes   int    `json:"successes"`
} {
	list := st.List()
	out := make([]struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     int    `json:"version"`
		Runs        int    `json:"runs"`
		Successes   int    `json:"successes"`
	}, 0, len(list))
	for _, s := range list {
		if s.Disabled {
			continue
		}
		out = append(out, struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Version     int    `json:"version"`
			Runs        int    `json:"runs"`
			Successes   int    `json:"successes"`
		}{s.Name, s.Description, s.Version, s.Runs, s.Successes})
	}
	return out
}

func (st *Store) GetParams(name string) ([]string, error) {
	s, err := st.Get(name)
	if err != nil {
		return nil, err
	}
	return s.Params, nil
}

func (st *Store) pathFor(name string) string {
	return filepath.Join(st.dir, name+".yaml")
}

func marshalSkill(s Skill) ([]byte, error) {
	root := config.NewYMap()
	root.Set("name", s.Name)
	root.Set("description", s.Description)
	root.Set("version", s.Version)
	if len(s.Params) > 0 {
		params := make([]any, 0, len(s.Params))
		for _, p := range s.Params {
			params = append(params, p)
		}
		root.Set("params", params)
	}
	steps := make([]any, 0, len(s.Steps))
	for _, stp := range s.Steps {
		sm := config.NewYMap()
		sm.Set("id", stp.ID)
		sm.Set("tool", stp.Tool)
		sm.Set("description", stp.Description)
		if len(stp.Args) > 0 {
			sm.Set("args", toYValue(stp.Args))
		}
		if len(stp.DependsOn) > 0 {
			deps := make([]any, 0, len(stp.DependsOn))
			for _, d := range stp.DependsOn {
				deps = append(deps, d)
			}
			sm.Set("depends_on", deps)
		}
		steps = append(steps, sm)
	}
	root.Set("steps", steps)
	root.Set("runs", s.Runs)
	root.Set("successes", s.Successes)
	if s.Disabled {
		root.Set("disabled", true)
	}
	if s.LastUsed != nil {
		root.Set("last_used", s.LastUsed.Format(time.RFC3339))
	}
	return config.MarshalYAML(root)
}

func toYValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := config.NewYMap()
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			m.Set(k, toYValue(t[k]))
		}
		return m
	case []any:
		arr := make([]any, 0, len(t))
		for _, item := range t {
			arr = append(arr, toYValue(item))
		}
		return arr
	default:
		return v
	}
}

func unmarshalSkill(data []byte) (*Skill, error) {
	m, err := config.ParseYAML(string(data))
	if err != nil {
		return nil, fmt.Errorf("解析技能文件失败: %w", err)
	}
	s := &Skill{}
	s.Name, _ = m["name"].(string)
	s.Description, _ = m["description"].(string)
	s.Version = asInt(m["version"])
	s.Runs = asInt(m["runs"])
	s.Successes = asInt(m["successes"])
	s.Disabled, _ = m["disabled"].(bool)
	if v, ok := m["last_used"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			s.LastUsed = &t
		}
	}
	if arr, ok := m["params"].([]any); ok {
		for _, p := range arr {
			if str, ok := p.(string); ok {
				s.Params = append(s.Params, str)
			}
		}
	}
	if arr, ok := m["steps"].([]any); ok {
		for i, item := range arr {
			sm, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("步骤 %d 格式错误", i+1)
			}
			stp := types.Step{
				ID:          asStr(sm["id"]),
				Tool:        asStr(sm["tool"]),
				Description: asStr(sm["description"]),
			}
			if args, ok := sm["args"].(map[string]any); ok {
				stp.Args = args
			}
			if deps, ok := sm["depends_on"].([]any); ok {
				for _, d := range deps {
					if ds, ok := d.(string); ok {
						stp.DependsOn = append(stp.DependsOn, ds)
					}
				}
			}
			s.Steps = append(s.Steps, stp)
		}
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

func asStr(v any) string {
	s, _ := v.(string)
	return s
}

// asInt narrows a decoded JSON number to int. Values arrive from skill argument
// files, so out-of-range input must degrade to the zero value instead of
// wrapping around when the target int is narrower than the source.
func asInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		if t < math.MinInt || t > math.MaxInt {
			return 0
		}
		return int(t)
	case float64:
		// Bound the float first: 2^63 is the first value float64 cannot round
		// back into an int64, so comparing against that exact limit keeps the
		// narrowing below well defined.
		if math.IsNaN(t) || t < math.MinInt64 || t >= math.MaxInt64 {
			return 0
		}
		n := int64(t)
		if n < math.MinInt || n > math.MaxInt {
			return 0
		}
		return int(n)
	}
	return 0
}

func SubstituteParams(args map[string]any, params map[string]string) (map[string]any, error) {
	if len(args) == 0 {
		return args, nil
	}
	out := make(map[string]any, len(args))
	var missing []string
	for k, v := range args {
		out[k] = substValue(v, params, &missing)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("缺少技能参数: %s", strings.Join(missing, ", "))
	}
	return out, nil
}

var phRe = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_\-]+)\s*\}\}`)

func substValue(v any, params map[string]string, missing *[]string) any {
	switch t := v.(type) {
	case string:
		if m := phRe.FindStringSubmatch(t); m != nil && m[0] == t {
			if val, ok := params[m[1]]; ok {
				return val
			}
			*missing = append(*missing, m[1])
			return t
		}
		return phRe.ReplaceAllStringFunc(t, func(ph string) string {
			m := phRe.FindStringSubmatch(ph)
			if val, ok := params[m[1]]; ok {
				return val
			}
			*missing = append(*missing, m[1])
			return ph
		})
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			out[k] = substValue(item, params, missing)
		}
		return out
	case []any:
		arr := make([]any, 0, len(t))
		for _, item := range t {
			arr = append(arr, substValue(item, params, missing))
		}
		return arr
	default:
		return v
	}
}
