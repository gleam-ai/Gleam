package geo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gleam/internal/atomicfile"
	"gleam/pkg/types"
)

const maxRecords = 200

type Record struct {
	ID          string    `json:"id"`
	TaskID      string    `json:"task_id,omitempty"`
	Goal        string    `json:"goal,omitempty"`
	Score       int       `json:"score"`
	Summary     string    `json:"summary"`
	Strengths   []string  `json:"strengths"`
	Weaknesses  []string  `json:"weaknesses"`
	Actionables []Action  `json:"actionables"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
}

type Store struct {
	path    string
	mu      sync.Mutex
	records []Record
}

func Open(path string) (*Store, error) {
	s := &Store{path: path}
	if path == "" {
		return s, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return s, nil
	}
	var recs []Record
	if err := json.Unmarshal(data, &recs); err != nil {
		return s, nil
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].CreatedAt.Before(recs[j].CreatedAt) })
	s.records = recs
	return s, nil
}

func (s *Store) Add(r Record) Record {
	if s == nil {
		return r
	}
	if r.ID == "" {
		r.ID = types.NewID()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	if r.Source == "" {
		r.Source = "auto"
	}
	s.mu.Lock()
	s.records = append(s.records, r)
	if len(s.records) > maxRecords {
		s.records = s.records[len(s.records)-maxRecords:]
	}
	snapshot := make([]Record, len(s.records))
	copy(snapshot, s.records)
	s.mu.Unlock()

	_ = s.persist(snapshot)
	return r
}

func (s *Store) List(n int) []Record {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, len(s.records))
	copy(out, s.records)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

func (s *Store) Clear() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.records = nil
	s.mu.Unlock()
	_ = s.persist(nil)
}

func (s *Store) Stats() (total int, avg float64, best int) {
	if s == nil {
		return 0, 0, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	total = len(s.records)
	if total == 0 {
		return 0, 0, 0
	}
	sum := 0
	for _, r := range s.records {
		sum += r.Score
		if r.Score > best {
			best = r.Score
		}
	}
	return total, float64(sum) / float64(total), best
}

func (s *Store) persist(snapshot []Record) error {
	if s == nil || s.path == "" {
		return nil
	}
	if snapshot == nil {
		snapshot = []Record{}
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(s.path, data, 0o644)
}

var creativeStrong = []string{
	"文章", "文案", "博客", "公众号", "新闻稿", "演讲稿", "发言稿", "宣传", "软文",
	"小红书", "微博", "推文", "广告语", "slogan", "标题", "故事", "剧本", "诗",
}

var creativeWeak = []string{
	"写一篇", "写一份", "写个", "撰写", "起草", "拟写", "润色", "改写", "扩写", "缩写",
	"生成报告", "总结报告", "报告", "方案", "大纲", "邮件", "信", "简介", "介绍文案",
	"产品介绍", "说明书", "公告", "通知", "文案", "翻译", "论文", "摘要", "旁白",
}

var nonCreative = []string{
	"代码", "函数", "编译", "构建", "单元测试", "重构", "bug", "报错", "日志",
	"部署", "服务器", "数据库", "sql", "脚本执行", "表格", "excel", "csv",
	"安装", "配置环境", "性能调优",
}

func IsCreative(goal, role string) bool {
	if role == "writer" {
		return true
	}
	goal = strings.ToLower(strings.TrimSpace(goal))
	if goal == "" {
		return false
	}
	strong := containsAny(goal, creativeStrong)
	if role == "coder" || role == "ops" || role == "analyst" {
		return strong
	}
	if strong {
		return true
	}
	if containsAny(goal, nonCreative) && !containsAny(goal, []string{"文案", "文章", "博客"}) {
		return false
	}
	return containsAny(goal, creativeWeak)
}

func containsAny(s string, xs []string) bool {
	for _, x := range xs {
		if x != "" && strings.Contains(s, strings.ToLower(x)) {
			return true
		}
	}
	return false
}
