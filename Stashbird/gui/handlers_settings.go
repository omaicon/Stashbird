package gui

import (
	"net/http"
	"strings"
)

// GET/PUT /api/settings
func (ws *WebServer) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, map[string]interface{}{
			"device_id":            ws.cfg.DeviceID,
			"device_name":          ws.cfg.DeviceName,
			"listen_port":          ws.cfg.ListenPort,
			"tailscale_mode":       ws.cfg.TailscaleMode,
			"tailscale_auth_key":   maskKey(ws.cfg.TailscaleAuthKey),
			"peer_relay_enabled":   ws.cfg.PeerRelayEnabled,
			"relay_chunk_size_mb":  ws.cfg.RelayChunkSizeMB,
			"large_file_threshold": ws.cfg.LargeFileThresholdMB,
			"versioning_enabled":   ws.cfg.VersioningEnabled,
			"max_file_versions":    ws.cfg.MaxFileVersions,
			"max_version_age_days": ws.cfg.MaxVersionAgeDays,
			"conflict_strategy":    ws.cfg.ConflictStrategy,
			"integrity_check":      ws.cfg.IntegrityCheckEnabled,
			"scan_interval_sec":    ws.cfg.ScanIntervalSec,
			"web_remote_access":    ws.cfg.WebRemoteAccess,
			"web_port":             ws.cfg.WebPort,
		})

	case http.MethodPut:
		var req map[string]interface{}
		if err := readJSON(r, &req); err != nil {
			jsonError(w, "invalid json", 400)
			return
		}

		if v, ok := req["tailscale_auth_key"]; ok {
			if s, ok := v.(string); ok && s != "" && !strings.HasPrefix(s, "****") {
				ws.cfg.TailscaleAuthKey = s
				ws.tailscale.SetAuthKey(s)
			}
		}
		if v, ok := req["conflict_strategy"]; ok {
			ws.cfg.ConflictStrategy = v.(string)
		}
		if v, ok := req["versioning_enabled"]; ok {
			ws.cfg.VersioningEnabled = v.(bool)
		}
		if v, ok := req["max_file_versions"]; ok {
			ws.cfg.MaxFileVersions = int(v.(float64))
		}
		if v, ok := req["scan_interval_sec"]; ok {
			ws.cfg.ScanIntervalSec = int(v.(float64))
		}
		if v, ok := req["peer_relay_enabled"]; ok {
			ws.cfg.PeerRelayEnabled = v.(bool)
		}
		if v, ok := req["integrity_check"]; ok {
			ws.cfg.IntegrityCheckEnabled = v.(bool)
		}
		if v, ok := req["web_remote_access"]; ok {
			ws.cfg.WebRemoteAccess = v.(bool)
		}
		if v, ok := req["web_port"]; ok {
			ws.cfg.WebPort = int(v.(float64))
		}

		ws.cfg.Save()
		ws.AddLog("Configurações atualizadas")
		jsonResponse(w, map[string]string{"status": "ok"})

	default:
		jsonError(w, "method not allowed", 405)
	}
}

// GET /api/logs
func (ws *WebServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", 405)
		return
	}
	jsonResponse(w, ws.logs.GetAll())
}

// POST /api/sync/trigger
func (ws *WebServer) handleTriggerSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}
	go ws.syncServer.TriggerSync()
	ws.AddLog("Sincronização manual disparada")
	jsonResponse(w, map[string]string{"status": "triggered"})
}
