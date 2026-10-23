package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStreamingClientsHaveNoTotalHTTPTimeout(t *testing.T) {
	if c := NewGLM("http://x", "", "m", 0, 0, time.Second); c.HTTP.Timeout != 0 {
		t.Fatalf("GLM stream client timeout = %v, want 0", c.HTTP.Timeout)
	}
	if c := NewAnthropic("http://x", "", "m", 0, 0, time.Second); c.HTTP.Timeout != 0 {
		t.Fatalf("Anthropic stream client timeout = %v, want 0", c.HTTP.Timeout)
	}
	if c := NewResponses("http://x", "", "m", 0, 0, time.Second); c.HTTP.Timeout != 0 {
		t.Fatalf("Responses stream client timeout = %v, want 0", c.HTTP.Timeout)
	}
}

// A long generation must outlive the configured non-streaming timeout.
func TestChatStreamOutlivesRequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, chunk := range []string{"he", "llo", "!"} {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", chunk)
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(80 * time.Millisecond)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := NewGLM(srv.URL, "", "m", 0, 0, 50*time.Millisecond)
	got, err := c.ChatStream(context.Background(), ChatRequest{}, nil)
	if err != nil {
		t.Fatalf("stream aborted by total timeout: %v", err)
	}
	if got != "hello!" {
		t.Fatalf("stream text = %q", got)
	}
}

func TestChatNonStreamHonoursTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(400 * time.Millisecond):
		}
	}))
	defer srv.Close()

	c := NewGLM(srv.URL, "", "m", 0, 0, 80*time.Millisecond)
	start := time.Now()
	if _, err := c.Chat(context.Background(), ChatRequest{}); err == nil {
		t.Fatal("non-stream call should time out")
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("non-stream timeout not applied, took %v", elapsed)
	}
}
