package gui

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"

	"Stashbird/config"
	"Stashbird/network"
)

// App é a aplicação GUI principal (Web-based).
type App struct {
	cfg        *config.AppConfig
	tailscale  *network.TailscaleManager
	syncServer *network.SyncServer
	webServer  *WebServer
}

// NewApp cria a aplicação GUI (Web-based).
func NewApp(cfg *config.AppConfig, ts *network.TailscaleManager, ss *network.SyncServer) *App {
	return &App{
		cfg:        cfg,
		tailscale:  ts,
		syncServer: ss,
	}
}

// Run inicia o servidor web e abre o navegador.
func (a *App) Run() {
	host := "127.0.0.1"
	if a.cfg.WebRemoteAccess {
		host = "0.0.0.0" // Tailscale-only middleware filters non-Tailscale IPs
	}
	port := a.cfg.WebPort
	if port == 0 {
		port = 8384
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	a.webServer = NewWebServer(a.cfg, a.tailscale, a.syncServer, addr)

	a.syncServer.SetSyncCallback(func(folderID, relPath, action string) {
		a.webServer.AddLog(fmt.Sprintf("[%s] %s — %s", folderID, relPath, action))
	})
	a.syncServer.SetStatusCallback(func(peerID string, connected bool) {
		status := "desconectado"
		if connected {
			status = "conectado"
		}
		a.webServer.AddLog(fmt.Sprintf("Peer %s: %s", peerID, status))
	})

	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	log.Printf("[GUI] Abrindo navegador em %s", url)

	if a.cfg.WebRemoteAccess {
		log.Printf("[GUI] Acesso remoto ATIVADO — somente IPs Tailscale podem acessar http://<IP-Tailscale>:%d", port)
	}

	go openBrowser(url)

	// Blocking — runs until process exits
	if err := a.webServer.ListenAndServe(); err != nil {
		log.Fatalf("[GUI] Erro no servidor web: %v", err)
	}
}

// openBrowser abre o navegador padrão do sistema.
func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = exec.Command("xdg-open", url).Start()
	}
	if err != nil {
		log.Printf("[GUI] Não foi possível abrir o navegador: %v", err)
	}
}
