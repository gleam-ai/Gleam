package llm

import (
	"net/url"
	"sync"
)

type EgressFunc func(host string, nbytes int)

var (
	egressMu   sync.RWMutex
	egressHook EgressFunc
)

func SetEgressHook(fn EgressFunc) {
	egressMu.Lock()
	egressHook = fn
	egressMu.Unlock()
}

func noteEgress(rawURL string, nbytes int) {
	egressMu.RLock()
	fn := egressHook
	egressMu.RUnlock()
	if fn == nil {
		return
	}
	host := rawURL
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		host = u.Host
	}
	fn(host, nbytes)
}
