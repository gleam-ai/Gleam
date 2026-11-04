// Package lsp provides read-only code intelligence tools: definition / references / hover.
//
// Design:
//   - The LSP server subprocess starts lazily, on first call.
//   - One server instance per workspace root.
//   - Only gopls (Go) is supported; other language servers are left as future extension points.
//   - Calls go over JSON-RPC on stdio, similar to the MCP transport but covering only an LSP subset.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

// server is an LSP server subprocess plus a stdio JSON-RPC channel.
type server struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	seq    int
	// After start the server must complete initialize + initialized before requests are accepted.
	initialized bool
}

// Manager maintains the workspace -> server mapping.
type Manager struct {
	mu      sync.Mutex
	servers map[string]*server
}

func NewManager() *Manager { return &Manager{servers: map[string]*server{}} }

// ensure starts and initializes gopls for the given workspace.
func (m *Manager) ensure(ctx context.Context, workspace string) (*server, error) {
	m.mu.Lock()
	s, ok := m.servers[workspace]
	m.mu.Unlock()
	if ok {
		return s, nil
	}

	gopls, err := exec.LookPath("gopls")
	if err != nil {
		return nil, fmt.Errorf("未找到 gopls，请先安装：go install golang.org/x/tools/gopls@latest")
	}
	cmd := exec.Command(gopls, "serve")
	cmd.Dir = workspace
	cmd.Env = os.Environ()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 gopls 失败: %w", err)
	}
	s = &server{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdoutPipe)}
	if err := s.initialize(ctx, workspace); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	m.mu.Lock()
	m.servers[workspace] = s
	m.mu.Unlock()
	return s, nil
}

func (s *server) initialize(ctx context.Context, workspace string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized {
		return nil
	}
	rootURI := pathToURI(workspace)
	_, err := s.call(ctx, "initialize", map[string]any{
		"processId": os.Getpid(),
		"rootUri":   rootURI,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"definition":     map[string]any{},
				"references":     map[string]any{},
				"hover":          map[string]any{"contentFormat": []string{"plaintext"}},
				"documentSymbol": map[string]any{},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("LSP initialize: %w", err)
	}
	if err := s.notify("initialized", map[string]any{}); err != nil {
		return fmt.Errorf("LSP initialized: %w", err)
	}
	s.initialized = true
	return nil
}

// call sends one JSON-RPC request and waits for the response. Holding s.mu keeps request-response pairing strictly serial.
func (s *server) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.seq++
	id := s.seq
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	if err := s.write(req); err != nil {
		return nil, err
	}
	// LSP uses Content-Length framing; read header -> body.
	return s.readResponse(ctx, id)
}

