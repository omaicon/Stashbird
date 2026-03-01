package network

import (
	"encoding/json"
	"strings"
)

// tailscaleStatusJSON representa a saída JSON de "tailscale status --json"
type tailscaleStatusJSON struct {
	Self struct {
		ID           string   `json:"ID"`
		HostName     string   `json:"HostName"`
		DNSName      string   `json:"DNSName"`
		OS           string   `json:"OS"`
		TailscaleIPs []string `json:"TailscaleIPs"`
		Online       bool     `json:"Online"`
		Relay        string   `json:"Relay"`
	} `json:"Self"`
	Peer map[string]struct {
		ID           string   `json:"ID"`
		HostName     string   `json:"HostName"`
		DNSName      string   `json:"DNSName"`
		OS           string   `json:"OS"`
		TailscaleIPs []string `json:"TailscaleIPs"`
		Online       bool     `json:"Online"`
		Relay        string   `json:"Relay"`
		CurAddr      string   `json:"CurAddr"`
		Tags         []string `json:"Tags"`
	} `json:"Peer"`
}

// parsePeersFromStatus analisa a saída JSON do tailscale status
func parsePeersFromStatus(data []byte) ([]TailscalePeer, error) {
	var status tailscaleStatusJSON
	if err := json.Unmarshal(data, &status); err != nil {
		// Fallback: tentar parse de texto simples
		return parsePeersFromText(string(data))
	}

	var peers []TailscalePeer
	for _, p := range status.Peer {
		ip := ""
		if len(p.TailscaleIPs) > 0 {
			ip = p.TailscaleIPs[0]
		}
		peers = append(peers, TailscalePeer{
			ID:       p.ID,
			Hostname: p.HostName,
			DNSName:  p.DNSName,
			IP:       ip,
			Online:   p.Online,
			OS:       p.OS,
			Relay:    p.Relay,
			Tags:     p.Tags,
		})
	}

	return peers, nil
}

// parsePeersFromText fallback para parse de texto
func parsePeersFromText(text string) ([]TailscalePeer, error) {
	var peers []TailscalePeer
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			peer := TailscalePeer{
				IP:       parts[0],
				Hostname: parts[1],
				Online:   !strings.Contains(line, "offline"),
				OS:       extractOS(parts),
			}
			peers = append(peers, peer)
		}
	}
	return peers, nil
}

func extractOS(parts []string) string {
	for _, p := range parts {
		p = strings.ToLower(p)
		if strings.Contains(p, "windows") {
			return "windows"
		}
		if strings.Contains(p, "linux") {
			return "linux"
		}
		if strings.Contains(p, "macos") || strings.Contains(p, "darwin") {
			return "macos"
		}
	}
	return "unknown"
}
