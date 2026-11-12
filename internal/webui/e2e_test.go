//go:build e2e

package webui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gleam/internal/agent"
	"gleam/internal/config"
)

// TestConversationFlow tests the complete conversation input/output lifecycle.
// This is an end-to-end integration test simulating real user interaction.
func TestConversationFlow(t *testing.T) {
	// Setup
	a := setupTestAgent(t)
	router := New(a)

	// Create a test conversation
	convo, err := a.Convos.Create()
	if err != nil {
		t.Fatalf("Create conversation: %v", err)
	}

	// Simulate user sending a message
	userMsg := agent.Message{
		Role:      "user",
		Content:   "Hello, help me with code",
		CreatedAt: time.Now(),
	}
	if _, err := a.AppendConvo(convo.ID, userMsg); err != nil {
		t.Fatalf("Append user message: %v", err)
	}

	// Simulate assistant reply
	assistantMsg := agent.Message{
		Role:      "assistant",
		Content:   "Sure! What would you like to build?",
		CreatedAt: time.Now(),
		Status:    "success",
		TaskID:    "test-task-123",
	}
	if _, err := a.AppendConvo(convo.ID, assistantMsg); err != nil {
		t.Fatalf("Append assistant message: %v", err)
	}

	// Verify conversation retrieval
	got, err := a.Convos.Get(convo.ID)
	if err != nil {
		t.Fatalf("Get conversation: %v", err)
	}

	if len(got.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(got.Messages))
	}

	// Verify message roles
	if got.Messages[0].Role != "user" || got.Messages[1].Role != "assistant" {
		t.Error("Message roles mismatch")
	}
}

// TestStreamOutput tests SSE streaming output similar to Qoder's behavior.
func TestStreamOutput(t *testing.T) {
	a := setupTestAgent(t)
	router := New(a)

	// Create test handler for SSE endpoint
	handler := func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "SSE not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // nginx bypass buffer

		// Stream multiple events like real LLM response
		events := []map[string]interface{}{
			{"type": "start"},
			{"type": "chunk", "data": "Here"},
			{"type": "chunk", "data": " is a"},
			{"type": "chunk", "data": " streamed"},
			{"type": "chunk", "data": " response"},
			{"type": "end"},
		}

		for i, event := range events {
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			time.Sleep(50 * time.Millisecond) // Simulate network latency

			// Check if client disconnected
			if i < len(events)-1 && r.Context().Err() != nil {
				t.Log("Client disconnected early")
				return
			}
		}
	}

	// Test SSE connection
	ts := httptest.NewServer(http.HandlerFunc(handler))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("HTTP GET: %v", err)
	}
	defer resp.Body.Close()

	// Read and verify stream
	reader := bufio.NewReader(resp.Body)
	var received []string

	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		}
		if strings.HasPrefix(line, "data: ") {
			received = append(received, line[6:len(line)-2]) // remove "data: " and "\n"
		}
	}

	if len(received) != 5 {
		t.Errorf("Expected 5 events, received %d", len(received))
	}

	expected := []string{"start", "chunk", "chunk", "chunk", "end"}
	for i, exp := range expected {
		if received[i] != exp {
			t.Errorf("Event %d: got %q, want %q", i, received[i], exp)
		}
	}
}

// TestSearchPerformance tests full-text search performance optimization.
func TestSearchPerformance(t *testing.T) {
	a := setupTestAgent(t)

	// Create 100 conversations for performance testing
	convoIDs := make([]string, 100)
	for i := 0; i < 100; i++ {
		c, err := a.Convos.Create()
		if err != nil {
			t.Fatalf("Create conversation %d: %v", i, err)
		}
		convoIDs[i] = c.ID

		// Add search-worthy content
		a.AppendConvo(c.ID, agent.Message{
			Role:      "user",
			Content:   fmt.Sprintf("Task %d: analyze data", i),
			CreatedAt: time.Now(),
		})
		a.AppendConvo(c.ID, agent.Message{
			Role:      "assistant",
			Content:   fmt.Sprintf("I will analyze the data in task %d", i),
			CreatedAt: time.Now(),
		})
	}

	// Performance test: search should be fast even with many conversations
	start := time.Now()
	hits, err := a.SessionSearch("data", 10)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(hits) == 0 {
		t.Error("Search should find conversations containing 'data'")
	}

	// Should complete in under 100ms with in-memory index
	if elapsed > 100*time.Millisecond {
		t.Warnf("Search took %v, expected <100ms", elapsed)
	} else {
		t.Logf("Search completed in %v ✅", elapsed)
	}
}

// TestEmojiTruncation tests Unicode-safe title truncation with emojis.
func TestEmojiTruncation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		valid bool
	}{
		{"empty_emoji", "", 10, true},
		{"single_emoji", "😀", 1, false}, // will add ellipsis
		{"complex_family", "Hello 👨‍👩‍👧‍👦 World", 8, false},
		{"mixed_text", "中文+Emoji 🌐测试", 6, false},
		{"exact_fit", "abcdefg", 7, true}, // exact length, no truncation
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := setupTestAgent(t)
			c, _ := a.Convos.Create()

			msg := agent.Message{
				Role:      "user",
				Content:   tt.input,
				CreatedAt: time.Now(),
			}
			a.AppendConvo(c.ID, msg)

			got, _ := a.Convos.Get(c.ID)
			title := got.Title

			// Verify length constraint
			r := []rune(title)
			if len(r) > tt.max+1 {
				t.Errorf("Title length=%d exceeds limit %d+1: %q", len(r), tt.max, title)
			}

			// If input longer than max, should have ellipsis
			if len([]rune(tt.input)) > tt.max && !strings.HasSuffix(title, "…") {
				t.Errorf("Missing ellipsis: %q", title)
			}

			// No double ellipsis
			if strings.Count(title, "…") > 1 {
				t.Errorf("Duplicate ellipsis: %q", title)
			}
		})
	}
}

// Helper functions
func setupTestAgent(t *testing.T) *agent.Agent {
	cfg := &config.Config{
		DataDir: t.TempDir(),
	}

	// Minimal agent setup for tests
	a := &agent.Agent{
		Cfg: cfg,
	}

	// Initialize conversation store
	if err := a.InitConvos(); err != nil {
		t.Fatalf("InitConvos: %v", err)
	}

	return a
}