func (s *server) notify(method string, params any) error {
	return s.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (s *server) write(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(s.stdin, header); err != nil {
		return err
	}
	_, err = s.stdin.Write(body)
	return err
}

// readResponse skips notifications until the response with the matching id arrives.
func (s *server) readResponse(ctx context.Context, wantID int) (json.RawMessage, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(15 * time.Second)
	}
	for time.Now().Before(deadline) {
		// Read the header
		length := -1
		for {
			line, err := s.stdout.ReadString('\n')
			if err != nil {
				return nil, err
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if strings.HasPrefix(strings.ToLower(line), "content-length:") {
				v := strings.TrimSpace(line[len("content-length:"):])
				n, _ := strconv.Atoi(v)
				length = n
			}
		}
		if length <= 0 {
			continue
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(s.stdout, body); err != nil {
			return nil, err
		}
		var msg struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		// Server -> client notifications/requests; ignore them.
		if msg.Method != "" {
			continue
		}
		// Match the id (JSON numbers may decode as float64)
		match := false
		switch v := msg.ID.(type) {
		case float64:
			match = int(v) == wantID
		case int:
			match = v == wantID
		}
		if !match {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("LSP error %d: %s", msg.Error.Code, msg.Error.Message)
		}
		return msg.Result, nil
	}
	return nil, fmt.Errorf("LSP 响应超时")
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.servers {
		_ = s.stdin.Close()
		_ = s.cmd.Process.Kill()
	}
	m.servers = map[string]*server{}
}

// Toolset

// DefinitionTool exposes lsp.definition.
type DefinitionTool struct{ M *Manager }

func NewDefinition(m *Manager) types.Tool { return &DefinitionTool{m} }
func (t *DefinitionTool) Name() string    { return "lsp.definition" }
func (t *DefinitionTool) Description() string {
	return "跳转到符号定义（Go 文件，需要已安装 gopls）。返回文件路径与行列号。"
}
func (t *DefinitionTool) Permission() types.Permission { return types.PermissionReadOnly }
func (t *DefinitionTool) Schema() map[string]any       { return lspSchema("跳转到定义") }
func (t *DefinitionTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return lspQuery(ctx, t.M, args, "textDocument/definition")
}

// ReferencesTool exposes lsp.references.
type ReferencesTool struct{ M *Manager }

func NewReferences(m *Manager) types.Tool { return &ReferencesTool{m} }
func (t *ReferencesTool) Name() string    { return "lsp.references" }
func (t *ReferencesTool) Description() string {
	return "查找符号的所有引用（Go 文件，需要 gopls）。返回位置列表。"
}
func (t *ReferencesTool) Permission() types.Permission { return types.PermissionReadOnly }
func (t *ReferencesTool) Schema() map[string]any       { return lspSchema("查找引用") }
func (t *ReferencesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return lspQuery(ctx, t.M, args, "textDocument/references")
}

// HoverTool exposes lsp.hover.
type HoverTool struct{ M *Manager }

func NewHover(m *Manager) types.Tool { return &HoverTool{m} }
func (t *HoverTool) Name() string    { return "lsp.hover" }
func (t *HoverTool) Description() string {
	return "查看符号的类型签名/文档（Go 文件，需要 gopls）。"
}
func (t *HoverTool) Permission() types.Permission { return types.PermissionReadOnly }
func (t *HoverTool) Schema() map[string]any       { return lspSchema("查看 hover") }
func (t *HoverTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return lspQuery(ctx, t.M, args, "textDocument/hover")
}

func lspSchema(desc string) map[string]any {
	return toolutil.Schema(desc, []string{"file", "line", "column"}, map[string]any{
		"file":      toolutil.SchemaProp("Go 源文件路径（绝对路径）", "string"),
		"line":      toolutil.SchemaProp("行号（从 1 开始）", "integer"),
		"column":    toolutil.SchemaProp("列号（从 1 开始）", "integer"),
		"workspace": toolutil.SchemaProp("可选：工作区根目录，默认取文件所在目录", "string"),
	})
}

func lspQuery(ctx context.Context, m *Manager, args map[string]any, method string) (any, error) {
	file, err := toolutil.RequireStr(args, "file")
	if err != nil {
		return nil, err
	}
	line := toolutil.Int(args, "line", 0)
	col := toolutil.Int(args, "column", 0)
	if line <= 0 || col <= 0 {
		return nil, fmt.Errorf("line 与 column 必须从 1 开始")
	}
	ws := toolutil.Str(args, "workspace")
	if ws == "" {
		ws = filepath.Dir(file)
	}
	srv, err := m.ensure(ctx, ws)
	if err != nil {
		return nil, err
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	// didOpen: tell the server about this file, otherwise hover may come back empty.
	_ = srv.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        pathToURI(file),
			"languageId": "go",
			"version":    1,
			"text":       readFileOrEmpty(file),
		},
	})
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(file)},
		"position":     map[string]any{"line": line - 1, "character": col - 1},
	}
	if method == "textDocument/references" {
		params["context"] = map[string]any{"includeDeclaration": true}
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result, err := srv.call(cctx, method, params)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(result, &out); err != nil {
		return map[string]any{"raw": string(result)}, nil
	}
	return map[string]any{"result": out}, nil
}

func pathToURI(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	s := filepath.ToSlash(abs)
	// Windows drive-letter paths need a leading slash: C:/foo -> /C:/foo
	if len(s) >= 2 && s[1] == ':' {
		s = "/" + s
	}
	return "file://" + s
}

func readFileOrEmpty(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}
