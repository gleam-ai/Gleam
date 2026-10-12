package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gleam/internal/buildinfo"
)

const remoteTimeout = 30 * time.Second

var OnRemoteEgress func(host string, nbytes int)

func SetRemoteEgressHook(fn func(host string, nbytes int)) { OnRemoteEgress = fn }

type httpTransport struct {
	name    string
	url     string
	headers map[string]string
	client  *http.Client
	logf    func(string, ...any)

	mu        sync.Mutex
	nextID    int64
	sessionID string
	closed    bool
}

func newHTTPTransport(cfg ServerConfig, logf func(string, ...any)) (*httpTransport, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.URL))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("mcp: 远端地址不可用 %q", cfg.URL)
	}
	if u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
		return nil, fmt.Errorf("mcp: 远端地址必须是 https（拿到的是 %q）", u.Scheme)
	}
	return &httpTransport{
		name: cfg.Name, url: u.String(), headers: cfg.Headers,
		client: &http.Client{Timeout: remoteTimeout}, logf: logf,
	}, nil
}

func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || strings.HasPrefix(h, "127.")
}

func (t *httpTransport) notify(method string, params any) error {
	_, err := t.roundTrip(context.Background(), rpcMessage{
		JSONRPC: "2.0", Method: method, Params: paramsRaw(params),
	}, false)
	return err
}

func (t *httpTransport) call(ctx context.Context, method string, params any, out any) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("mcp: %s 已关闭", t.name)
	}
	t.nextID++
	id := t.nextID
	t.mu.Unlock()

	idRaw, _ := json.Marshal(id)
	msg, err := t.roundTrip(ctx, rpcMessage{
		JSONRPC: "2.0", ID: idRaw, Method: method, Params: paramsRaw(params),
	}, true)
	if err != nil {
		return err
	}
	return decodeResult(t.name, method, msg, out)
}

func (t *httpTransport) roundTrip(ctx context.Context, req rpcMessage, wantResult bool) (rpcMessage, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return rpcMessage{}, err
	}
	cctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	hreq, err := http.NewRequestWithContext(cctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return rpcMessage{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "application/json, text/event-stream")
	hreq.Header.Set("User-Agent", "gleam/"+buildinfo.Version)
	for k, v := range t.headers {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			hreq.Header.Set(k, v)
		}
	}
	t.mu.Lock()
	sid := t.sessionID
	t.mu.Unlock()
	if sid != "" {
		hreq.Header.Set("Mcp-Session-Id", sid)
	}

	resp, err := t.client.Do(hreq)
	if err != nil {
		return rpcMessage{}, fmt.Errorf("mcp: %s 连不上 %s：%w", t.name, hostOfURL(t.url), err)
	}
	defer resp.Body.Close()
	if s := resp.Header.Get("Mcp-Session-Id"); s != "" {
		t.mu.Lock()
		t.sessionID = s
		t.mu.Unlock()
	}

	rb := &countingReader{r: io.LimitReader(resp.Body, 8<<20)}
	defer func() { t.noteEgress(rb.n) }()

	if !wantResult && resp.StatusCode < 400 {
		_, _ = io.Copy(io.Discard, rb)
		return rpcMessage{}, nil
	}
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(rb, 4096))
		return rpcMessage{}, fmt.Errorf("mcp: %s %s 返回 %d：%s",
			t.name, hostOfURL(t.url), resp.StatusCode, clipSecret(string(msg)))
	}

	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		return t.readSSE(rb)
	}
	var msg rpcMessage
	if err := json.NewDecoder(rb).Decode(&msg); err != nil {
		return rpcMessage{}, fmt.Errorf("mcp: %s 的响应读不懂：%w", t.name, err)
	}
	return msg, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (t *httpTransport) readSSE(r io.Reader) (rpcMessage, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	var data []string
	for {
		line, err := br.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if len(data) > 0 {
				var msg rpcMessage
				if jerr := json.Unmarshal([]byte(strings.Join(data, "\n")), &msg); jerr == nil && (len(msg.ID) > 0 || msg.Error != nil) {
					return msg, nil
				}
				data = nil
			}
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
		if err != nil {
			if len(data) > 0 {
				var msg rpcMessage
				if jerr := json.Unmarshal([]byte(strings.Join(data, "\n")), &msg); jerr == nil && (len(msg.ID) > 0 || msg.Error != nil) {
					return msg, nil
				}
			}
			if err == io.EOF {
				return rpcMessage{}, fmt.Errorf("mcp: %s 的事件流里没有带结果的响应", t.name)
			}
			return rpcMessage{}, err
		}
	}
}

func (t *httpTransport) close() error {
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
	return nil
}

func (t *httpTransport) noteEgress(n int64) {
	if OnRemoteEgress == nil || n <= 0 {
		return
	}
	OnRemoteEgress(hostOfURL(t.url), int(n))
}

func hostOfURL(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

func clipSecret(s string) string {
	s = strings.TrimSpace(s)
	for _, key := range []string{"Authorization", "authorization", "api_key", "apikey", "token", "Bearer "} {
		if i := strings.Index(s, key); i >= 0 {
			end := i + len(key)
			s = s[:end] + " ***"
			break
		}
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
