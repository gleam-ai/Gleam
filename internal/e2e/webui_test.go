package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gleam/internal/llm"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestE2E_WebUI(t *testing.T) {
	bin := buildBinary(t)
	ws := t.TempDir()
	dataDir := t.TempDir()
	port := freePort(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	victim := filepath.Join(ws, "victim.txt")
	if err := os.WriteFile(victim, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := writeScript(t, []llm.Scripted{
		{Kind: "plan", Texts: []string{`{"steps":[{"id":"s1","description":"删除文件","tool":"file.delete","args":{"path":"victim.txt","recursive":false}}]}`}},
		{Kind: "reflect", Texts: []string{`{"score":95,"verdict":"done","reason":"已删除"}`}},
		{Kind: "plan", Texts: []string{`{"steps":[
			{"id":"s1","description":"写文件","tool":"file.write","args":{"path":"webui-e2e.txt","content":"WebUI 端到端"}},
			{"id":"s2","description":"回复","tool":"reply","args":{"text":"WebUI 流程完成"},"depends_on":["s1"]}
		]}`}},
		{Kind: "reflect", Texts: []string{`{"score":93,"verdict":"done","reason":"ok"}`}},
	})

	cmd := exec.Command(bin, "webui", "--mock-llm", "--mock-script", script, "--workspace", ws,
		"--data-dir", dataDir, "--addr", fmt.Sprintf("127.0.0.1:%d", port))
	cmd.Env = append(os.Environ(), "GLEAM_WEBUI_TOKEN=")
	stderr := &strings.Builder{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	client := &http.Client{Timeout: 5 * time.Second}
	ready := false
	for i := 0; i < 50; i++ {
		resp, err := client.Get(base + "/api/info")
		if err == nil {
			resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("Web UI 未就绪: %s", stderr.String())
	}

	if resp, err := client.Get(base + "/api/info"); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("无口令 GET /api/info 应为 401: %v %v", err, resp)
	} else {
		resp.Body.Close()
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, "webui.token"))
	if err != nil {
		t.Fatalf("读口令文件失败: %v", err)
	}
	client.Transport = tokenTransport{token: strings.TrimSpace(string(raw))}

	call := func(method, path string, body any) map[string]any {
		var rd *strings.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		} else {
			rd = strings.NewReader("")
		}
		req, _ := http.NewRequest(method, base+path, rd)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if resp.StatusCode >= 400 {
			t.Fatalf("%s %s -> %d: %v", method, path, resp.StatusCode, out)
		}
		return out
	}

	info := call("GET", "/api/info", nil)
	if info["name"] != "gleam" {
		t.Fatalf("info = %v", info)
	}
	resp, err := client.Get(base + "/")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("首页加载失败: %v", err)
	}
	resp.Body.Close()

	out := call("POST", "/api/goals", map[string]any{"goal": "删除 victim.txt", "mode": "auto"})
	taskID, _ := out["task_id"].(string)
	if taskID == "" {
		t.Fatalf("submit = %v", out)
	}
	var approvals []any
	for i := 0; i < 100; i++ {
		res := call("GET", "/api/approvals", nil)
		approvals, _ = res["approvals"].([]any)
		if len(approvals) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(approvals) == 0 {
		t.Fatal("未出现审批请求")
	}
	ap := approvals[0].(map[string]any)
	call("POST", fmt.Sprintf("/api/approvals/%v", ap["id"]), map[string]any{"approved": true})
	waitDone(t, client, base, taskID, "success", 15*time.Second)
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Error("批准后文件应被删除")
	}

	out2 := call("POST", "/api/goals", map[string]any{"goal": "创建 webui-e2e.txt 并验证"})
	taskID2, _ := out2["task_id"].(string)
	done := waitDone(t, client, base, taskID2, "success", 15*time.Second)
	result := done["result"].(map[string]any)
	if result["summary"] != "WebUI 流程完成" {
		t.Errorf("summary = %v", result["summary"])
	}
	data, rerr := os.ReadFile(filepath.Join(ws, "webui-e2e.txt"))
	if rerr != nil || string(data) != "WebUI 端到端" {
		t.Errorf("产物错误: %v %q", rerr, data)
	}
}

func waitDone(t *testing.T, client *http.Client, base, taskID, want string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(base + "/api/goals/" + taskID)
		if err == nil {
			var out map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&out)
			resp.Body.Close()
			if s, _ := out["status"].(string); s == want {
				return out
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("等待任务 %s 状态 %s 超时", taskID, want)
	return nil
}

type tokenTransport struct{ token string }

func (t tokenTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Gleam-Token", t.token)
	return http.DefaultTransport.RoundTrip(r)
}
