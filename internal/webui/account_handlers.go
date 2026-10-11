package webui

import (
	"io/fs"
	"net/http"
	"path/filepath"

	"gleam/internal/desktop"
)

func (s *Server) handleAccountGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.Agent.AuthSession())
}

func (s *Server) handleAccountConfigure(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SupabaseURL     string `json:"supabase_url"`
		SupabaseAnonKey string `json:"supabase_anon_key"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "请求格式有误")
		return
	}
	if err := s.Agent.AuthConfigure(req.SupabaseURL, req.SupabaseAnonKey); err != nil {
		writeErr(w, 400, "%s", err.Error())
		return
	}
	writeJSON(w, 200, s.Agent.AuthSession())
}

func (s *Server) handleAccountSignUp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "请求格式有误")
		return
	}
	if err := s.Agent.AuthSignUp(req.Email, req.Password); err != nil {
		writeErr(w, 400, "%s", err.Error())
		return
	}
	writeJSON(w, 200, s.Agent.AuthSession())
}

func (s *Server) handleAccountSignIn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "请求格式有误")
		return
	}
	if err := s.Agent.AuthSignIn(req.Email, req.Password); err != nil {
		writeErr(w, 400, "%s", err.Error())
		return
	}
	writeJSON(w, 200, s.Agent.AuthSession())
}

func (s *Server) handleAccountSignOut(w http.ResponseWriter, _ *http.Request) {
	if err := s.Agent.AuthSignOut(); err != nil {
		writeErr(w, 500, "%s", err.Error())
		return
	}
	writeJSON(w, 200, s.Agent.AuthSession())
}

func (s *Server) handleAccountOAuth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, "请求格式有误")
		return
	}
	u, err := s.Agent.AuthOAuth(req.Provider)
	if err != nil {
		writeErr(w, 400, "%s", err.Error())
		return
	}
	_ = desktop.OpenURL(u)
	writeJSON(w, 200, map[string]any{"auth_url": u})
}

func (s *Server) handleLocalData(w http.ResponseWriter, _ *http.Request) {
	dataDir := s.Agent.DataDir()
	var files int64
	var bytes int64
	_ = filepath.WalkDir(dataDir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		files++
		if info, e := d.Info(); e == nil {
			bytes += info.Size()
		}
		return nil
	})
	writeJSON(w, 200, map[string]any{
		"data_dir":         dataDir,
		"credentials_file": s.Agent.CredentialsFile(),
		"file_count":       files,
		"total_bytes":      bytes,
	})
}
