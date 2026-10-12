package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEgressHookFiresOnChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	var host string
	var nbytes int
	SetEgressHook(func(h string, n int) { host, nbytes = h, n })
	defer SetEgressHook(nil)

	c := NewGLM(srv.URL, "k", "m", 0.3, 100, 5*time.Second)
	if _, err := c.Chat(context.Background(), ChatRequest{
		System:   "s",
		Messages: []Message{{Role: RoleUser, Content: "机密内容-不该出现在留痕里"}},
	}); err != nil {
		t.Fatalf("Chat 应成功: %v", err)
	}

	if host == "" {
		t.Fatal("模型调用发生了却没有出网留痕")
	}
	if !strings.Contains(host, "127.0.0.1") {
		t.Errorf("留痕应记下目标主机，实际 %q", host)
	}
	if nbytes == 0 {
		t.Error("留痕应记下请求字节数")
	}
	if strings.Contains(host, "机密内容") {
		t.Errorf("留痕不该包含内容：%q", host)
	}
}

func TestEgressHookRecordsStreamingToo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	hits := 0
	SetEgressHook(func(string, int) { hits++ })
	defer SetEgressHook(nil)

	c := NewGLM(srv.URL, "k", "m", 0.3, 100, 5*time.Second)
	_, _ = c.ChatStream(context.Background(), ChatRequest{
		System: "s", Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, nil)

	if hits == 0 {
		t.Error("流式调用也必须留痕，否则对话模式是一条无痕出网路径")
	}
}

func TestEgressHookOffByDefault(t *testing.T) {
	SetEgressHook(nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := NewGLM(srv.URL, "k", "m", 0.3, 100, 5*time.Second)
	if _, err := c.Chat(context.Background(), ChatRequest{
		System: "s", Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatalf("没装钩子时也应正常调用: %v", err)
	}
}
