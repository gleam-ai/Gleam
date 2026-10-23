package llm

import (
	"context"
	"strings"
	"sync"
	"unicode"
)

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	System      string
	Messages    []Message
	Temperature float64
	// TemperatureSet distinguishes an explicit 0 (deterministic output) from an
	// unset value that keeps the client default.
	TemperatureSet bool
	MaxTokens      int
	TaskID         string
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	Estimated        bool
}

func (u Usage) Total() int { return u.PromptTokens + u.CompletionTokens }

func (u Usage) CacheHitRate() float64 {
	if u.PromptTokens <= 0 || u.CachedTokens <= 0 {
		return 0
	}
	if u.CachedTokens > u.PromptTokens {
		return 1
	}
	return float64(u.CachedTokens) / float64(u.PromptTokens)
}

func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	cjk, other := 0, 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			cjk++
		} else if !unicode.IsSpace(r) {
			other++
		}
	}
	return cjk + (other+3)/4
}

type usageHook func(taskID, kind string, req ChatRequest, text string, u Usage)

var (
	usageMu    sync.RWMutex
	usageSinks []usageHook
)

func AddUsageHook(h func(taskID, kind string, req ChatRequest, text string, u Usage)) {
	if h == nil {
		return
	}
	usageMu.Lock()
	usageSinks = append(usageSinks, h)
	usageMu.Unlock()
}

func ReportUsage(req ChatRequest, text string, u Usage) {
	if u.PromptTokens == 0 && u.CompletionTokens == 0 {
		var b strings.Builder
		b.WriteString(req.System)
		for _, m := range req.Messages {
			b.WriteString(m.Content)
		}
		u = Usage{
			PromptTokens:     EstimateTokens(b.String()),
			CompletionTokens: EstimateTokens(text),
			Estimated:        true,
		}
	}
	usageMu.RLock()
	sinks := usageSinks
	usageMu.RUnlock()
	kind := KindOf(req.System)
	for _, h := range sinks {
		h(req.TaskID, kind, req, text, u)
	}
}

type Client interface {
	Chat(ctx context.Context, req ChatRequest) (string, error)
	ChatStream(ctx context.Context, req ChatRequest, onDelta func(delta string)) (string, error)
	Name() string
}

const (
	MarkerPlan     = "[GLEAM-TASK:PLAN]"
	MarkerReflect  = "[GLEAM-TASK:REFLECT]"
	MarkerCompress = "[GLEAM-TASK:COMPRESS]"
	MarkerSkillFix = "[GLEAM-TASK:SKILL_FIX]"
	MarkerChat     = "[GLEAM-TASK:CHAT]"
	MarkerGEO      = "[GLEAM-TASK:GEO]"
	MarkerToolPick = "[GLEAM-TASK:TOOL_PICK]"
	MarkerReview   = "[GLEAM-TASK:REVIEW]"
	MarkerChatChk  = "[GLEAM-TASK:CHAT_CHECK]"
)

func KindOf(system string) string {
	switch {
	case contains(system, MarkerPlan):
		return "plan"
	case contains(system, MarkerReflect):
		return "reflect"
	case contains(system, MarkerCompress):
		return "compress"
	case contains(system, MarkerSkillFix):
		return "skill_fix"
	case contains(system, MarkerGEO):
		return "geo"
	case contains(system, MarkerToolPick):
		return "tool_pick"
	case contains(system, MarkerReview):
		return "review"
	case contains(system, MarkerChatChk):
		return "chat_check"
	case contains(system, MarkerChat):
		return "chat_mode"
	default:
		return "chat"
	}
}

func IsMockClient(c Client) bool {
	_, ok := c.(*MockClient)
	return ok
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
