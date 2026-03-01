package gui

import (
	"encoding/json"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"Stashbird/config"
	"Stashbird/network"
)

// logBuffer mantém as últimas N entradas de log para a API.
type logBuffer struct {
	mu      sync.Mutex
	entries []logEntry
	maxSize int
}

type logEntry struct {
	Time    string `json:"time"`
	Message string `json:"message"`
}

func newLogBuffer(size int) *logBuffer {
	return &logBuffer{maxSize: size, entries: make([]logEntry, 0, size)}
}

func (lb *logBuffer) Add(msg string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if len(lb.entries) >= lb.maxSize {
		lb.entries = lb.entries[1:]
	}
	lb.entries = append(lb.entries, logEntry{
		Time:    time.Now().Format("15:04:05"),
		Message: msg,
	})
}

func (lb *logBuffer) GetAll() []logEntry {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	cp := make([]logEntry, len(lb.entries))
	copy(cp, lb.entries)
	return cp
}

// WebServer serves the REST API and static frontend.
type WebServer struct {
	cfg        *config.AppConfig
	tailscale  *network.TailscaleManager
	syncServer *network.SyncServer
	logs       *logBuffer
	mux        *http.ServeMux
	addr       string
	mdRenderer *MarkdownRenderer
	noteIndex  *NoteIndex
}

// NewWebServer creates a new web server.
func NewWebServer(cfg *config.AppConfig, ts *network.TailscaleManager, ss *network.SyncServer, addr string) *WebServer {
	ws := &WebServer{
		cfg:        cfg,
		tailscale:  ts,
		syncServer: ss,
		logs:       newLogBuffer(500),
		mux:        http.NewServeMux(),
		addr:       addr,
		mdRenderer: NewMarkdownRenderer(),
		noteIndex:  NewNoteIndex(),
	}
	ws.registerRoutes()
	return ws
}

// AddLog adds a log entry.
func (ws *WebServer) AddLog(msg string) {
	ws.logs.Add(msg)
}

// ListenAndServe starts the HTTP server (blocking).
func (ws *WebServer) ListenAndServe() error {
	log.Printf("[WebUI] Servidor iniciado em http://%s", ws.addr)

	// Se acesso remoto está habilitado, usar middleware de filtro por IP
	var handler http.Handler = ws.mux
	if ws.cfg.WebRemoteAccess {
		handler = ws.tailscaleOnlyMiddleware(ws.mux)
		log.Println("[WebUI] Firewall ativo: somente localhost e IPs Tailscale (100.64.0.0/10) são permitidos")
	}

	return http.ListenAndServe(ws.addr, handler)
}

// tailscaleCIDR is the CGNAT range used by Tailscale (100.64.0.0/10).
var tailscaleCIDR = func() *net.IPNet {
	_, cidr, _ := net.ParseCIDR("100.64.0.0/10")
	return cidr
}()

// tailscaleOnlyMiddleware blocks all connections that are not from localhost or the Tailscale network.
// Prevents devices on the local LAN from reaching the service even when bound to 0.0.0.0.
func (ws *WebServer) tailscaleOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteIP := extractIP(r.RemoteAddr)
		if remoteIP == "" {
			http.Error(w, "403 — Acesso negado", http.StatusForbidden)
			return
		}

		ip := net.ParseIP(remoteIP)
		if ip == nil {
			http.Error(w, "403 — Acesso negado", http.StatusForbidden)
			return
		}

		if ip.IsLoopback() {
			next.ServeHTTP(w, r)
			return
		}

		if tailscaleCIDR.Contains(ip) {
			next.ServeHTTP(w, r)
			return
		}

		log.Printf("[WebUI/Firewall] Acesso BLOQUEADO de IP não-Tailscale: %s", remoteIP)
		http.Error(w, "403 — Acesso permitido apenas via Tailscale", http.StatusForbidden)
	})
}

// extractIP extracts the host from a host:port address.
func extractIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = strings.TrimSpace(remoteAddr)
	}
	return host
}

func (ws *WebServer) registerRoutes() {
	// Embedded static files (tudo dentro do binário)
	staticSub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("[WebUI] Erro ao carregar arquivos estáticos embutidos: %v", err)
	}
	fileServer := http.FileServer(http.FS(staticSub))

	ws.mux.Handle("/static/", http.StripPrefix("/static/", fileServer))
	ws.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(staticFiles, "static/index.html")
		if err != nil {
			http.Error(w, "index.html not found", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	// API
	ws.mux.HandleFunc("/api/status", ws.handleStatus)
	ws.mux.HandleFunc("/api/folders", ws.handleFolders)
	ws.mux.HandleFunc("/api/folders/files", ws.handleFolderFiles)
	ws.mux.HandleFunc("/api/search", ws.handleSearch)
	ws.mux.HandleFunc("/api/peers", ws.handlePeers)
	ws.mux.HandleFunc("/api/peers/discover", ws.handleDiscoverPeers)
	ws.mux.HandleFunc("/api/tailscale/status", ws.handleTailscaleStatus)
	ws.mux.HandleFunc("/api/tailscale/connect", ws.handleTailscaleConnect)
	ws.mux.HandleFunc("/api/tailscale/disconnect", ws.handleTailscaleDisconnect)
	ws.mux.HandleFunc("/api/tailscale/mode", ws.handleTailscaleMode)
	ws.mux.HandleFunc("/api/settings", ws.handleSettings)
	ws.mux.HandleFunc("/api/logs", ws.handleLogs)
	ws.mux.HandleFunc("/api/sync/trigger", ws.handleTriggerSync)
	ws.mux.HandleFunc("/api/browse", ws.handleBrowse)
	ws.mux.HandleFunc("/api/browse/create-folder", ws.handleBrowseCreateFolder)
	ws.mux.HandleFunc("/api/folders/create-subfolder", ws.handleCreateSubfolder)
	ws.mux.HandleFunc("/api/files/create", ws.handleCreateFile)
	ws.mux.HandleFunc("/api/files/delete", ws.handleDeleteFile)
	ws.mux.HandleFunc("/api/files/rename", ws.handleRenameFile)
	ws.mux.HandleFunc("/api/files/download-zip", ws.handleDownloadZip)
	ws.mux.HandleFunc("/api/folders/files/download", ws.handleFileDownload)

	// Markdown editor API
	ws.mux.HandleFunc("/api/files/read", ws.handleFileRead)
	ws.mux.HandleFunc("/api/files/write", ws.handleFileWrite)
	ws.mux.HandleFunc("/api/files/upload-image", ws.handleUploadImage)
	ws.mux.HandleFunc("/api/files/image", ws.handleServeImage)
	ws.mux.HandleFunc("/api/files/serve", ws.handleServeFile)
	ws.mux.HandleFunc("/api/markdown/render", ws.handleMarkdownRender)
	ws.mux.HandleFunc("/api/notes/backlinks", ws.handleBacklinks)
	ws.mux.HandleFunc("/api/notes/graph", ws.handleGraph)
	ws.mux.HandleFunc("/api/notes/list", ws.handleNotesList)
	ws.mux.HandleFunc("/api/folders/image-folder", ws.handleImageFolderConfig)
	ws.mux.HandleFunc("/api/shutdown", ws.handleShutdown)
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func readJSON(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}
