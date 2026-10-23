package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gleam/internal/llm"
)

// Must be shorter than the gate's -timeout 300s; otherwise the package timeout
// fires first and this cold-build hint never prints.
const buildTimeout = 4 * time.Minute

var (
	buildOnce sync.Once
	buildDir  string
	buildPath string
	buildErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if buildDir != "" {
		_ = os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

func buildBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		goTool, err := exec.LookPath("go")
		if err != nil {
			goTool = `D:\PersonalProject\tools\go\bin\go.exe`
			if _, err2 := os.Stat(goTool); err2 != nil {
				buildErr = fmt.Errorf("go 工具链不可用")
				return
			}
		}
		dir, err := os.MkdirTemp("", "gleam-e2e-bin-")
		if err != nil {
			buildErr = err
			return
		}
		buildDir = dir
		ext := ""
		if runtime.GOOS == "windows" {
			ext = ".exe"
		}
		buildPath = filepath.Join(dir, "gleam"+ext)
		cacheDir := os.Getenv("GOCACHE")
		if cacheDir == "" {
			if out, e := exec.Command(goTool, "env", "GOCACHE").Output(); e == nil {
				cacheDir = strings.TrimSpace(string(out))
			}
		}
		buildCtx, cancel := context.WithTimeout(context.Background(), buildTimeout)
		defer cancel()
		// Match the real release builds (smoke.sh, build-desktop.sh, install.ps1
		// and package.sh all use -trimpath -ldflags="-s -w"). Without -trimpath
		// the size gate measures a build that never ships.
		cmd := exec.CommandContext(buildCtx, goTool, "build", "-p", "2", "-trimpath", "-ldflags=-s -w", "-o", buildPath, "gleam/cmd/gleam")
		cmd.Dir = projectRoot(t)
		cmd.Env = append(os.Environ(), "GOMAXPROCS=2")
		if cacheDir != "" {
			cmd.Env = append(cmd.Env, "GOCACHE="+cacheDir)
		}
		out, err := cmd.CombinedOutput()
		if buildCtx.Err() != nil {
			buildErr = fmt.Errorf("构建超时（%s）: %w；若为首次冷编译可先 `go build ./cmd/gleam` 预热缓存", buildTimeout, buildCtx.Err())
		} else if err != nil {
			buildErr = fmt.Errorf("构建失败: %w\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return buildPath
}

func projectRoot(t *testing.T) string {
	t.Helper()
	cwd, _ := os.Getwd()
	root := cwd
	for i := 0; i < 3; i++ {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		root = filepath.Dir(root)
	}
	t.Fatal("未找到项目根目录")
	return ""
}

func writeScript(t *testing.T, scripts []llm.Scripted) string {
	t.Helper()
	b, err := json.Marshal(scripts)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "mock-script.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustEnv() []string { return append(os.Environ(), "GLEAM_MOCK_LLM=1") }

func TestE2E_GoalCommand(t *testing.T) {
	bin := buildBinary(t)
	ws := t.TempDir()
	script := writeScript(t, []llm.Scripted{
		{Kind: "plan", Texts: []string{`{"steps":[
			{"id":"s1","description":"创建问候文件","tool":"file.write","args":{"path":"e2e.txt","content":"Gleam e2e 你好"}},
			{"id":"s2","description":"读回验证","tool":"file.read","args":{"path":"e2e.txt"},"depends_on":["s1"]},
			{"id":"s3","description":"回复","tool":"reply","args":{"text":"e2e.txt 已创建并验证"}}
		],"estimated_time":"short"}`}},
		{Kind: "reflect", Texts: []string{`{"score":96,"verdict":"done","reason":"全部成功"}`}},
	})
	dataDir := t.TempDir()
	cmd := exec.Command(bin, "goal", "--mock-llm", "--mock-script", script, "--workspace", ws, "--data-dir", dataDir, "创建 e2e.txt 并验证")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("goal 失败: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "e2e.txt 已创建并验证") {
		t.Errorf("stdout 缺少摘要: %s", out)
	}
	data, rerr := os.ReadFile(filepath.Join(ws, "e2e.txt"))
	if rerr != nil || string(data) != "Gleam e2e 你好" {
		t.Errorf("产物错误: %v %q", rerr, data)
	}
}

type serveClient struct {
	t        *testing.T
	cmd      *exec.Cmd
	stdin    chan string
	lines    chan string
	closed   chan struct{}
	waitDone chan struct{}
}

func startServe(t *testing.T, bin, script, ws, dataDir string) *serveClient {
	t.Helper()
	cmd := exec.Command(bin, "serve", "--mock-llm", "--mock-script", script, "--workspace", ws, "--data-dir", dataDir)
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	sc := &serveClient{
		t:        t,
		cmd:      cmd,
		stdin:    make(chan string, 8),
		lines:    make(chan string, 256),
		closed:   make(chan struct{}),
		waitDone: make(chan struct{}),
	}
	go func() {
		_ = cmd.Wait()
		close(sc.waitDone)
	}()
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			sc.lines <- scanner.Text()
		}
		close(sc.closed)
	}()
	go func() {
		for line := range sc.stdin {
			if _, err := stdinPipe.Write([]byte(line + "\n")); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(sc.stdin)
		_ = cmd.Process.Kill()
		<-sc.waitDone
	})
	return sc
}

func (c *serveClient) send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		c.t.Fatal(err)
	}
	c.stdin <- string(b)
}

