package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fakeRemote(t *testing.T, sse bool) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		auth := r.Header.Get("Authorization")
		seen = append(seen, req.Method+" auth="+auth)
		if len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-123")
			result = map[string]any{"protocolVersion": protocolVersion, "serverInfo": map[string]any{"name": "fake"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{
				{"name": "echo", "description": "回声", "inputSchema": map[string]any{"type": "object"}},
			}}
		default:
			result = map[string]any{}
		}
		payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		if !sse {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(payload)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestRemoteMCPHandshakeOverJSON(t *testing.T) {
	srv, seen := fakeRemote(t, false)

	var egHosts []string
	SetRemoteEgressHook(func(host string, nbytes int) { egHosts = append(egHosts, host) })
	defer SetRemoteEgressHook(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Start(ctx, ServerConfig{
		Name: "fake-remote", URL: srv.URL,
		Headers: map[string]string{"Authorization": "Bearer t0ken"},
	}, nil)
	if err != nil {
		t.Fatalf("远端握手失败：%v", err)
	}
	defer c.Close()

	if len(c.Tools) != 1 || c.Tools[0].Name != "echo" {
		t.Fatalf("工具列表不对：%+v", c.Tools)
	}
	if c.Info == nil || c.Info["serverInfo"] == nil {
		t.Errorf("initialize 的返回没接住：%v", c.Info)
	}
	if len(egHosts) == 0 {
		t.Fatal("远端请求没有走出网留痕：台账上那一行会永远显示\"没有记录\"")
	}
	for _, s := range *seen {
		if !strings.Contains(s, "auth=Bearer t0ken") {
			t.Errorf("请求头没带上凭据：%q", s)
		}
	}
	for _, h := range egHosts {
		if strings.Contains(h, "t0ken") {
			t.Errorf("留痕里混进了凭据：%q", h)
		}
	}
}

func TestRemoteMCPSessionIDIsEchoed(t *testing.T) {
	var gotSession []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSession = append(gotSession, r.Header.Get("Mcp-Session-Id"))
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "sess-xyz")
		}
		result := map[string]any{}
		if req.Method == "tools/list" {
			result = map[string]any{"tools": []map[string]any{}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Start(ctx, ServerConfig{Name: "s", URL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if n := len(gotSession); n < 2 || gotSession[n-1] != "sess-xyz" {
		t.Errorf("后续请求没带会话 ID：%v", gotSession)
	}
}

func TestRemoteMCPEventStreamResponse(t *testing.T) {
	srv, _ := fakeRemote(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Start(ctx, ServerConfig{Name: "sse-remote", URL: srv.URL}, nil)
	if err != nil {
		t.Fatalf("事件流响应解析失败：%v", err)
	}
	defer c.Close()
	if len(c.Tools) != 1 {
		t.Errorf("事件流那条路上的工具列表不对：%+v", c.Tools)
	}
}

func TestRemoteMCPRejectsPlainHTTP(t *testing.T) {
	_, err := Start(context.Background(), ServerConfig{Name: "x", URL: "http://example.com/mcp"}, nil)
	if err == nil {
		t.Fatal("明文 http 的远端地址应被拒绝")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Errorf("拒绝理由应点明 https：%v", err)
	}
}

func TestRemoteMCPRecordsEgressOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Authorization: Bearer leaked-token"}`))
	}))
	defer srv.Close()

	var hosts []string
	SetRemoteEgressHook(func(host string, nbytes int) { hosts = append(hosts, host) })
	defer SetRemoteEgressHook(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := Start(ctx, ServerConfig{Name: "401", URL: srv.URL}, nil)
	if err == nil {
		t.Fatal("401 应当失败")
	}
	if len(hosts) == 0 {
		t.Fatal("请求已经发出去了（401 也是发出去了），必须留痕")
	}
	if strings.Contains(err.Error(), "leaked-token") {
		t.Errorf("错误文案里不该回显凭据：%v", err)
	}
}
