package conversation

import (
	"strings"
	"time"
)

const RetryWindow = 10 * time.Minute

const SimThreshold = 0.45

func SimilarGoal(a, b string) bool {
	na, nb := normalizeGoal(a), normalizeGoal(b)
	if na == "" || nb == "" {
		return false
	}
	if na == nb {
		return true
	}
	as, bs := bigrams(na), bigrams(nb)
	if len(as) == 0 || len(bs) == 0 {
		return false
	}
	inter := 0
	for g := range as {
		if bs[g] {
			inter++
		}
	}
	union := len(as) + len(bs) - inter
	return float64(inter)/float64(union) >= SimThreshold
}

func normalizeGoal(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			continue
		case strings.ContainsRune("，。、！？；：\"'（）()【】[],.!?;:", r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func bigrams(s string) map[string]bool {
	out := map[string]bool{}
	var runes []rune
	flushLatin := func(word string) {
		if word != "" {
			out[word] = true
		}
	}
	var latin strings.Builder
	for _, r := range s {
		switch {
		case isASCIILetterOrDigit(r):
			latin.WriteRune(r)
			runes = runes[:0]
		default:
			flushLatin(latin.String())
			latin.Reset()
			runes = append(runes, r)
		}
	}
	flushLatin(latin.String())
	for i := 0; i+1 < len(runes); i++ {
		out[string(runes[i:i+2])] = true
	}
	return out
}

func isASCIILetterOrDigit(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
}

func (s *Store) DetectRetry(id, goal string) bool {
	p, err := s.path(id)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.readLocked(p)
	if err != nil {
		return false
	}
	for i := len(c.Messages) - 1; i >= 0; i-- {
		m := c.Messages[i]
		if m.Role != "user" {
			continue
		}
		return time.Since(m.CreatedAt) <= RetryWindow && SimilarGoal(m.Content, goal)
	}
	return false
}
