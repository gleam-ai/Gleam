package webui

import (
	"context"
	"encoding/json"
	"net/http"

	"gleam/internal/tools/hardware"
)

// ── Hardware API Endpoints ─────────────────────────────────────────────────────
//
// GET  /api/hardware/devices      List registered devices
// GET  /api/hardware/instances    List connected instances
// POST /api/hardware/connect      Connect to a device
// POST /api/hardware/disconnect   Disconnect from a device
// POST /api/hardware/send         Send command to device
// POST /api/hardware/receive      Receive from device
// GET  /api/hardware/info/{id}    Get device runtime info

func (s *Server) handleHardwareDevices(w http.ResponseWriter, r *http.Request) {
	devices := hardware.List()
	result := make([]map[string]any, 0, len(devices))
	for _, d := range devices {
		result = append(result, map[string]any{
			"id":           d.ID(),
			"name":         d.Name(),
			"description":  d.Description(),
			"capabilities": d.Capabilities(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": result})
}

func (s *Server) handleHardwareInstances(w http.ResponseWriter, r *http.Request) {
	instances := hardware.ListInstances()
	result := make([]map[string]any, 0, len(instances))
	for _, d := range instances {
		result = append(result, map[string]any{
			"id":    d.ID(),
			"name":  d.Name(),
			"state": string(d.State()),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": result})
}

func (s *Server) handleHardwareConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID string         `json:"device_id"`
		Config   map[string]any `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if req.Config == nil {
		req.Config = make(map[string]any)
	}

	if err := hardware.Connect(r.Context(), req.DeviceID, req.Config); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "connected", "device_id": req.DeviceID})
}

func (s *Server) handleHardwareDisconnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	if err := hardware.Disconnect(r.Context(), req.DeviceID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "disconnected", "device_id": req.DeviceID})
}

func (s *Server) handleHardwareSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID string `json:"device_id"`
		Command  string `json:"command"`
		Payload  any    `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	d, ok := hardware.GetInstance(req.DeviceID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "device not connected"})
		return
	}

	result, err := d.Send(r.Context(), req.Command, req.Payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func (s *Server) handleHardwareReceive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	d, ok := hardware.GetInstance(req.DeviceID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "device not connected"})
		return
	}

	result, err := d.Receive(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) handleHardwareInfo(w http.ResponseWriter, r *http.Request) {
	// Extract device ID from URL path: /api/hardware/info/{id}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing device id"})
		return
	}

	d, ok := hardware.GetInstance(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "device not connected"})
		return
	}

	info := d.Info(r.Context())
	info["device_id"] = id
	info["name"] = d.Name()
	info["state"] = string(d.State())
	writeJSON(w, http.StatusOK, info)
}

// registerHardwareRoutes adds hardware API routes to the mux.
func (s *Server) registerHardwareRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/hardware/devices", s.handleHardwareDevices)
	mux.HandleFunc("GET /api/hardware/instances", s.handleHardwareInstances)
	mux.HandleFunc("POST /api/hardware/connect", s.handleHardwareConnect)
	mux.HandleFunc("POST /api/hardware/disconnect", s.handleHardwareDisconnect)
	mux.HandleFunc("POST /api/hardware/send", s.handleHardwareSend)
	mux.HandleFunc("POST /api/hardware/receive", s.handleHardwareReceive)
	mux.HandleFunc("GET /api/hardware/info/{id}", s.handleHardwareInfo)
}

// shutdownHardware disconnects all devices on server shutdown.
func (s *Server) shutdownHardware(ctx context.Context) {
	hardware.DisconnectAll(ctx)
}
