package agent

import (
	"fmt"

	"gleam/internal/config"
	"gleam/internal/llm"
)

// Config returns the currently published settings. The returned *Config is
// immutable: hot reload swaps in a clone under cfgMu instead of mutating in
// place, so its fields can be read without further locking. Code that reads
// several fields must keep one returned pointer (one consistent snapshot)
// rather than calling Config() again mid-expression.
func (a *Agent) Config() *config.Config {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.Cfg
}

// PrimaryLLM returns the live main-model client; settings saves swap it under
// cfgMu, so callers must not read the LLM field directly.
func (a *Agent) PrimaryLLM() llm.Client {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.LLM
}

// FastClient returns the auxiliary-model client (nil when none is configured).
func (a *Agent) FastClient() llm.Client {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.FastLLM
}

// updateConfig runs fn on a private clone of the published settings and
// publishes the clone only when fn returns nil, so a rejected save leaves no
// half-applied state. fn runs under the cfgMu write lock: it must not call
// Config/PrimaryLLM/FastClient or anything else that takes cfgMu (an RLock
// under the held write lock deadlocks). Assigning a.LLM / a.FastLLM inside fn
// is allowed and is the only sanctioned way to swap them.
func (a *Agent) updateConfig(fn func(cfg *config.Config) error) error {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	if a.Cfg == nil {
		return fmt.Errorf("配置未初始化")
	}
	next := a.Cfg.Clone()
	if err := fn(next); err != nil {
		return err
	}
	a.Cfg = next
	return nil
}
