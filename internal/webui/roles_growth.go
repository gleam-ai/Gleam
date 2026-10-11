package webui

import (
	"net/http"
	"strconv"

	"gleam/internal/agent"
)

func (s *Server) handleRolesList(w http.ResponseWriter, _ *http.Request) {
	roles := agent.RoleList()
	writeJSON(w, 200, map[string]any{
		"roles": roles,
		"count": len(roles),
	})
}

func (s *Server) handleGrowthStats(w http.ResponseWriter, _ *http.Request) {
	if s.Agent == nil || s.Agent.Growth == nil {
		writeJSON(w, 200, map[string]any{"stats": nil, "entries": []any{}})
		return
	}
	stats := s.Agent.Growth.Stats()
	writeJSON(w, 200, map[string]any{"stats": stats})
}

func (s *Server) handleGrowthRecent(w http.ResponseWriter, r *http.Request) {
	if s.Agent == nil || s.Agent.Growth == nil {
		writeJSON(w, 200, map[string]any{"entries": []any{}})
		return
	}
	n := 20
	if q := r.URL.Query().Get("n"); q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 && v <= 200 {
			n = v
		}
	}
	entries := s.Agent.Growth.Recent(n)
	writeJSON(w, 200, map[string]any{"entries": entries, "count": len(entries)})
}
