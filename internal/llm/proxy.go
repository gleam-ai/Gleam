package llm

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

var (
	proxyMu        sync.RWMutex
	proxyTransport http.RoundTripper = http.DefaultTransport
)

func SetProxyMode(mode, rawURL string) error {
	tr, err := buildTransport(mode, rawURL)
	if err != nil {
		return err
	}
	proxyMu.Lock()
	proxyTransport = tr
	proxyMu.Unlock()
	return nil
}

func Transport() http.RoundTripper {
	proxyMu.RLock()
	defer proxyMu.RUnlock()
	return proxyTransport
}

func buildTransport(mode, rawURL string) (http.RoundTripper, error) {
	switch strings.TrimSpace(mode) {
	case "", "system":
		return http.DefaultTransport, nil
	case "none":
		tr, err := cloneDefault()
		if err != nil {
			return nil, err
		}
		tr.Proxy = nil
		return tr, nil
	case "manual":
		raw := strings.TrimSpace(rawURL)
		if raw == "" {
			return nil, fmt.Errorf("选了手动代理，但没填代理地址")
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("代理地址不合法：%s（形如 http://127.0.0.1:7890）", raw)
		}
		switch u.Scheme {
		case "http", "https", "socks5":
		default:
			return nil, fmt.Errorf("只支持 http / https / socks5 代理，收到 %q", u.Scheme)
		}
		tr, err := cloneDefault()
		if err != nil {
			return nil, err
		}
		tr.Proxy = http.ProxyURL(u)
		return tr, nil
	default:
		return nil, fmt.Errorf("未知的代理方式：%s", mode)
	}
}

func cloneDefault() (*http.Transport, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("默认传输层不是 *http.Transport，代理设置无从附着")
	}
	return base.Clone(), nil
}
