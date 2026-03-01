package config

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// FolderConfig representa uma pasta configurada para sincronização
type FolderConfig struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Path        string `json:"path"`
	Enabled     bool   `json:"enabled"`
	SyncDelete  bool   `json:"sync_delete"`  // sincronizar exclusões
	ImageFolder string `json:"image_folder"` // pasta de imagens relativa ao root (padrão: attachments)
}

// PeerConfig representa um peer (dispositivo remoto) na rede Tailscale
type PeerConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TailscaleIP string `json:"tailscale_ip"`
	Port        int    `json:"port"`
	Enabled     bool   `json:"enabled"`
}

// AppConfig is the main application configuration.
type AppConfig struct {
	mu sync.RWMutex

	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`

	TailscaleAuthKey string `json:"tailscale_auth_key"`
	TailscaleMode    string `json:"tailscale_mode"` // "auto", "cli" or "tsnet"
	ListenPort       int    `json:"listen_port"`

	PeerRelayEnabled     bool `json:"peer_relay_enabled"`
	RelayChunkSizeMB     int  `json:"relay_chunk_size_mb"`
	LargeFileThresholdMB int  `json:"large_file_threshold_mb"`

	VersioningEnabled bool `json:"versioning_enabled"`
	MaxFileVersions   int  `json:"max_file_versions"`    // 0 = unlimited
	MaxVersionAgeDays int  `json:"max_version_age_days"` // 0 = no limit

	ConflictStrategy string `json:"conflict_strategy"` // "rename", "newest", "oldest"

	IntegrityCheckEnabled bool `json:"integrity_check_enabled"`

	Folders []FolderConfig `json:"folders"`
	Peers   []PeerConfig   `json:"peers"`

	ScanIntervalSec int `json:"scan_interval_sec"`

	// WebRemoteAccess binds to 0.0.0.0 instead of 127.0.0.1; only Tailscale IPs are allowed through.
	WebRemoteAccess bool `json:"web_remote_access"`
	WebPort         int  `json:"web_port"`

	configPath string `json:"-"`
}

// DefaultConfig retorna configuração padrão
func DefaultConfig() *AppConfig {
	return &AppConfig{
		DeviceID:              generateDeviceID(),
		DeviceName:            getHostname(),
		TailscaleMode:         "auto",
		ListenPort:            22000,
		PeerRelayEnabled:      false,
		RelayChunkSizeMB:      64,
		LargeFileThresholdMB:  500,
		VersioningEnabled:     true,
		MaxFileVersions:       10,
		MaxVersionAgeDays:     30,
		ConflictStrategy:      "rename",
		IntegrityCheckEnabled: true,
		Folders:               []FolderConfig{},
		Peers:                 []PeerConfig{},
		ScanIntervalSec:       30,
		WebRemoteAccess:       false,
		WebPort:               8384,
	}
}

// AppDataDir returns the platform-specific application data directory:
//   - Windows: %APPDATA%\SyncthingGO
//   - macOS:   ~/Library/Application Support/SyncthingGO
//   - Linux:   $XDG_CONFIG_HOME/SyncthingGO (fallback: ~/.config/SyncthingGO)
func AppDataDir() string {
	switch runtime.GOOS {
	case "windows":
		if v := os.Getenv("APPDATA"); v != "" {
			return filepath.Join(v, "SyncthingGO")
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "SyncthingGO")
		}
	default: // linux, freebsd, etc.
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "SyncthingGO")
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".config", "SyncthingGO")
		}
	}
	// Fallback
	return filepath.Join(".", "SyncthingGO")
}

// UserHome returns the current user's home directory.
func UserHome() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	if v := os.Getenv("USERPROFILE"); v != "" {
		return v
	}
	if v := os.Getenv("HOME"); v != "" {
		return v
	}
	return "."
}

// ConfigPath retorna o caminho padrão do arquivo de configuração
func ConfigPath() string {
	dir := AppDataDir()
	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "config.json")
}

// Load carrega configuração do disco
func Load() (*AppConfig, error) {
	path := ConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			cfg.configPath = path
			cfg.Save()
			return cfg, nil
		}
		return nil, err
	}

	cfg := &AppConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.configPath = path

	// Apply defaults for missing/zero fields
	if cfg.ListenPort == 0 {
		cfg.ListenPort = 22000
	}
	if cfg.RelayChunkSizeMB == 0 {
		cfg.RelayChunkSizeMB = 64
	}
	if cfg.LargeFileThresholdMB == 0 {
		cfg.LargeFileThresholdMB = 500
	}
	if cfg.ScanIntervalSec == 0 {
		cfg.ScanIntervalSec = 30
	}
	if cfg.DeviceID == "" {
		cfg.DeviceID = generateDeviceID()
	}
	if cfg.DeviceName == "" {
		cfg.DeviceName = getHostname()
	}
	if cfg.TailscaleMode == "" {
		cfg.TailscaleMode = "auto"
	}
	if cfg.ConflictStrategy == "" {
		cfg.ConflictStrategy = "rename"
	}
	if cfg.MaxFileVersions == 0 {
		cfg.MaxFileVersions = 10
	}
	if cfg.MaxVersionAgeDays == 0 {
		cfg.MaxVersionAgeDays = 30
	}
	if cfg.WebPort == 0 {
		cfg.WebPort = 8384
	}

	return cfg, nil
}

// Save salva configuração no disco
func (c *AppConfig) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	path := c.configPath
	if path == "" {
		path = ConfigPath()
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// AddFolder adiciona uma pasta para sincronização
func (c *AppConfig) AddFolder(folder FolderConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Folders = append(c.Folders, folder)
}

// RemoveFolder remove uma pasta por ID
func (c *AppConfig) RemoveFolder(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, f := range c.Folders {
		if f.ID == id {
			c.Folders = append(c.Folders[:i], c.Folders[i+1:]...)
			return
		}
	}
}

// AddPeer adiciona um peer
func (c *AppConfig) AddPeer(peer PeerConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Peers = append(c.Peers, peer)
}

// RemovePeer remove um peer por ID
func (c *AppConfig) RemovePeer(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, p := range c.Peers {
		if p.ID == id {
			c.Peers = append(c.Peers[:i], c.Peers[i+1:]...)
			return
		}
	}
}

// GetFolderImageFolder retorna a pasta de imagens configurada para uma pasta
func (c *AppConfig) GetFolderImageFolder(id string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, f := range c.Folders {
		if f.ID == id || f.Label == id {
			return f.ImageFolder
		}
	}
	return ""
}

// SetFolderImageFolder define a pasta de imagens para uma pasta de sincronização
func (c *AppConfig) SetFolderImageFolder(id, imageFolder string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, f := range c.Folders {
		if f.ID == id || f.Label == id {
			c.Folders[i].ImageFolder = imageFolder
			return
		}
	}
}

// GetFolders retorna cópia das pastas
func (c *AppConfig) GetFolders() []FolderConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]FolderConfig, len(c.Folders))
	copy(result, c.Folders)
	return result
}

// GetPeers retorna cópia dos peers
func (c *AppConfig) GetPeers() []PeerConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]PeerConfig, len(c.Peers))
	copy(result, c.Peers)
	return result
}

// UpdatePeerID updates a peer's ID after the real DeviceID is discovered via Hello handshake.
func (c *AppConfig) UpdatePeerID(oldID, newID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, p := range c.Peers {
		if p.ID == oldID {
			c.Peers[i].ID = newID
			return true
		}
	}
	return false
}

// generateDeviceID gera um ID único para o dispositivo
func generateDeviceID() string {
	hostname := getHostname()
	// Gerar ID baseado no hostname + timestamp
	return hostname + "-" + randomString(8)
}

func getHostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return name
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b)
}
