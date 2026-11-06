package web

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gleam/pkg/types"
)

func localTool() *Tool {
	t := New()
	t.AllowPrivate = true
	return t
}

func TestWeb_RejectsChallengePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>环境异常</title></head><body>` +
			`<p>当前环境异常，完成验证后即可继续访问。</p><a>去验证</a></body></html>`))
	}))
	defer srv.Close()

	_, err := localTool().Execute(context.Background(), map[string]any{"url": srv.URL})
	if err == nil {
		t.Fatal("反爬验证页应当报错，而不是当成正文交给模型")
	}
	if !strings.Contains(err.Error(), "微信访问验证页") {
		t.Fatalf("错误信息应点名验证页类型，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "重试同一 URL 不会成功") {
		t.Errorf("错误信息应提示重试无效，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "computer_use") {
		t.Errorf("错误信息应给出 CUA 接管路径，实际: %v", err)
	}
	// The agent loop only stops and hands off on this kind. If the wording
	// drifts, classification must be updated in the same change.
	if got := types.ClassifyError(err.Error()); got != types.ErrNeedsHuman {
		t.Errorf("风控错误被归类为 %q，应为 %q", got, types.ErrNeedsHuman)
	}
}

func TestWeb_ArticleAboutVerificationIsNotAChallengePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><article><p>环境异常排查手册：先完成验证再重试。</p>` +
			`<p>这是一篇讲排障的文章，不是拦截页。</p></article></body></html>`))
	}))
	defer srv.Close()

	out, err := localTool().Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("讨论验证的正文不该被判成验证页: %v", err)
	}
	if text, _ := out.(map[string]any)["text"].(string); !strings.Contains(text, "排障") {
		t.Errorf("正文应保留，实际: %v", out)
	}
}

func TestWeb_BlocksPrivateByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("内网数据"))
	}))
	defer srv.Close()

	tool := New()
	if _, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL}); err == nil {
		t.Fatal("默认应拒绝 127.0.0.1")
	} else if !strings.Contains(err.Error(), "拒绝访问本机或内网地址") {
		t.Fatalf("错误信息应说明是内网拦截，实际: %v", err)
	}

	for _, u := range []string{
		"http://localhost:8080/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
	} {
		if _, err := tool.Execute(context.Background(), map[string]any{"url": u}); err == nil {
			t.Errorf("%s 应被拒绝", u)
		}
	}
}

func TestWeb_AllowsPrivateWhenOptedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("本地内容"))
	}))
	defer srv.Close()

	tool := New()
	tool.AllowPrivate = true
	out, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("放宽后应可访问: %v", err)
	}
	if !strings.Contains(out.(map[string]any)["text"].(string), "本地内容") {
		t.Error("应拿到本地内容")
	}
}

func TestWeb_OutcomeReportsHTTPFailure(t *testing.T) {
	tool := localTool()
	if oc, note := tool.Outcome(nil, map[string]any{"status": 404}); oc != "failed" || note != "HTTP 404" {
		t.Errorf("404 应报 failed/HTTP 404，实际 %q/%q", oc, note)
	}
	if oc, _ := tool.Outcome(nil, map[string]any{"status": 200}); oc != "ok" {
		t.Errorf("200 应报 ok，实际 %q", oc)
	}
}

func TestWeb_FetchText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("纯文本内容 Gleam"))
	}))
	defer srv.Close()

	tool := localTool()
	out, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	m := out.(map[string]any)
	if !strings.Contains(m["text"].(string), "纯文本内容") {
		t.Errorf("text = %v", m["text"])
	}
	if m["status"] != http.StatusOK {
		t.Errorf("status = %v", m["status"])
	}
}

func TestWeb_MarksBodyTruncationAfterTextExtraction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		prefix := "<script>" + strings.Repeat("x", maxBodyBytes-1024) + "</script><p>正文</p>"
		_, _ = w.Write([]byte(prefix + strings.Repeat(" ", 2048)))
	}))
	defer srv.Close()

	out, err := localTool().Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["text"] != "正文" {
		t.Fatalf("text = %q", m["text"])
	}
	if m["bytes"] != maxBodyBytes {
		t.Fatalf("bytes = %v, want %d", m["bytes"], maxBodyBytes)
	}
	if m["truncated"] != true {
		t.Fatal("body limit was reached but truncated was false")
	}
}

func TestWeb_ExtractsHTMLText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><style>.x{color:red}</style></head>
		<body><h1>标题一</h1><script>alert(1)</script><p>正文&amp;更多</p></body></html>`))
	}))
	defer srv.Close()

	tool := localTool()
	out, _ := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	m := out.(map[string]any)
	text := m["text"].(string)
	if !strings.Contains(text, "标题一") || !strings.Contains(text, "正文&更多") {
		t.Errorf("正文提取失败: %q", text)
	}
	if strings.Contains(text, "alert") || strings.Contains(text, "color:red") {
		t.Errorf("script/style 应被剔除: %q", text)
	}
}

func TestWeb_RechecksDNSAtDialTime(t *testing.T) {
	tool := New()
	calls := 0
	tool.lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		calls++
		if calls == 1 {
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		}
		return []net.IP{net.ParseIP("127.0.0.1")}, nil // rebinding
	}
	_, err := tool.Execute(context.Background(), map[string]any{"url": "http://rebind.example/"})
	if err == nil {
		t.Fatal("DNS 重绑定到本机地址应在连接前被拦下")
	}
	if !strings.Contains(err.Error(), "拒绝访问本机或内网地址") {
		t.Fatalf("应报内网拦截，实际: %v", err)
	}
	if calls < 2 {
		t.Fatalf("解析应至少发生两次（校验 + 连接），实际 %d", calls)
	}
}

func TestWeb_RejectsNonHTTP(t *testing.T) {
	tool := localTool()
	if _, err := tool.Execute(context.Background(), map[string]any{"url": "file:///etc/passwd"}); err == nil {
		t.Error("file:// 应被拒绝")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"url": "ftp://x"}); err == nil {
		t.Error("ftp:// 应被拒绝")
	}
}

func TestWeb_HTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()
	tool := localTool()
	out, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("非 2xx 不应返回 error（由调用方判断状态）: %v", err)
	}
	if out.(map[string]any)["status"] != http.StatusNotFound {
		t.Error("应透传状态码")
	}
}

func TestWeb_Permission(t *testing.T) {
	tool := localTool()
	if tool.Name() != "web.fetch" {
		t.Errorf("name = %s", tool.Name())
	}
	_ = tool.Permission()
}
