package web

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

const (
	maxBodyBytes = 2 * 1024 * 1024
	maxTextLen   = 20 * 1024
)

type Tool struct {
	HTTP *http.Client

	AllowPrivate bool

	OnEgress func(host string, nbytes int)

	// lookupIP is swappable for tests; nil means the system resolver.
	lookupIP func(ctx context.Context, host string) ([]net.IP, error)
}

func New() *Tool {
	return &Tool{HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// GuardURL applies web.fetch's private-network policy to URLs that come from
// remote responses rather than from the user.
func GuardURL(ctx context.Context, raw string) error {
	return (&Tool{}).guardURL(ctx, raw)
}

// GuardedClient returns a client that re-checks every redirect and dials the
// exact IP that passed the check, so a URL cannot be swapped for a loopback
// address between validation and connect.
func GuardedClient(timeout time.Duration) *http.Client {
	t := &Tool{HTTP: &http.Client{}}
	c := &http.Client{Timeout: timeout, Transport: t.pinnedTransport()}
	c.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("重定向次数过多")
		}
		return t.guardURL(r.Context(), r.URL.String())
	}
	return c
}

func (t *Tool) Name() string        { return "web.fetch" }
func (t *Tool) Description() string { return "抓取网页内容并提取正文文本（只读）" }
func (t *Tool) Permission() types.Permission {
	return types.PermissionUserApproved
}
func (t *Tool) Schema() map[string]any {
	return toolutil.Schema("抓取网页", []string{"url"}, map[string]any{
		"url": toolutil.SchemaProp("网页地址（http/https）", "string"),
	})
}
func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	u, err := toolutil.RequireStr(args, "url")
	if err != nil {
		return nil, err
	}
	if err := t.guardURL(ctx, u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Gleam/0.1 (local-first desktop agent)")

	if t.OnEgress != nil {
		host := u
		if parsed, perr := url.Parse(u); perr == nil && parsed.Host != "" {
			host = parsed.Host
		}
		t.OnEgress(host, len(u))
	}

	client := *t.HTTP
	if !t.AllowPrivate {
		client.Transport = t.pinnedTransport()
	}
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("重定向次数过多")
		}
		return t.guardURL(r.Context(), r.URL.String())
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	contentType := resp.Header.Get("Content-Type")
	bodyTruncated := len(body) > maxBodyBytes
	if bodyTruncated {
		body = body[:maxBodyBytes]
	}
	text := string(body)
	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml") {
		text = extractText(text)
		if kind := challengePage(text, string(body)); kind != "" {
			// Handing the interstitial to the model is worse than failing: it
			// spends steps "analyzing" a page that only says the request was
			// rejected, then reports it could not find the content.
			return nil, fmt.Errorf(
				"目标站点返回了%s，没有拿到正文。这是对方的风控策略，不是网络故障；"+
					"不要重复请求同一 URL，改换来源或请用户手动打开", kind)
		}
	}
	truncated := bodyTruncated || len(text) > maxTextLen
	if len(text) > maxTextLen {
		end := maxTextLen
		for end > 0 && !utf8.RuneStart(text[end]) {
			end--
		}
		text = text[:end]
	}
	return map[string]any{
		"url":          u,
		"status":       resp.StatusCode,
		"content_type": contentType,
		"bytes":        len(body),
		"text":         text,
		"truncated":    truncated,
	}, nil
}

var (
	scriptRe = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	tagRe    = regexp.MustCompile(`(?s)<[^>]*>`)
	spaceRe  = regexp.MustCompile(`[ \t]+`)
	linesRe  = regexp.MustCompile(`\n{3,}`)
)

func (t *Tool) Outcome(args map[string]any, out any) (types.StepOutcome, string) {
	m, ok := out.(map[string]any)
	if !ok {
		return types.OutcomeOK, ""
	}
	if code := toolutil.IntOf(m["status"]); code >= 400 {
		return types.OutcomeFailed, fmt.Sprintf("HTTP %d", code)
	}
	return types.OutcomeOK, ""
}

func (t *Tool) guardURL(ctx context.Context, raw string) error {
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return fmt.Errorf("仅支持 http/https 地址，收到 %q", raw)
	}
	if t.AllowPrivate {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("地址解析失败: %w", err)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("地址缺少主机名: %q", raw)
	}
	ips, err := t.resolve(ctx, host)
	if err != nil {
		return fmt.Errorf("域名解析失败（%s）: %w", host, err)
	}
	for _, ip := range ips {
		if blockedIP(ip) {
			return fmt.Errorf("拒绝访问本机或内网地址：%s 解析为 %s（如确需访问，请打开 safety.allow_private_web）", host, ip)
		}
	}
	return nil
}

func (t *Tool) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	if t.lookupIP != nil {
		return t.lookupIP(ctx, host)
	}
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// pinnedTransport checks the address again at dial time and dials the very IP
// that passed the check. Guarding only the URL leaves a DNS-rebinding window:
// the name can resolve to a public address during the check and to 127.0.0.1
// or 169.254.169.254 by the time the request connects.
func (t *Tool) pinnedTransport() http.RoundTripper {
	var tr *http.Transport
	switch base := t.HTTP.Transport.(type) {
	case *http.Transport:
		tr = base.Clone()
	case nil:
		tr = http.DefaultTransport.(*http.Transport).Clone()
	default:
		return t.HTTP.Transport
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := t.resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if blockedIP(ip) {
				return nil, fmt.Errorf("拒绝访问本机或内网地址：%s 解析为 %s（如确需访问，请打开 safety.allow_private_web）", host, ip)
			}
		}
		var lastErr error
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("域名 %s 没有可用地址", host)
		}
		return nil, lastErr
	}
	return tr
}

func blockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

// challengePage names the bot-check interstitials worth failing on. Only
// signatures that do not occur on ordinary pages are listed here: a false
// positive would hide a real article from the model.
func challengePage(text, raw string) string {
	switch {
	case strings.Contains(text, "环境异常") && strings.Contains(text, "去验证"):
		// WeChat's interstitial is the environment-abnormal notice plus a
		// verify button. Requiring both keeps an article that merely discusses
		// verification from being swallowed.
		return "微信访问验证页"
	case strings.Contains(raw, "cf-browser-verification"),
		strings.Contains(raw, "cf-challenge"),
		strings.Contains(raw, "Enable JavaScript and cookies to continue"):
		return "Cloudflare 人机验证页"
	}
	return ""
}

func extractText(html string) string {
	html = scriptRe.ReplaceAllString(html, " ")
	html = tagRe.ReplaceAllString(html, "\n")
	html = strings.ReplaceAll(html, "&nbsp;", " ")
	html = strings.ReplaceAll(html, "&amp;", "&")
	html = strings.ReplaceAll(html, "&lt;", "<")
	html = strings.ReplaceAll(html, "&gt;", ">")
	html = strings.ReplaceAll(html, "&quot;", "\"")
	html = spaceRe.ReplaceAllString(html, " ")
	html = linesRe.ReplaceAllString(html, "\n\n")
	return strings.TrimSpace(html)
}
