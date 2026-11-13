package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gleam/internal/llm"
)

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func postGoalStatus(t *testing.T, f *fixture, body map[string]any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(f.ts.URL+"/api/goals", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestGoalSubmit_RejectsInvalidImages(t *testing.T) {
	img := map[string]any{"mime": "image/png", "data": tinyPNG}
	cases := []struct {
		name string
		body map[string]any
	}{
		{"work mode", map[string]any{
			"goal": "看图", "mode": "auto", "task_mode": "work",
			"images": []any{img},
		}},
		{"too many", map[string]any{
			"goal": "看图", "mode": "auto", "task_mode": "chat",
			"images": []any{img, img, img, img, img},
		}},
		{"bad mime", map[string]any{
			"goal": "看图", "mode": "auto", "task_mode": "chat",
			"images": []any{map[string]any{"mime": "image/svg+xml", "data": tinyPNG}},
		}},
		{"empty data", map[string]any{
			"goal": "看图", "mode": "auto", "task_mode": "chat",
			"images": []any{map[string]any{"mime": "image/png"}},
		}},
		{"bad url scheme", map[string]any{
			"goal": "看图", "mode": "auto", "task_mode": "chat",
			"images": []any{map[string]any{"mime": "image/png", "url": "file:///etc/passwd"}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, nil)
			if code := postGoalStatus(t, f, tc.body); code != 400 {
				t.Fatalf("应返回 400，实际 %d", code)
			}
		})
	}
}

func TestGoalSubmit_ChatImageReachesLLM(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		{Kind: "chat_mode", Texts: []string{"图里是一个红色像素。"}},
	})
	out := f.call("POST", "/api/goals", map[string]any{
		"goal": "这张图是什么", "mode": "auto", "task_mode": "chat",
		"images": []any{map[string]any{"mime": "image/png", "data": tinyPNG}},
	})
	taskID, _ := out["task_id"].(string)
	done := f.waitTask(taskID, 10*time.Second)
	if done["status"] != "success" {
		t.Fatalf("chat 结果异常: %v", done)
	}
	found := false
	for _, c := range f.mock.Calls {
		for _, m := range c.Messages {
			if len(m.Images) == 1 && m.Images[0].MIME == "image/png" && m.Images[0].Data == tinyPNG {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("图片附件没有传进 LLM 请求")
	}
}

func TestWorkspaceImageEndpoint(t *testing.T) {
	f := newFixture(t, nil)
	imgDir := filepath.Join(f.ws, "images")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\nfake-but-served")
	if err := os.WriteFile(filepath.Join(imgDir, "pic.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imgDir, "notes.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.ws, "outside.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}

	get := func(path string) *http.Response {
		t.Helper()
		resp, err := http.Get(f.ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

	resp := get("/api/images/pic.png")
	if resp.StatusCode != 200 {
		t.Fatalf("合法图片应 200，实际 %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/png") {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "private, max-age=3600" {
		t.Errorf("Cache-Control = %q", cc)
	}

	cases := []struct {
		path string
		want int
	}{
		{"/api/images/notes.txt", 400},
		{"/api/images/nope.png", 404},
		{"/api/images/outside.png", 404},
		{"/api/images/..%5Coutside.png", 400},
	}
	for _, tc := range cases {
		if resp := get(tc.path); resp.StatusCode != tc.want {
			t.Errorf("GET %s = %d, 期望 %d", tc.path, resp.StatusCode, tc.want)
		}
	}
}
