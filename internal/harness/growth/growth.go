package growth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"gleam/internal/atomicfile"
)

type Log struct {
	mu      sync.Mutex
	path    string
	entries []Entry
}

type Entry struct {
	ID        string    `json:"id"`
	Time      time.Time `json:"time"`
	Type      string    `json:"type"`
	Goal      string    `json:"goal,omitempty"`
	Score     int       `json:"score,omitempty"`
	Steps     int       `json:"steps,omitempty"`
	Duration  float64   `json:"duration_seconds,omitempty"`
	Tokens    int       `json:"tokens,omitempty"`
	LLMCalls  int       `json:"llm_calls,omitempty"`
	ToolCalls int       `json:"tool_calls,omitempty"`
	SkillName string    `json:"skill_name,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	RuleSet   string    `json:"rule_set,omitempty"`
	Retried   bool      `json:"retried,omitempty"`
	Delivery  bool      `json:"delivery,omitempty"`
	FirstPass bool      `json:"first_pass,omitempty"`
	Reworks   int       `json:"reworks,omitempty"`
}

type Stats struct {
	TotalTasks     int            `json:"total_tasks"`
	TotalSkills    int            `json:"total_skills"`
	SkillUses      int            `json:"skill_uses"`
	AvgScore       float64        `json:"avg_score"`
	TotalDuration  float64        `json:"total_duration_seconds"`
	TotalTokens    int            `json:"total_tokens"`
	WeekTokens     int            `json:"week_tokens"`
	TotalLLMCalls  int            `json:"total_llm_calls"`
	AvgTokens      float64        `json:"avg_tokens_per_task"`
	TasksByType    map[string]int `json:"tasks_by_type"`
	RecentStreak   int            `json:"recent_streak"`
	RetryCount     int            `json:"retry_count"`
	AbortedTasks   int            `json:"aborted_tasks"`
	RetryRate      float64        `json:"retry_rate"`
	AbortRate      float64        `json:"abort_rate"`
	FirstPassCount int            `json:"first_pass_count"`
	FirstPassBase  int            `json:"first_pass_base"`
	FirstPassRate  float64        `json:"first_pass_rate"`
	ReworksTotal   int            `json:"reworks_total"`
	Level          string         `json:"level"`
	LevelProgress  float64        `json:"level_progress"`
}

func Open(dataDir string) (*Log, error) {
	path := filepath.Join(dataDir, "growth.json")
	l := &Log{path: path}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return l, nil
		}
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	l.entries = entries
	return l, nil
}

func (l *Log) Record(e Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e.ID == "" {
		e.ID = fmtEntryID(l.entries)
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	l.entries = append(l.entries, e)
	l.persist()
}

func (l *Log) persist() {
	data, err := json.MarshalIndent(l.entries, "", "  ")
	if err != nil {
		return
	}
	_ = atomicfile.Write(l.path, data, 0o644)
}

func (l *Log) Recent(n int) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > len(l.entries) {
		n = len(l.entries)
	}
	start := len(l.entries) - n
	if start < 0 {
		start = 0
	}
	result := make([]Entry, n)
	for i := 0; i < n; i++ {
		result[i] = l.entries[start+n-1-i]
	}
	return result
}

func (l *Log) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()

	s := Stats{TasksByType: map[string]int{}}
	var totalScore float64
	scoredTasks := 0
	weekAgo := time.Now().AddDate(0, 0, -7)

	for _, e := range l.entries {
		switch e.Type {
		case "task_completed":
			s.TotalTasks++
			s.TasksByType["task"]++
			if e.Retried {
				s.RetryCount++
			}
			if e.Delivery {
				s.FirstPassBase++
				if e.FirstPass {
					s.FirstPassCount++
				}
			}
			s.ReworksTotal += e.Reworks
			if e.Score > 0 {
				totalScore += float64(e.Score)
				scoredTasks++
			}
			s.TotalDuration += e.Duration
			s.TotalTokens += e.Tokens
			s.TotalLLMCalls += e.LLMCalls
			if e.Time.After(weekAgo) {
				s.WeekTokens += e.Tokens
			}
		case "task_aborted":
			s.AbortedTasks++
			s.ReworksTotal += e.Reworks
		case "skill_created":
			s.TotalSkills++
			s.TasksByType["skill_created"]++
		case "skill_used":
			s.SkillUses++
			s.TasksByType["skill_used"]++
		}
	}

	if submitted := s.TotalTasks + s.AbortedTasks; submitted > 0 {
		s.RetryRate = float64(s.RetryCount) / float64(submitted)
		s.AbortRate = float64(s.AbortedTasks) / float64(submitted)
	}

	if s.FirstPassBase > 0 {
		s.FirstPassRate = float64(s.FirstPassCount) / float64(s.FirstPassBase)
	}

	if scoredTasks > 0 {
		s.AvgScore = totalScore / float64(scoredTasks)
		s.AvgTokens = float64(s.TotalTokens) / float64(scoredTasks)
	}

	s.RecentStreak = l.computeStreak()

	score := s.TotalTasks*10 + s.TotalSkills*20 + s.SkillUses*5
	s.Level, s.LevelProgress = computeLevel(score)

	return s
}

func (l *Log) computeStreak() int {
	if len(l.entries) == 0 {
		return 0
	}
	last := l.entries[len(l.entries)-1].Time
	today := time.Now()
	days := int(today.Sub(last.Truncate(24*time.Hour)).Hours() / 24)
	if days > 1 {
		return 0
	}
	seen := map[string]bool{}
	for _, e := range l.entries {
		seen[e.Time.Format("2006-01-02")] = true
	}
	streak := 0
	d := today
	for {
		key := d.Format("2006-01-02")
		if !seen[key] {
			break
		}
		streak++
		d = d.AddDate(0, 0, -1)
	}
	return streak
}

func computeLevel(score int) (string, float64) {
	levels := []struct {
		name string
		min  int
	}{
		{"萌新微光", 0},
		{"初识微光", 50},
		{"渐入佳境", 150},
		{"独当一面", 350},
		{"行家里手", 700},
		{"微光大师", 1200},
		{"光华璀璨", 2000},
	}
	for i := len(levels) - 1; i >= 0; i-- {
		if score >= levels[i].min {
			progress := 0.0
			if i < len(levels)-1 {
				next := levels[i+1].min
				progress = float64(score-levels[i].min) / float64(next-levels[i].min)
			} else {
				progress = 1.0
			}
			return levels[i].name, progress
		}
	}
	return levels[0].name, 0
}

func fmtEntryID(existing []Entry) string {
	max := 0
	for _, e := range existing {
		if len(e.ID) > 1 && e.ID[0] == 'g' {
			n := 0
			for _, c := range e.ID[1:] {
				if c >= '0' && c <= '9' {
					n = n*10 + int(c-'0')
				} else {
					break
				}
			}
			if n > max {
				max = n
			}
		}
	}
	return fmtID(max + 1)
}

func fmtID(n int) string {
	s := ""
	if n == 0 {
		return "g0"
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return "g" + s
}

func (l *Log) All() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, len(l.entries))
	copy(out, l.entries)
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}
