package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// streamServer serves a fixed list of SSE delta chunks then [DONE].
func streamServer(t *testing.T, chunks []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
}

// eval A3: reasoning_content is internal chain-of-thought and must not leak
// into the returned text (answers, skill output, plan JSON). The live deltas
// already excluded it; the returned string must match.
func TestChatStream_ExcludesReasoningFromResult(t *testing.T) {
	withShortWatchdog(t, 2*time.Second, 2*time.Second)
	srv := streamServer(t, []string{
		`{"choices":[{"delta":{"reasoning_content":"让我想想"}}]}`,
		`{"choices":[{"delta":{"reasoning_content":"逐步推理"}}]}`,
		`{"choices":[{"delta":{"content":"{\"steps\":[]}"}}]}`,
	})
	defer srv.Close()
	c := NewGLM(srv.URL, "key", "m", 0.3, 128, 5*time.Second)

	var deltas strings.Builder
	got, err := c.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(d string) { deltas.WriteString(d) })
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"steps":[]}` {
		t.Errorf("返回值应只含 content，实际 %q", got)
	}
	if strings.Contains(got, "让我想想") || strings.Contains(got, "逐步推理") {
		t.Errorf("reasoning 不应混入结果: %q", got)
	}
	if deltas.String() != `{"steps":[]}` {
		t.Errorf("onDelta 应只收到 content，实际 %q", deltas.String())
	}
}

// A thinking model that spent its whole budget on reasoning produces no
// deliverable: that must read as an empty response, not as reasoning prose
// masquerading as the answer.
func TestChatStream_ReasoningOnlyIsEmpty(t *testing.T) {
	withShortWatchdog(t, 2*time.Second, 2*time.Second)
	srv := streamServer(t, []string{
		`{"choices":[{"delta":{"reasoning_content":"只有思考没有正文"}}]}`,
	})
	defer srv.Close()
	c := NewGLM(srv.URL, "key", "m", 0.3, 128, 5*time.Second)

	_, err := c.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "为空") {
		t.Fatalf("只有 reasoning 时应报空响应，实际 %v", err)
	}
}
