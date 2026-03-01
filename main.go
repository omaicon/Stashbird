package main

//go:generate goversioninfo -icon=stashbird.ico -o resource.syso

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"Stashbird/config"
	"Stashbird/gui"
	"Stashbird/network"
)

func main() {
	logFile := setupLogging()
	if logFile != nil {
		defer logFile.Close()
	}

	log.Println("=== Stashbird iniciando ===")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Erro ao carregar configuração: %v", err)
	}
	log.Printf("Configuração carregada: ID=%s, Nome=%s", cfg.DeviceID, cfg.DeviceName)

	tailscale := network.NewTailscaleManager(cfg.TailscaleAuthKey)

	savedMode := network.ParseMode(cfg.TailscaleMode)
	if savedMode != network.ModeAuto {
		tailscale.SetMode(savedMode)
	}

	tailscale.DetectMode()

	if warning := tailscale.GetWarning(); warning != "" {
		log.Printf("AVISO: %s", warning)
	}

	// Auto-connect via tsnet if auth key is already configured
	if tailscale.GetMode() == network.ModeTsnet && cfg.TailscaleAuthKey != "" {
		log.Println("[Tailscale] Conectando automaticamente via tsnet...")
		if err := tailscale.Connect(); err != nil {
			log.Printf("[Tailscale] Erro na conexão automática via tsnet: %v", err)
		}
	}

	syncServer := network.NewSyncServer(cfg, tailscale)

	go func() {
		if err := syncServer.Start(); err != nil {
			log.Printf("Aviso: Erro ao iniciar servidor: %v", err)
		}
	}()

	app := gui.NewApp(cfg, tailscale, syncServer)
	app.Run()

	log.Println("=== Stashbird encerrado ===")
}

// setupLogging writes logs to both stdout and a file (cross-platform).
func setupLogging() *os.File {
	logDir := config.AppDataDir()
	os.MkdirAll(logDir, 0755)

	logPath := filepath.Join(logDir, "Stashbird.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("Aviso: não foi possível criar arquivo de log: %v", err)
		return nil
	}

	log.SetOutput(io.MultiWriter(os.Stdout, f))
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	return f
}
