package registry

import (
	"fmt"
	"sort"
	"sync"

	"gleam/pkg/types"
)

type Registry struct {
	mu    sync.RWMutex
	tools map[string]types.Tool
}

func New() *Registry {
	return &Registry{tools: map[string]types.Tool{}}
}

func (r *Registry) Register(t types.Tool) error {
	if t == nil {
		return fmt.Errorf("registry: 工具为空")
	}
	name := t.Name()
	if name == "" {
		return fmt.Errorf("registry: 工具名为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("registry: 工具 %q 已注册", name)
	}
	r.tools[name] = t
	return nil
}

func (r *Registry) Replace(t types.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

func (r *Registry) MustRegister(t types.Tool) {
	if err := r.Register(t); err != nil {
		panic(err)
	}
}

func (r *Registry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; !ok {
		return false
	}
	delete(r.tools, name)
	return true
}

func (r *Registry) Get(name string) (types.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (r *Registry) List() []types.Tool {
	names := r.Names()
	out := make([]types.Tool, 0, len(names))
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, n := range names {
		out = append(out, r.tools[n])
	}
	return out
}

func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}
