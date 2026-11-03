package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gleam/pkg/types"
)

func TestMain(m *testing.M) {
	if os.Getenv("GLEAM_MCP_HELPER") == "1" {
		runFakeServer()
		return
	}
	os.Exit(m.Run())
}

func TestHelperProcess(_ *testing.T) {
	if os.Getenv("GLEAM_MCP_HELPER") != "1" {
		return
	}
	runFakeServer()
	os.Exit(0)
}

func runFakeServer() {
	scanner := bufio.NewScanner(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	var mu sync.Mutex
	write := func(v any) {
		b, _ := json.Marshal(v)
		mu.Lock()
		out.Write(append(b, '\n'))
		out.Flush()
		mu.Unlock()
	}
	for scanner.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil || req.Method == "" {
			continue
		}
		switch req.Method {
		case "initialize":
			write(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "fake", "version": "1"},
			}})
		case "notifications/initialized":
		case "tools/list":
			write(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"tools": []map[string]any{
				{"name": "echo", "description": "回显", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"}}},
				{"name": "add", "description": "求和", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "number"}, "b": map[string]any{"type": "number"}}}},
			}}})
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			var text string
			switch p.Name {
			case "echo":
				var a struct {
					Text string `json:"text"`
				}
				_ = json.Unmarshal(p.Arguments, &a)
				text = "echo: " + a.Text
			case "add":
				var a struct {
					A float64 `json:"a"`
					B float64 `json:"b"`
				}
				_ = json.Unmarshal(p.Arguments, &a)
				text = "sum: " + strconv.FormatFloat(a.A+a.B, 'f', -1, 64)
			}
			write(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": text}},
			}})
		default:
			write(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32601, "message": "method not found"}})
		}
	}
}

func startHelper(t *testing.T) *Client {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "-test.v=false")
	cmd.Env = append(os.Environ(), "GLEAM_MCP_HELPER=1")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.ReadAll(stderr) }()
	go func() { _ = cmd.Wait() }()

	t.Cleanup(func() {
		stdin.Close()
		p := cmd.Process
		if p != nil {
			_ = p.Kill()
		}
	})

	tp := &stdioTransport{
		name:    "fake",
		stdin:   stdin,
		stdout:  bufio.NewReaderSize(stdout, 1024*1024),
		pending: map[int64]chan rpcMessage{},
		logf:    func(string, ...any) {},
	}
	go tp.readLoop()
	return &Client{Name: "fake", tr: tp, logf: func(string, ...any) {}}
}

func TestMCPClient_InitializeAndList(t *testing.T) {
	c := startHelper(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var initResult map[string]any
	if err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "gleam-test", "version": "0"},
	}, &initResult); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	c.notify("notifications/initialized", nil)

	var list struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", map[string]any{}, &list); err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(list.Tools) != 2 || list.Tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", list.Tools)
	}
}

func TestMCPClient_CallTool(t *testing.T) {
	c := startHelper(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var _ = protocolVersion
	_ = c.call(ctx, "initialize", map[string]any{"protocolVersion": protocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}}, nil)
	c.notify("notifications/initialized", nil)

	out, err := c.CallTool(ctx, "echo", map[string]any{"text": "你好"})
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	if m, ok := out.(map[string]any); !ok || !strings.Contains(m["text"].(string), "echo: 你好") {
		t.Errorf("echo 输出 = %v", out)
	}
	out, err = c.CallTool(ctx, "add", map[string]any{"a": 2.5, "b": 1.0})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if m := out.(map[string]any); !strings.Contains(m["text"].(string), "sum: 3.5") {
		t.Errorf("add 输出 = %v", out)
	}
}

func TestMCP_AdapterTool(t *testing.T) {
	c := startHelper(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = c.call(ctx, "initialize", map[string]any{"protocolVersion": protocolVersion}, nil)
	c.notify("notifications/initialized", nil)

	tool := NewTool(c, ToolDef{Name: "echo", Description: "回显工具"}, types.PermissionReadOnly)
	if tool.Name() != "mcp.fake.echo" {
		t.Errorf("adapter name = %s", tool.Name())
	}
	if tool.Permission() != types.PermissionReadOnly {
		t.Error("权限透传失败")
	}
	out, err := tool.Execute(ctx, map[string]any{"text": "x"})
	if err != nil || !strings.Contains(fmt.Sprintf("%v", out), "echo: x") {
		t.Errorf("adapter execute = %v %v", out, err)
	}
}

type blockingWriteCloser struct {
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func newBlockingWriteCloser() *blockingWriteCloser {
	return &blockingWriteCloser{started: make(chan struct{}), closed: make(chan struct{})}
}

func (w *blockingWriteCloser) Write(_ []byte) (int, error) {
	w.startOnce.Do(func() { close(w.started) })
	<-w.closed
	return 0, io.ErrClosedPipe
}

func (w *blockingWriteCloser) Close() error {
	w.closeOnce.Do(func() { close(w.closed) })
	return nil
}

func blockingTransport(writer io.WriteCloser) *stdioTransport {
	return &stdioTransport{
		name: "blocking", stdin: writer, pending: map[int64]chan rpcMessage{},
		logf: func(string, ...any) {},
	}
}

func TestStdioTransportCloseUnblocksWrite(t *testing.T) {
	writer := newBlockingWriteCloser()
	t.Cleanup(func() { _ = writer.Close() })
	tp := blockingTransport(writer)
	writeDone := make(chan error, 1)
	go func() { writeDone <- tp.notify("blocked", nil) }()
	select {
	case <-writer.started:
	case <-time.After(2 * time.Second):
		t.Fatal("write did not start")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- tp.close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("close blocked behind stdin.Write")
	}
	select {
	case <-writeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("closing stdin did not release Write")
	}
}

func TestStdioTransportCallCancelDuringBlockedWrite(t *testing.T) {
	writer := newBlockingWriteCloser()
	t.Cleanup(func() { _ = writer.Close() })
	tp := blockingTransport(writer)
	ctx, cancel := context.WithCancel(context.Background())
	callDone := make(chan error, 1)
	go func() { callDone <- tp.call(ctx, "blocked", nil, nil) }()
	select {
	case <-writer.started:
	case <-time.After(2 * time.Second):
		t.Fatal("write did not start")
	}

	cancel()
	select {
	case err := <-callDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("call error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call cancellation blocked behind stdin.Write")
	}
	if err := tp.close(); err != nil {
		t.Fatal(err)
	}
}

func TestPermissionFromTrust(t *testing.T) {
	cases := map[string]types.Permission{
		"readonly":      types.PermissionReadOnly,
		"read_only":     types.PermissionReadOnly,
		"full_access":   types.PermissionFullAccess,
		"":              types.PermissionUserApproved,
		"user_approved": types.PermissionUserApproved,
	}
	for in, want := range cases {
		if got := permissionFromTrust(in); got != want {
			t.Errorf("permissionFromTrust(%q) = %v", in, got)
		}
	}
}
