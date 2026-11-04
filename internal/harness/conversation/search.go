package conversation

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Full-text conversation search. Deliberately index-free: a single machine
// holds a few hundred conversation files at most, so a full scan finishes in
// milliseconds and buys "zero invalidation, live on every write" -- an index
// is the stateful burden here.

// SearchHit is one search match: a conversation plus match statistics and
// context excerpts.
type SearchHit struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	SpaceID         string    `json:"space_id,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
	Count           int       `json:"count"`            // total hits across the whole conversation
	MatchedMessages []int     `json:"matched_messages"` // indexes of matching messages (0-based)
	Excerpts        []string  `json:"excerpts"`         // context excerpts, at most 3
}

const (
	searchDefaultLimit = 20
	searchMaxLimit     = 100
	// excerptRadius is how many characters the excerpt takes on each side of
	// the first matched term.
	excerptRadius = 60
	// maxExcerpts is how many excerpts one conversation may return.
	maxExcerpts = 3
)

// Search full-text searches every conversation. The query is split on
// whitespace into terms with AND semantics (every term must appear), matched
// case-insensitively as substrings against title + all message content (for
// Chinese this means whole-word containment). Ordering is deterministic:
// hit count desc, then updated time desc, then ID asc as the tiebreaker.
func (s *Store) Search(query string, limit int) ([]SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("检索词不能为空")
	}
	terms := strings.Fields(strings.ToLower(query))
	if limit <= 0 {
		limit = searchDefaultLimit
	}
	if limit > searchMaxLimit {
		limit = searchMaxLimit
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("读取会话目录失败: %w", err)
	}
	hits := make([]SearchHit, 0, 8)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		c, err := s.readLocked(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue // one corrupt file must not block the whole search
		}
		hit, ok := matchConversation(c, terms)
		if !ok {
			continue
		}
		hit.SpaceID = c.SpaceID
		if hit.SpaceID == "" {
			hit.SpaceID = DefaultSpaceID
		}
		hits = append(hits, hit)
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Count != hits[j].Count {
			return hits[i].Count > hits[j].Count
		}
		if !hits[i].UpdatedAt.Equal(hits[j].UpdatedAt) {
			return hits[i].UpdatedAt.After(hits[j].UpdatedAt)
		}
		return hits[i].ID < hits[j].ID
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// matchConversation scores one conversation; ok=false means not every term
// matched. The AND gate is decided on the merged corpus of title + all
// messages first -- scoring per source would let a conversation whose title
// contains term A (with body mentioning only A) slip through, quietly
// turning AND into OR.
func matchConversation(c *Conversation, terms []string) (SearchHit, bool) {
	corpus := strings.ToLower(c.Title)
	for _, m := range c.Messages {
		corpus += "\n" + strings.ToLower(m.Content)
	}
	for _, t := range terms {
		if !strings.Contains(corpus, t) {
			return SearchHit{}, false
		}
	}

	hit := SearchHit{ID: c.ID, Title: c.Title, UpdatedAt: c.UpdatedAt}
	titleLower := strings.ToLower(c.Title)
	// Title hits also count toward the score: the title is the strongest
	// signal, so it counts double.
	titleHits := 0
	for _, t := range terms {
		if strings.Contains(titleLower, t) {
			titleHits += strings.Count(titleLower, t)
		}
	}
	if titleHits > 0 {
		hit.Count += 2 * titleHits
		hit.Excerpts = append(hit.Excerpts, "标题："+c.Title)
	}
	for i, m := range c.Messages {
		lower := strings.ToLower(m.Content)
		matched := true
		n := 0
		for _, t := range terms {
			if !strings.Contains(lower, t) {
				matched = false
				break
			}
			n += strings.Count(lower, t)
		}
		if !matched {
			continue
		}
		hit.Count += n
		hit.MatchedMessages = append(hit.MatchedMessages, i)
		if len(hit.Excerpts) < maxExcerpts {
			hit.Excerpts = append(hit.Excerpts,
				fmt.Sprintf("消息#%d（%s）：%s", i, m.Role, excerpt(m.Content, terms)))
		}
	}
	if len(hit.MatchedMessages) == 0 && titleHits == 0 {
		return SearchHit{}, false
	}
	return hit, true
}

// excerpt takes a window around the first matched term instead of dumping
// the whole message. The window is always cut on runes: cutting on bytes
// shreds multi-byte characters into mojibake.
func excerpt(content string, terms []string) string {
	lr := []rune(strings.ToLower(content))
	best := -1
	for _, t := range terms {
		if i := indexOfRune(lr, []rune(t)); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	if best < 0 {
		return truncate(stripNewlines(content), 2*excerptRadius)
	}
	start := best - excerptRadius
	if start < 0 {
		start = 0
	}
	end := best + excerptRadius
	if end > len(lr) {
		end = len(lr)
	}
	out := lr[start:end]
	if start > 0 {
		out = append([]rune("…"), out...)
	}
	if end < len(lr) {
		out = append(out, '…')
	}
	return strings.TrimSpace(stripNewlines(string(out)))
}

// indexOfRune returns the rune index of the first occurrence of needle in
// haystack, or -1. A plain implementation is enough here: it only runs on
// the small texts of matched excerpts.
func indexOfRune(haystack, needle []rune) int {
	n := len(needle)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(haystack); i++ {
		match := true
		for j := 0; j < n; j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