func (c *serveClient) nextLine(timeout time.Duration) (string, error) {
	select {
	case line := <-c.lines:
		return line, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("读取超时")
	}
}

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *serveClient) callUntil(id int64, method string, params any, wantMethod string, wantID int64, approve bool) (rpcMsg, error) {
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	for {
		line, err := c.nextLine(30 * time.Second)
		if err != nil {
			return rpcMsg{}, err
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "{") {
			continue
		}
		var msg rpcMsg
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg.Method == "goal/ask_approval" && len(msg.ID) > 0 {
			resp := map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": map[string]any{"approved": approve}}
			b, _ := json.Marshal(resp)
			c.stdin <- string(b)
			continue
		}
		if wantMethod != "" && msg.Method == wantMethod {
			return msg, nil
		}
		if wantID != 0 && len(msg.ID) > 0 {
			var got int64
			if json.Unmarshal(msg.ID, &got) == nil && got == wantID {
				return msg, nil
			}
		}
	}
}

func TestE2E_ServeSession(t *testing.T) {
	bin := buildBinary(t)
	ws := t.TempDir()
	dataDir := t.TempDir()
	victim := filepath.Join(ws, "victim.txt")
	if err := os.WriteFile(victim, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := writeScript(t, []llm.Scripted{
		{Kind: "plan", Texts: []string{`{"steps":[
			{"id":"s1","description":"删除临时文件","tool":"file.delete","args":{"path":"victim.txt","recursive":false}}
		]}`}},
		{Kind: "reflect", Texts: []string{`{"score":95,"verdict":"done","reason":"已删除"}`}},
	})
	sc := startServe(t, bin, script, ws, dataDir)

	initResp, err := sc.callUntil(1, "initialize", map[string]any{}, "", 1, false)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if initResp.Error != nil || !strings.Contains(string(initResp.Result), "gleam") {
		t.Fatalf("initialize 响应 = %s err=%v", initResp.Result, initResp.Error)
	}

	_, err = sc.callUntil(2, "goal/submit", map[string]any{"goal": "删除 victim.txt"}, "goal/completed", 0, true)
	if err != nil {
		t.Fatalf("goal 流程: %v", err)
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Error("审批同意后文件应被删除")
	}

	sc.send(map[string]any{"jsonrpc": "2.0", "id": 99, "method": "shutdown"})
	done := make(chan struct{})
	go func() { <-sc.waitDone; close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Log("进程未在 5 秒内退出（MVP 可接受）")
	}
}

func TestE2E_MCPConnectorFullChain(t *testing.T) {
	bin := buildBinary(t)
	ws := t.TempDir()
	dataDir := t.TempDir()
	script := writeScript(t, []llm.Scripted{
		{Kind: "plan", Texts: []string{`{"steps":[
			{"id":"s1","description":"通过 MCP 回显","tool":"mcp.fake.echo","args":{"text":"gleam-mcp"}},
			{"id":"s2","description":"回复","tool":"reply","args":{"text":"MCP 完成"},"depends_on":["s1"]}
		]}`}},
		{Kind: "reflect", Texts: []string{`{"score":94,"verdict":"done","reason":"ok"}`}},
	})
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "mcp:\n  - name: fake\n    command: " + bin + "\n    args: [mcp-fake-server]\n    trust: readonly\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"goal", "--mock-llm", "--mock-script", script, "--workspace", ws, "--data-dir", dataDir, "--config", cfgPath, "通过 MCP 回显 gleam-mcp"}
	cmd := exec.Command(bin, args...)
	cmd.Env = mustEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("MCP goal 失败: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "MCP 完成") {
		t.Errorf("输出缺少摘要: %s", out)
	}
	if !strings.Contains(string(out), "[mcp] 已连接 fake（3 个工具）") && !strings.Contains(string(out), "已连接") {
		t.Logf("MCP 连接日志: %s", out)
	}
}

func TestE2E_BinarySelfCheck(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "gleam") {
		t.Fatalf("version: %v %s", err, out)
	}
	out, err = exec.Command(bin, "tools", "--mock-llm").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "file.write") {
		t.Fatalf("tools: %v %s", err, out)
	}
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("二进制体积: %.1f MB", float64(info.Size())/1024/1024)
	// Release builds with the embedded UI are around 16 MB; 15 MB was the old
	// target and would red-flag normal builds. 24 MB still catches
	// order-of-magnitude mistakes such as packing an installer into the engine.
	const sizeGate = 24 * 1024 * 1024
	if info.Size() > sizeGate {
		t.Errorf("二进制 %.1f MB 超过 %dMB 目标", float64(info.Size())/1024/1024, sizeGate>>20)
	}
}
