package webui

import (
	"errors"
	"net/http"

	"gleam/internal/worktree"
)

func (s *Server) handleWorktreeList(w http.ResponseWriter, r *http.Request) {
	if s.Agent == nil {
		writeErr(w, http.StatusServiceUnavailable, "引擎未就绪")
		return
	}
	writeJSON(w, http.StatusOK, s.Agent.WorktreesView())
}

func (s *Server) handleWorktreeRemove(w http.ResponseWriter, r *http.Request) {
	if s.Agent == nil {
		writeErr(w, http.StatusServiceUnavailable, "引擎未就绪")
		return
	}
	id := r.PathValue("id")
	force := r.URL.Query().Get("force") == "1"
	if err := s.Agent.RemoveWorktree(id, force); err != nil {
		switch {
		case errors.Is(err, worktree.ErrNeedsForce):
			writeErr(w, http.StatusConflict, "%v", err)
		case errors.Is(err, worktree.ErrNoDataDir):
			writeErr(w, http.StatusServiceUnavailable, "%v", err)
		default:
			writeErr(w, http.StatusInternalServerError, "删除失败：%v", err)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task_id": id, "removed": true, "force": force})
}
