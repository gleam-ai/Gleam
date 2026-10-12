package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWeb_RecordsEgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>页面正文</body></html>"))
	}))
	defer srv.Close()

	tool := localTool()
	var host string
	var nbytes int
	tool.OnEgress = func(h string, n int) { host, nbytes = h, n }

	if _, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL}); err != nil {
		t.Fatalf("抓取应成功: %v", err)
	}
	if host == "" {
		t.Fatal("抓取发生了却没有出网留痕")
	}
	if !strings.Contains(host, "127.0.0.1") {
		t.Errorf("留痕应记下目标主机，实际 %q", host)
	}
	if nbytes == 0 {
		t.Error("留痕应记下请求字节数")
	}
	if strings.Contains(host, "页面正文") {
		t.Errorf("留痕不该包含内容：%q", host)
	}
}

func TestWeb_BlockedRequestDoesNotRecordEgress(t *testing.T) {
	tool := New()
	called := false
	tool.OnEgress = func(string, int) { called = true }

	if _, err := tool.Execute(context.Background(), map[string]any{"url": "http://169.254.169.254/latest/meta-data/"}); err == nil {
		t.Fatal("默认应拦下云元数据地址")
	}
	if called {
		t.Error("被拦下的请求没有出网，不该记出网留痕")
	}
}

func TestWeb_NoHookNoPanic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer srv.Close()

	if _, err := localTool().Execute(context.Background(), map[string]any{"url": srv.URL}); err != nil {
		t.Fatalf("没装出网回调时也应正常抓取: %v", err)
	}
}
