package webui

import (
	"net/http"
)

func (s *Server) handleCues(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Agent.CueView(r.URL.Query().Get("lang") == "en"))
}

func (s *Server) handleCueDismiss(w http.ResponseWriter, r *http.Request) {
	if err := s.Agent.CueDismiss(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"muted": true})
}

func (s *Server) handleCueAdopt(w http.ResponseWriter, r *http.Request) {
	if err := s.Agent.CueAdopt(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"adopted": true})
}

func (s *Server) handleCueUnsuppress(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体格式无效")
		return
	}
	if err := s.Agent.CueUnsuppress(body.Fingerprint); err != nil {
		writeErr(w, http.StatusNotFound, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cleared": true})
}
