package gui

import (
	"net/http"

	"Stashbird/network"
)

// GET /api/tailscale/status
func (ws *WebServer) handleTailscaleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", 405)
		return
	}

	connected, ip := ws.tailscale.GetStatus()
	jsonResponse(w, map[string]interface{}{
		"connected":     connected,
		"ip":            ip,
		"mode":          ws.tailscale.GetMode().String(),
		"cli_available": ws.tailscale.IsCLIAvailable(),
		"warning":       ws.tailscale.GetWarning(),
		"config_mode":   ws.cfg.TailscaleMode,
		"has_auth_key":  ws.cfg.TailscaleAuthKey != "",
	})
}

// POST /api/tailscale/connect
func (ws *WebServer) handleTailscaleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}

	var req struct {
		AuthKey string `json:"auth_key"`
	}
	readJSON(r, &req)

	if req.AuthKey != "" {
		ws.tailscale.SetAuthKey(req.AuthKey)
		ws.cfg.TailscaleAuthKey = req.AuthKey
		ws.cfg.Save()
	}

	if err := ws.tailscale.Connect(); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	ws.AddLog("Tailscale conectado")
	connected, ip := ws.tailscale.GetStatus()
	jsonResponse(w, map[string]interface{}{"connected": connected, "ip": ip})
}

// POST /api/tailscale/disconnect
func (ws *WebServer) handleTailscaleDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}
	if err := ws.tailscale.Disconnect(); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	ws.AddLog("Tailscale desconectado")
	jsonResponse(w, map[string]string{"status": "disconnected"})
}

// POST /api/tailscale/mode
func (ws *WebServer) handleTailscaleMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, "invalid json", 400)
		return
	}
	newMode := network.ParseMode(req.Mode)
	ws.tailscale.SetMode(newMode)
	ws.cfg.TailscaleMode = req.Mode
	ws.cfg.Save()
	ws.AddLog("Modo Tailscale alterado para: " + req.Mode)
	jsonResponse(w, map[string]string{"mode": ws.tailscale.GetMode().String()})
}
