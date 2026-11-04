package image

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gleam/pkg/types"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\n" + "restofimage")

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func newTestTool(t *testing.T, status int, respBody string, roots ...string) (*Generate, *[]httpReq) {
	t.Helper()
	reqs := &[]httpReq{}
	tt := NewGenerate(
		func(ctx context.Context) (string, string, string, error) {
			return "https://api.test/v4", "key-123", "cogview-4-250304", nil
		},
		func(ctx context.Context) []string { return roots },
	)
	tt.httpPost = func(_ context.Context, url, key string, body []byte) (int, []byte, error) {
		*reqs = append(*reqs, httpReq{url, key, body})
		return status, []byte(respBody), nil
	}
	return tt, reqs
}

type httpReq struct {
	url  string
	key  string
	body []byte
}

func TestGenerate_B64ResponseSaved(t *testing.T) {
	root := t.TempDir()
	body := `{"data":[{"b64_json":"` + b64(pngBytes) + `"}]}`
	tt, reqs := newTestTool(t, 200, body, root)

	out, err := tt.Execute(context.Background(), map[string]any{"prompt": "一只猫", "file_name": "cat"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	path := m["path"].(string)
	if !strings.HasSuffix(path, filepath.Join("images", "cat.png")) {
		t.Errorf("path = %s, want images/cat.png", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != string(pngBytes) {
		t.Fatalf("落盘内容不对: %v", err)
	}
	if m["bytes"] != len(pngBytes) || m["model"] != "cogview-4-250304" {
		t.Errorf("meta = %v", m)
	}
	r := (*reqs)[0]
	if r.url != "https://api.test/v4/images/generations" || r.key != "key-123" {
		t.Errorf("req = %s %s", r.url, r.key)
	}
	if !strings.Contains(string(r.body), `"model":"cogview-4-250304"`) {
		t.Errorf("body = %s", r.body)
	}
}

func TestGenerate_URLResponseDownloaded(t *testing.T) {
	root := t.TempDir()
	tt, _ := newTestTool(t, 200, `{"data":[{"url":"https://cdn.test/img/abc"}]}`, root)
	tt.httpGet = func(_ context.Context, url string) ([]byte, error) {
		if url != "https://cdn.test/img/abc" {
			t.Errorf("download url = %s", url)
		}
		return pngBytes, nil
	}

	out, err := tt.Execute(context.Background(), map[string]any{"prompt": "测试"})
	if err != nil {
		t.Fatal(err)
	}
	path := out.(map[string]any)["path"].(string)
	if !strings.HasSuffix(path, ".png") {
		t.Errorf("path = %s", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestGenerate_ErrorsAreHonest(t *testing.T) {
	root := t.TempDir()
	// image_model not configured
	tt := NewGenerate(
		func(ctx context.Context) (string, string, string, error) { return "https://api.test/v4", "k", "", nil },
		func(ctx context.Context) []string { return []string{root} },
	)
	if _, err := tt.Execute(context.Background(), map[string]any{"prompt": "x"}); err == nil || !strings.Contains(err.Error(), "image_model") {
		t.Errorf("缺 image_model 应明确报错, got %v", err)
	}
	// no API key
	tt2 := NewGenerate(
		func(ctx context.Context) (string, string, string, error) {
			return "https://api.test/v4", "", "cogview-4-250304", nil
		},
		func(ctx context.Context) []string { return []string{root} },
	)
	if _, err := tt2.Execute(context.Background(), map[string]any{"prompt": "x"}); err == nil || !strings.Contains(err.Error(), "密钥") {
		t.Errorf("缺密钥应明确报错, got %v", err)
	}
	// API error payload
	tt3, _ := newTestTool(t, 200, `{"error":{"message":"quota exceeded"}}`, root)
	if _, err := tt3.Execute(context.Background(), map[string]any{"prompt": "x"}); err == nil || !strings.Contains(err.Error(), "quota") {
		t.Errorf("API 错误应透传, got %v", err)
	}
	// HTTP 500
	tt4, _ := newTestTool(t, 500, `boom`, root)
	if _, err := tt4.Execute(context.Background(), map[string]any{"prompt": "x"}); err == nil {
		t.Error("500 应报错")
	}
	// response has neither b64 nor url
	tt5, _ := newTestTool(t, 200, `{"data":[{}]}`, root)
	if _, err := tt5.Execute(context.Background(), map[string]any{"prompt": "x"}); err == nil {
		t.Error("空响应应报错")
	}
	// bytes are not an image
	tt6, _ := newTestTool(t, 200, `{"data":[{"b64_json":"`+b64([]byte("not an image"))+`"}]}`, root)
	if _, err := tt6.Execute(context.Background(), map[string]any{"prompt": "x"}); err == nil || !strings.Contains(err.Error(), "图片格式") {
		t.Errorf("非图片字节应报错, got %v", err)
	}
	// no workspace
	tt7, _ := newTestTool(t, 200, `{"data":[{"b64_json":"`+b64(pngBytes)+`"}]}`)
	tt7.Roots = func(ctx context.Context) []string { return nil }
	if _, err := tt7.Execute(context.Background(), map[string]any{"prompt": "x"}); err == nil {
		t.Error("无工作区应报错")
	}
}

func TestGenerate_PathEscapeBlocked(t *testing.T) {
	root := t.TempDir()
	tt, _ := newTestTool(t, 200, `{"data":[{"b64_json":"`+b64(pngBytes)+`"}]}`, root)
	out, err := tt.Execute(context.Background(), map[string]any{"prompt": "x", "file_name": "../../../evil"})
	if err != nil {
		t.Fatalf("穿越序列应被清洗成安全文件名而不是报错: %v", err)
	}
	path := out.(map[string]any)["path"].(string)
	// The written file must still land inside the workspace images/ directory.
	if !strings.HasPrefix(filepath.Clean(path), filepath.Join(root, "images")) {
		t.Errorf("保存路径越界: %s", path)
	}
	if strings.Contains(filepath.Base(path), "..") {
		t.Errorf("文件名残留穿越序列: %s", path)
	}
}

func TestSniffExtAndSanitize(t *testing.T) {
	if sniffExt(pngBytes) != "png" {
		t.Error("png 魔数")
	}
	if sniffExt([]byte("\xff\xd8\xff\xe0jpeg")) != "jpg" {
		t.Error("jpg 魔数")
	}
	if sniffExt([]byte("RIFFxxxxWEBPVP8 ")) != "webp" {
		t.Error("webp 魔数")
	}
	if sniffExt([]byte("plain text")) != "" {
		t.Error("文本不应识别为图片")
	}
	if got := sanitizeName("我的 猫/图:1"); strings.ContainsAny(got, "/:") {
		t.Errorf("sanitize 应清洗路径分隔符: %q", got)
	}
}

func TestGenerate_ToolContract(t *testing.T) {
	tt := NewGenerate(nil, nil)
	if tt.Name() != "image.generate" || tt.Permission() != types.PermissionUserApproved {
		t.Errorf("name/perm = %s/%v", tt.Name(), tt.Permission())
	}
	if _, err := tt.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("缺 prompt 应报错")
	}
	// Source not wired up
	if _, err := tt.Execute(context.Background(), map[string]any{"prompt": "x"}); err == nil {
		t.Error("未接入运行时应报错")
	}
}
