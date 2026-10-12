package webui

import (
	"net/http"
	"net/http/httptest"
)

func newTokenTestServer(srv *Server) *httptest.Server {
	h := srv.Handler()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(TokenHeader) == "" {
			r.Header.Set(TokenHeader, srv.Token())
		}
		h.ServeHTTP(w, r)
	}))
}
