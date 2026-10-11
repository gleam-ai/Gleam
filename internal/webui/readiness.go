package webui

import "net/http"

func (s *Server) handleReadiness(w http.ResponseWriter, _ *http.Request) {
	if s.Agent == nil {
		writeErr(w, 503, "引擎尚未就绪")
		return
	}
	rep := s.Agent.Readiness()
	writeJSON(w, 200, map[string]any{
		"report":  rep,
		"summary": rep.SummaryLine(),
	})
}
