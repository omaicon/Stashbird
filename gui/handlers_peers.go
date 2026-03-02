package gui

import (
	"fmt"
	"net/http"
	"time"

	"Stashbird/config"
	"Stashbird/network"
)

// GET/POST/DELETE /api/peers
func (ws *WebServer) handlePeers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		peers := ws.cfg.GetPeers()
		connStatus := ws.syncServer.GetConnectedPeerIDs()

		type peerInfo struct {
			config.PeerConfig
			Connected bool `json:"connected"`
		}
		result := make([]peerInfo, len(peers))
		for i, p := range peers {
			alive := false
			if v, ok := connStatus[p.ID]; ok {
				alive = v
			}
			result[i] = peerInfo{PeerConfig: p, Connected: alive}
		}
		jsonResponse(w, result)

	case http.MethodPost:
		var req struct {
			Name        string `json:"name"`
			TailscaleIP string `json:"tailscale_ip"`
			Port        int    `json:"port"`
			Enabled     bool   `json:"enabled"`
		}
		if err := readJSON(r, &req); err != nil {
			jsonError(w, "invalid json", 400)
			return
		}
		if req.Name == "" || req.TailscaleIP == "" {
			jsonError(w, "name and tailscale_ip required", 400)
			return
		}
		if req.Port == 0 {
			req.Port = 22000
		}
		peer := config.PeerConfig{
			ID:          fmt.Sprintf("peer-%d", time.Now().UnixNano()),
			Name:        req.Name,
			TailscaleIP: req.TailscaleIP,
			Port:        req.Port,
			Enabled:     req.Enabled,
		}
		ws.cfg.AddPeer(peer)
		ws.cfg.Save()
		go ws.syncServer.ConnectToNewPeer(peer)
		ws.AddLog("Dispositivo adicionado: " + peer.Name)
		jsonResponse(w, peer)

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			jsonError(w, "id required", 400)
			return
		}
		ws.cfg.RemovePeer(id)
		ws.cfg.Save()
		ws.AddLog("Dispositivo removido: " + id)
		jsonResponse(w, map[string]string{"status": "ok"})

	default:
		jsonError(w, "method not allowed", 405)
	}
}

// GET /api/peers/discover
func (ws *WebServer) handleDiscoverPeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", 405)
		return
	}

	peers, err := ws.tailscale.GetPeers()
	if err != nil {
		jsonError(w, "error discovering peers: "+err.Error(), 500)
		return
	}

	existingIPs := make(map[string]bool)
	for _, p := range ws.cfg.GetPeers() {
		existingIPs[p.TailscaleIP] = true
	}

	type discoverResult struct {
		network.TailscalePeer
		DisplayName  string `json:"display_name"`
		AlreadyAdded bool   `json:"already_added"`
	}

	results := make([]discoverResult, len(peers))
	for i, p := range peers {
		results[i] = discoverResult{
			TailscalePeer: p,
			DisplayName:   p.DisplayName(),
			AlreadyAdded:  existingIPs[p.IP],
		}
	}

	jsonResponse(w, results)
}
