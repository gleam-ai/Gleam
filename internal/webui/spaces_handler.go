package webui

import (
	"errors"
	"net/http"

	"gleam/internal/harness/space"
)

func (s *Server) requireSpaces(w http.ResponseWriter) bool {
	if s.Agent == nil || s.Agent.Spaces == nil {
		writeErr(w, 503, "空间存储未启用")
		return false
	}
	return true
}

func writeSpaceErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, space.ErrInvalidID):
		writeErr(w, 400, "%v", err)
	case errors.Is(err, space.ErrNotFound):
		writeErr(w, 404, "空间不存在")
	case errors.Is(err, space.ErrDefaultSpace):
		writeErr(w, 400, "默认空间不能删除")
	default:
		writeErr(w, 400, "%v", err)
	}
}

func (s *Server) handleSpaceList(w http.ResponseWriter, _ *http.Request) {
	if !s.requireSpaces(w) {
		return
	}
	writeJSON(w, 200, s.Agent.SpaceView())
}

type spaceCreateBody struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func (s *Server) handleSpaceCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireSpaces(w) {
		return
	}
	var body spaceCreateBody
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "请求体无效")
		return
	}
	view, err := s.Agent.SpaceCreate(body.Name, body.Path)
	if err != nil {
		writeSpaceErr(w, err)
		return
	}
	writeJSON(w, 200, view)
}

type spaceRenameBody struct {
	Name string `json:"name"`
}

func (s *Server) handleSpaceRename(w http.ResponseWriter, r *http.Request) {
	if !s.requireSpaces(w) {
		return
	}
	id := r.PathValue("id")
	var body spaceRenameBody
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "请求体无效")
		return
	}
	view, err := s.Agent.SpaceRename(id, body.Name)
	if err != nil {
		writeSpaceErr(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) handleSpaceActivate(w http.ResponseWriter, r *http.Request) {
	if !s.requireSpaces(w) {
		return
	}
	id := r.PathValue("id")
	view, err := s.Agent.SpaceActivate(id)
	if err != nil {
		writeSpaceErr(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) handleSpaceDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireSpaces(w) {
		return
	}
	id := r.PathValue("id")
	view, err := s.Agent.SpaceDelete(id)
	if err != nil {
		writeSpaceErr(w, err)
		return
	}
	writeJSON(w, 200, view)
}
