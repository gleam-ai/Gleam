package memory

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

type Turn struct {
	Role    string    `json:"role"`
	Content string    `json:"content"`
	At      time.Time `json:"at"`
}

type ShortTerm struct {
	mu   sync.Mutex
	buf  []Turn
	cap  int
	next int
	full bool
}

func NewShortTerm(cap int) *ShortTerm {
	if cap <= 0 {
		cap = 20
	}
	return &ShortTerm{buf: make([]Turn, 0, cap), cap: cap}
}

func (s *ShortTerm) Add(role, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := Turn{Role: role, Content: content, At: time.Now()}
	if len(s.buf) < s.cap {
		s.buf = append(s.buf, t)
	} else {
		s.buf[s.next] = t
		s.next = (s.next + 1) % s.cap
		s.full = true
	}
}

func (s *ShortTerm) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = make([]Turn, 0, s.cap)
	s.next = 0
	s.full = false
}

func (s *ShortTerm) Recent(n int) []Turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ordered []Turn
	if s.full {
		ordered = append(ordered, s.buf[s.next:]...)
		ordered = append(ordered, s.buf[:s.next]...)
	} else {
		ordered = append(ordered, s.buf...)
	}
	if n <= 0 || n >= len(ordered) {
		return append([]Turn(nil), ordered...)
	}
	return append([]Turn(nil), ordered[len(ordered)-n:]...)
}

func (s *ShortTerm) Oldest() (Turn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) == 0 {
		return Turn{}, false
	}
	var t Turn
	if s.full {
		t = s.buf[s.next]
	} else {
		t = s.buf[0]
	}
	return t, true
}

func (s *ShortTerm) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buf)
}

type Item struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Vec       []float32 `json:"vec"`
	Source    string    `json:"source,omitempty"`
	Deleted   bool      `json:"deleted,omitempty"`
	Scope     string    `json:"scope,omitempty"`
}

const MinHitScore = 0.15

const DupThreshold = 0.92

type Hit struct {
	ID      string   `json:"id"`
	Content string   `json:"content"`
	Tags    []string `json:"tags,omitempty"`
	Score   float32  `json:"score"`
}

func tokenize(text string) []string {
	var tokens []string
	var latin strings.Builder
	var cjk []rune
	flushLatin := func() {
		if latin.Len() > 0 {
			tokens = append(tokens, latin.String())
			latin.Reset()
		}
	}
	flushCJK := func() {
		for i := 0; i+1 < len(cjk); i++ {
			tokens = append(tokens, string(cjk[i:i+2]))
		}
		cjk = cjk[:0]
	}
	for _, r := range text {
		switch {
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-':
			latin.WriteRune(toLower(r))
			flushCJK()
		case isCJK(r):
			flushLatin()
			cjk = append(cjk, r)
		default:
			flushLatin()
			flushCJK()
		}
	}
	flushLatin()
	flushCJK()
	return tokens
}

func toLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	return r
}

func isCJK(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF ||
		r >= 0x3400 && r <= 0x4DBF ||
		r >= 0xF900 && r <= 0xFAFF
}

func embed(text string, dim int) []float32 {
	if dim <= 0 {
		dim = 256
	}
	tf := map[string]float32{}
	for _, tok := range tokenize(text) {
		tf[tok]++
	}
	vec := make([]float32, dim)
	for tok, freq := range tf {
		idx := fnv1a(tok) % uint32(dim)
		vec[idx] += 1 + float32(freq-1)*0.5
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if norm == 0 {
		return vec
	}
	inv := float32(1.0 / math.Sqrt(norm))
	for i := range vec {
		vec[i] *= inv
	}
	return vec
}

const (
	fnvOffset uint32 = 2166136261
	fnvPrime  uint32 = 16777619
)

func fnv1a(s string) uint32 {
	h := fnvOffset
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= fnvPrime
	}
	return h
}

func cosine(a, b []float32) float32 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot float64
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
	}
	if dot > 1 {
		dot = 1
	}
	return float32(dot)
}

func SearchHitsTop(items []Item, query string, k int, dim int) []Hit {
	qv := embed(query, dim)
	type pair struct {
		idx   int
		score float32
	}
	pairs := make([]pair, 0, len(items))
	for i, it := range items {
		if it.Deleted {
			continue
		}
		score := cosine(qv, it.Vec)
		if score < MinHitScore {
			continue
		}
		pairs = append(pairs, pair{i, score})
	}
	for i := 0; i < k && i < len(pairs); i++ {
		max := i
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].score > pairs[max].score {
				max = j
			}
		}
		pairs[i], pairs[max] = pairs[max], pairs[i]
	}
	out := make([]Hit, 0, k)
	for i := 0; i < k && i < len(pairs); i++ {
		it := items[pairs[i].idx]
		out = append(out, Hit{ID: it.ID, Content: it.Content, Tags: it.Tags, Score: pairs[i].score})
	}
	return out
}

func ValidateItem(it Item) error {
	if it.ID == "" {
		return fmt.Errorf("记忆条目缺少 ID")
	}
	if strings.TrimSpace(it.Content) == "" {
		return fmt.Errorf("记忆条目内容为空")
	}
	return nil
}
