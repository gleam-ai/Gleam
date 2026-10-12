package webui

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gleam/internal/atomicfile"
)

const TokenHeader = "X-Gleam-Token"

const TokenEnv = "GLEAM_WEBUI_TOKEN"

const TokenFileName = "webui.token"

const tokenQueryParam = "token"

const serverHeader = "X-Gleam-Server"

const minTokenLen = 16

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("webui: 生成口令失败: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func tokenFromEnv() string {
	v := strings.TrimSpace(os.Getenv(TokenEnv))
	if v == "" {
		return ""
	}
	if len(v) < minTokenLen {
		fmt.Fprintf(os.Stderr, "[gleam] %s 少于 %d 个字符，已忽略并改用随机口令\n", TokenEnv, minTokenLen)
		return ""
	}
	return v
}

func (s *Server) Token() string { return s.token }

func (s *Server) TokenFilePath() string {
	return filepath.Join(s.Agent.DataDir(), TokenFileName)
}

func (s *Server) WriteTokenFile() (string, error) {
	p := s.TokenFilePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	if err := atomicfile.Write(p, []byte(s.token+"\n"), 0o600); err != nil {
		return "", err
	}
	return p, nil
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(serverHeader, "gleam")
		if !s.hostAllowed(r) {
			writeErr(w, http.StatusForbidden, "Host 不在允许范围内")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if isStateChanging(r.Method) {
			if !originAllowed(r) {
				writeErr(w, http.StatusForbidden, "跨站请求已拒绝")
				return
			}
			if !contentTypeAllowed(r) {
				writeErr(w, http.StatusUnsupportedMediaType, "请求体必须是 application/json")
				return
			}
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/show-window" && isLoopbackRemote(r) {
			next.ServeHTTP(w, r)
			return
		}
		if !s.tokenOK(r) {
			writeErr(w, http.StatusUnauthorized, "缺少或错误的 API 口令")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) tokenOK(r *http.Request) bool {
	got := r.Header.Get(TokenHeader)
	if got == "" {
		if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
			got = strings.TrimSpace(strings.TrimPrefix(a, "Bearer "))
		}
	}
	if got == "" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		got = r.URL.Query().Get(tokenQueryParam)
	}
	return tokenEqual(got, s.token)
}

func tokenEqual(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func isStateChanging(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func originAllowed(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	return strings.EqualFold(o, "http://"+r.Host)
}

func contentTypeAllowed(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return r.ContentLength == 0
	}
	mt, _, err := mime.ParseMediaType(ct)
	return err == nil && mt == "application/json"
}

func (s *Server) hostAllowed(r *http.Request) bool {
	host := r.Host
	if host == "" {
		return false
	}
	if s.BindAddr != "" && strings.EqualFold(host, s.BindAddr) {
		return true
	}
	port := listenPort(r, s.BindAddr)
	h, p, err := net.SplitHostPort(host)
	if err != nil {
		h, p = strings.Trim(host, "[]"), "80"
	}
	if port == "" || p != port {
		return false
	}
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	return !bindIsLoopback(s.BindAddr)
}

func listenPort(r *http.Request, bindAddr string) string {
	if la, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok && la != nil {
		if _, p, err := net.SplitHostPort(la.String()); err == nil {
			return p
		}
	}
	if _, p, err := net.SplitHostPort(bindAddr); err == nil {
		return p
	}
	return ""
}

func bindIsLoopback(bindAddr string) bool {
	if strings.TrimSpace(bindAddr) == "" {
		return true
	}
	h, _, err := net.SplitHostPort(bindAddr)
	if err != nil {
		h = bindAddr
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) BindIsLoopback() bool { return bindIsLoopback(s.BindAddr) }

func isLoopbackRemote(r *http.Request) bool {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		h = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

func (s *Server) tokenMeta(r *http.Request) string {
	if !isLoopbackRemote(r) && !tokenEqual(r.URL.Query().Get(tokenQueryParam), s.token) {
		return ""
	}
	return `<meta name="gleam-token" content="` + s.token + `">`
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	raw, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "index.html 缺失", http.StatusInternalServerError)
		return
	}
	page := string(raw)
	if meta := s.tokenMeta(r); meta != "" {
		page = strings.Replace(page, "<head>", "<head>\n  "+meta, 1)
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(page))
}

func tokenOrNew() string {
	if t := tokenFromEnv(); t != "" {
		return t
	}
	return newToken()
}
