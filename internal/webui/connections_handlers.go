package webui

import (
	"net/http"
)

func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Agent.ConnectionView(s.BindAddr, r.URL.Query().Get("lang") == "en"))
}
