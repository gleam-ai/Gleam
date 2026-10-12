package agent

import (
	"fmt"
	"strings"
	"sync"

	"gleam/internal/tools/mcp"
)

type MCPManager struct {
	mu      sync.Mutex
	clients map[string]*mcp.Client
}

func NewMCPManager() *MCPManager {
	return &MCPManager{clients: map[string]*mcp.Client{}}
}

func (m *MCPManager) Add(c *mcp.Client) {
	if m == nil || c == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[c.Name] = c
}

func (m *MCPManager) AddAll(cs []*mcp.Client) {
	for _, c := range cs {
		m.Add(c)
	}
}

func (m *MCPManager) Get(name string) (*mcp.Client, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.clients[name]
	return c, ok
}

func (m *MCPManager) Remove(name string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	c, ok := m.clients[name]
	if ok {
		delete(m.clients, name)
	}
	m.mu.Unlock()
	if ok {
		c.Close()
	}
	return ok
}

func (m *MCPManager) CloseAll() {
	if m == nil {
		return
	}
	m.mu.Lock()
	cs := m.clients
	m.clients = map[string]*mcp.Client{}
	m.mu.Unlock()
	for _, c := range cs {
		c.Close()
	}
}

func mcpToolPrefix(server string) string {
	return fmt.Sprintf("mcp.%s.", strings.TrimSpace(server))
}
