package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gleam/internal/harness/credentials"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	creds, err := credentials.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := creds.SetCloudConfig(credentials.CloudConfig{
		SupabaseURL: "https://example.supabase.co", SupabaseAnonKey: "test-anon",
	}); err != nil {
		t.Fatal(err)
	}
	return New(creds)
}

func TestCallbackPageEscapesMessage(t *testing.T) {
	payload := `<script>alert(1)</script>`
	page := callbackPage(false, payload)
	if strings.Contains(page, "<script>alert(1)</script>") {
		t.Errorf("callbackPage 未转义: %s", page)
	}
	if !strings.Contains(page, "&lt;script&gt;") {
		t.Errorf("callbackPage 应转义为实体: %s", page)
	}
}

func TestStartOAuthCarriesState(t *testing.T) {
	m := newTestManager(t)
	authURL, err := m.StartOAuth("github")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatal("authorize URL 缺少 state")
	}
	if u.Query().Get("code_challenge_method") != "S256" {
		t.Error("PKCE 方法应为 S256")
	}
}

func TestOAuthCallbackRejectsStateMismatch(t *testing.T) {
	m := newTestManager(t)
	authURL, err := m.StartOAuth("github")
	if err != nil {
		t.Fatal(err)
	}
	base := queryOf(authURL, "redirect_to")
	if base == "" {
		t.Fatal("authorize URL 缺少 redirect_to")
	}
	waitForListener(t, base)

	get(t, base+"?code=SHOULD_NOT_BE_USED")
	get(t, base+"?code=SHOULD_NOT_BE_USED&state=forged")
	if len(m.pending.codeCh) != 0 {
		t.Fatal("state 不匹配的回调不应把 code 送入换令牌流程")
	}
}

func queryOf(rawurl, key string) string {
	u, _ := url.Parse(rawurl)
	return u.Query().Get(key)
}

func waitForListener(t *testing.T, callbackURL string) {
	t.Helper()
	u, _ := url.Parse(callbackURL)
	host := "http://" + u.Host
	for i := 0; i < 50; i++ {
		resp, err := http.Get(host + "/callback")
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("回环监听器未能启动")
}

func get(t *testing.T, full string) string {
	t.Helper()
	resp, err := http.Get(full)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestSignInRecordsEgress(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"a","refresh_token":"b","expires_in":3600,`+
			`"token_type":"bearer","user":{"id":"u1","email":"me@example.com"}}`)
	}))
	defer ts.Close()

	creds, err := credentials.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := creds.SetCloudConfig(credentials.CloudConfig{SupabaseURL: ts.URL, SupabaseAnonKey: "anon"}); err != nil {
		t.Fatal(err)
	}
	m := New(creds)
	var gotHost string
	var gotBytes int
	m.OnEgress = func(h string, n int) { gotHost, gotBytes = h, n }

	if err := m.SignIn("me@example.com", "hunter2"); err != nil {
		t.Fatalf("登录失败：%v", err)
	}
	if want := strings.TrimPrefix(ts.URL, "http://"); gotHost != want {
		t.Errorf("出网留痕的主机 = %q，应为 %q", gotHost, want)
	}
	if gotBytes <= 0 {
		t.Errorf("出网留痕应带上请求体大小，实得 %d", gotBytes)
	}
}

func TestSignInWithoutHookStillWorks(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"a","expires_in":3600,"user":{"id":"u1"}}`)
	}))
	defer ts.Close()
	creds, err := credentials.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := creds.SetCloudConfig(credentials.CloudConfig{SupabaseURL: ts.URL, SupabaseAnonKey: "anon"}); err != nil {
		t.Fatal(err)
	}
	if err := New(creds).SignIn("me@example.com", "hunter2"); err != nil {
		t.Fatalf("回调为空不应影响登录：%v", err)
	}
}
