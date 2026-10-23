package webui

import (
	"errors"
	"net/http"

	"gleam/internal/agent"
	"gleam/internal/harness/conversation"
	"gleam/pkg/types"
)

func writeConvoErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrAgentBusy):
		writeErr(w, 409, "%v", err)
	case errors.Is(err, conversation.ErrInvalidID):
		writeErr(w, 400, "%v", err)
	case errors.Is(err, conversation.ErrNotFound):
		writeErr(w, 404, "会话不存在")
	default:
		writeErr(w, 500, "会话操作失败")
	}
}

func (s *Server) requireConvos(w http.ResponseWriter) bool {
	if s.Agent == nil || s.Agent.Convos == nil {
		writeErr(w, 503, "会话存储未启用")
		return false
	}
	return true
}

func (s *Server) handleConversationList(w http.ResponseWriter, r *http.Request) {
	if !s.requireConvos(w) {
		return
	}
	items, err := s.Agent.Convos.List()
	if err != nil {
		writeErr(w, 500, "%v", err)
		return
	}
	if items == nil {
		items = []conversation.Summary{}
	}
	writeJSON(w, 200, map[string]any{"conversations": items})
}

func (s *Server) handleConversationCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireConvos(w) {
		return
	}
	var body struct {
		SpaceID string `json:"space_id"`
	}
	_ = readJSON(r, &body)
	c, err := s.Agent.NewConversation(body.SpaceID)
	if err != nil {
		writeErr(w, 500, "%v", err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) handleConversationGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireConvos(w) {
		return
	}
	id := r.PathValue("id")
	c, err := s.Agent.Convos.Get(id)
	if err != nil {
		writeConvoErr(w, err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) handleConversationActivate(w http.ResponseWriter, r *http.Request) {
	if !s.requireConvos(w) {
		return
	}
	id := r.PathValue("id")
	c, err := s.Agent.LoadConversationContext(id)
	if err != nil {
		writeConvoErr(w, err)
		return
	}
	writeJSON(w, 200, c)
}

type conversationRenameBody struct {
	Title string `json:"title"`
}

func (s *Server) handleConversationRename(w http.ResponseWriter, r *http.Request) {
	if !s.requireConvos(w) {
		return
	}
	id := r.PathValue("id")
	var body conversationRenameBody
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "请求体无效")
		return
	}
	c, err := s.Agent.Convos.Rename(id, body.Title)
	if err != nil {
		writeConvoErr(w, err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) handleConversationDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireConvos(w) {
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	for _, t := range s.tasks {
		if t.Status == types.GoalRunning && t.ConversationID == id {
			goal := types.Shorten(t.Goal, 40)
			s.mu.Unlock()
			writeErr(w, 409, "这个对话还有目标在运行（%s），先停止它再删除", goal)
			return
		}
	}
	s.mu.Unlock()
	if err := s.Agent.Convos.Delete(id); err != nil {
		writeConvoErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
