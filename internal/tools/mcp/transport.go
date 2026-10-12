package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type transport interface {
	call(ctx context.Context, method string, params any, out any) error
	notify(method string, params any) error
	close() error
}

type stdioTransport struct {
	name   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr io.ReadCloser
	logf   func(string, ...any)

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcMessage
	closed  bool
}

func newStdioTransport(cfg ServerConfig, logf func(string, ...any)) (*stdioTransport, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	if len(cfg.Env) > 0 {
		env := os.Environ()
		for k, v := range cfg.Env {
			if strings.TrimSpace(k) != "" {
				env = append(env, k+"="+v)
			}
		}
		cmd.Env = env
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: 启动服务器 %q 失败: %w", cfg.Name, err)
	}
	t := &stdioTransport{
		name: cfg.Name, cmd: cmd, stdin: stdin,
		stdout: bufio.NewReaderSize(stdout, 1024*1024), stderr: stderr,
		pending: map[int64]chan rpcMessage{}, logf: logf,
	}
	if stderr != nil {
		go func() {
			buf := make([]byte, 4096)
			for {
				if _, err := stderr.Read(buf); err != nil {
					return
				}
			}
		}()
	}
	go func() { _ = cmd.Wait() }()
	go t.readLoop()
	return t, nil
}

func (t *stdioTransport) readLoop() {
	for {
		line, err := t.stdout.ReadString('\n')
		if err != nil {
			t.failPending(fmt.Errorf("mcp: %s 连接关闭", t.name))
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if len(msg.ID) > 0 {
			var id int64
			if err := json.Unmarshal(msg.ID, &id); err == nil {
				t.mu.Lock()
				ch := t.pending[id]
				delete(t.pending, id)
				t.mu.Unlock()
				if ch != nil {
					ch <- msg
				}
				continue
			}
		}
		if msg.Method != "" {
			t.logf("mcp[%s] 收到 %s", t.name, msg.Method)
		}
	}
}

func (t *stdioTransport) failPending(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	for id, ch := range t.pending {
		ch <- rpcMessage{Error: &rpcError{Code: -32000, Message: err.Error()}}
		delete(t.pending, id)
	}
}

func (t *stdioTransport) send(msg rpcMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return fmt.Errorf("mcp: %s 已关闭", t.name)
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = t.stdin.Write(append(b, '\n'))
	return err
}

func (t *stdioTransport) notify(method string, params any) error {
	return t.send(rpcMessage{JSONRPC: "2.0", Method: method, Params: paramsRaw(params)})
}

func (t *stdioTransport) call(ctx context.Context, method string, params any, out any) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("mcp: %s 已关闭", t.name)
	}
	t.nextID++
	id := t.nextID
	ch := make(chan rpcMessage, 1)
	t.pending[id] = ch
	t.mu.Unlock()

	idRaw, _ := json.Marshal(id)
	if err := t.send(rpcMessage{JSONRPC: "2.0", ID: idRaw, Method: method, Params: paramsRaw(params)}); err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return err
	}
	select {
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return ctx.Err()
	case msg := <-ch:
		return decodeResult(t.name, method, msg, out)
	}
}

func decodeResult(name, method string, msg rpcMessage, out any) error {
	if msg.Error != nil {
		return fmt.Errorf("mcp: %s %s 错误(%d): %s", name, method, msg.Error.Code, msg.Error.Message)
	}
	if out != nil && len(msg.Result) > 0 {
		return json.Unmarshal(msg.Result, out)
	}
	return nil
}

func (t *stdioTransport) close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()
	_ = t.stdin.Close()
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	return nil
}

func paramsRaw(p any) json.RawMessage {
	if p == nil {
		return nil
	}
	b, _ := json.Marshal(p)
	return b
}
