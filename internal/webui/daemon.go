package webui

import (
	"encoding/json"
	"net/http"
	"os"

	"gleam/internal/harness/daemon"
)

// handleDaemonStatus returns the daemon status.
func (s *Server) handleDaemonStatus(w http.ResponseWriter, r *http.Request) {
	mgr := s.daemonManager()
	if mgr == nil {
		writeErr(w, http.StatusInternalServerError, "无法获取可执行文件路径")
		return
	}
	info, err := mgr.Status()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    info.Name,
		"state":   info.State,
		"enabled": info.Enabled,
	})
}

// handleDaemonAction performs a daemon operation (install/uninstall/start/stop).
func (s *Server) handleDaemonAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求")
		return
	}

	mgr := s.daemonManager()
	if mgr == nil {
		writeErr(w, http.StatusInternalServerError, "无法获取可执行文件路径")
		return
	}

	var err error
	switch req.Action {
	case "install":
		err = mgr.Install()
	case "uninstall":
		err = mgr.Uninstall()
	case "start":
		err = mgr.Start()
	case "stop":
		err = mgr.Stop()
	default:
		writeErr(w, http.StatusBadRequest, "未知操作")
		return
	}

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Return the latest status after the operation succeeds.
	info, err := mgr.Status()
	if err != nil || info == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"name":    info.Name,
		"state":   info.State,
		"enabled": info.Enabled,
	})
}

// daemonManager creates the daemon manager.
func (s *Server) daemonManager() *daemon.Manager {
	binPath, err := os.Executable()
	if err != nil {
		return nil
	}
	return daemon.New("gleam", s.Agent.DataDir(), binPath)
}
