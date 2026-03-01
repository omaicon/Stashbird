package network

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"Stashbird/config"

	"tailscale.com/tsnet"
)

// TailscaleMode define o modo de operação do Tailscale
type TailscaleMode int

const (
	ModeNone  TailscaleMode = iota // Não detectado
	ModeCLI                        // Usando CLI do Tailscale instalado
	ModeTsnet                      // Usando biblioteca tsnet embutida
	ModeAuto                       // Detecção automática
)

// String retorna descrição do modo
func (m TailscaleMode) String() string {
	switch m {
	case ModeCLI:
		return "CLI (Tailscale instalado)"
	case ModeTsnet:
		return "tsnet (biblioteca embutida)"
	case ModeAuto:
		return "Automático"
	default:
		return "Não detectado"
	}
}

// ParseMode converte string de configuração para TailscaleMode
func ParseMode(s string) TailscaleMode {
	switch strings.ToLower(s) {
	case "cli":
		return ModeCLI
	case "tsnet":
		return ModeTsnet
	default:
		return ModeAuto
	}
}

// TailscaleManager gerencia a integração com o Tailscale
type TailscaleManager struct {
	mu          sync.RWMutex
	authKey     string
	connected   bool
	localIP     string
	peerRelayOn bool
	statusCb    func(connected bool, ip string)

	// Modo de operação
	mode         TailscaleMode
	cliAvailable bool // se o CLI foi detectado na máquina
	warningMsg   string

	// tsnet server (quando CLI não está disponível)
	tsnetServer *tsnet.Server
}

// NewTailscaleManager cria um novo gerenciador Tailscale
func NewTailscaleManager(authKey string) *TailscaleManager {
	return &TailscaleManager{
		authKey: authKey,
	}
}

// DetectMode detecta se o Tailscale CLI está instalado e define o modo de operação.
// preferredMode pode ser "auto", "cli" ou "tsnet".
// No modo "auto", usa CLI se disponível, senão tsnet.
func (tm *TailscaleManager) DetectMode() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Verificar disponibilidade do CLI
	tsPath := tm.getTailscalePath()
	tm.cliAvailable = tsPath != ""

	if tm.cliAvailable {
		log.Printf("[Tailscale] CLI detectado em: %s", tsPath)
	} else {
		log.Printf("[Tailscale] CLI não encontrado na máquina.")
	}

	// Se o modo já foi definido manualmente (não é ModeNone/ModeAuto), respeitar
	if tm.mode == ModeCLI || tm.mode == ModeTsnet {
		log.Printf("[Tailscale] Modo manual configurado: %s", tm.mode.String())
		tm.updateWarning()
		return
	}

	// Modo automático: escolher com base na disponibilidade
	if tm.cliAvailable {
		tm.mode = ModeCLI
	} else {
		tm.mode = ModeTsnet
	}

	tm.updateWarning()
	log.Printf("[Tailscale] Modo definido automaticamente: %s", tm.mode.String())
}

// SetMode define manualmente o modo de operação.
// Se alterar de CLI para tsnet (ou vice-versa), desconecta da sessão atual.
func (tm *TailscaleManager) SetMode(mode TailscaleMode) {
	tm.mu.Lock()
	oldMode := tm.mode

	if mode == ModeAuto {
		// Resolver auto para o modo concreto
		if tm.cliAvailable {
			mode = ModeCLI
		} else {
			mode = ModeTsnet
		}
	}

	tm.mode = mode
	tm.updateWarning()
	tm.mu.Unlock()

	log.Printf("[Tailscale] Modo alterado para: %s", mode.String())

	// Se mudou o modo e estava conectado, desconectar
	if oldMode != mode && oldMode != ModeNone {
		switch oldMode {
		case ModeCLI:
			_ = tm.disconnectCLI()
		case ModeTsnet:
			_ = tm.disconnectTsnet()
		}
	}
}

// updateWarning atualiza a mensagem de aviso baseada no modo e disponibilidade do CLI
// NOTA: deve ser chamado com tm.mu.Lock() ativo
func (tm *TailscaleManager) updateWarning() {
	if tm.mode == ModeTsnet && !tm.cliAvailable {
		tm.warningMsg = "Não detectamos Tailscale instalado. Instale ou configure sua AuthKey para usarmos diretamente a biblioteca tsnet sem precisar de instalação."
	} else if tm.mode == ModeTsnet && tm.cliAvailable {
		tm.warningMsg = "Modo tsnet selecionado manualmente. A biblioteca embutida será usada. Configure sua AuthKey para conectar."
	} else if tm.mode == ModeCLI && !tm.cliAvailable {
		tm.warningMsg = "Modo CLI selecionado mas o Tailscale CLI não foi encontrado. Instale o Tailscale ou troque para modo tsnet."
	} else {
		tm.warningMsg = ""
	}
}

// GetMode retorna o modo atual de operação
func (tm *TailscaleManager) GetMode() TailscaleMode {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.mode
}

// GetWarning retorna a mensagem de aviso (vazia se nenhum aviso)
func (tm *TailscaleManager) GetWarning() string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.warningMsg
}

// IsCLIAvailable retorna se o Tailscale CLI está disponível na máquina
func (tm *TailscaleManager) IsCLIAvailable() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.cliAvailable
}

// SetStatusCallback define callback para mudanças de status
func (tm *TailscaleManager) SetStatusCallback(cb func(connected bool, ip string)) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.statusCb = cb
}

// Connect conecta ao Tailscale usando o modo detectado
func (tm *TailscaleManager) Connect() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.authKey == "" {
		return fmt.Errorf("tailscale authkey não configurada")
	}

	switch tm.mode {
	case ModeCLI:
		return tm.connectCLI()
	case ModeTsnet:
		return tm.connectTsnet()
	default:
		return fmt.Errorf("modo Tailscale não detectado. Execute DetectMode() primeiro")
	}
}

// connectCLI conecta usando CLI do Tailscale instalado
// NOTA: deve ser chamado com tm.mu.Lock() ativo
func (tm *TailscaleManager) connectCLI() error {
	tsPath := tm.getTailscalePath()
	if tsPath == "" {
		return fmt.Errorf("tailscale não encontrado")
	}

	cmd := exec.Command(tsPath, "up", "--authkey="+tm.authKey, "--reset")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[Tailscale/CLI] Erro ao conectar: %s - %v", string(output), err)
		return fmt.Errorf("erro ao conectar: %v - %s", err, string(output))
	}

	log.Printf("[Tailscale/CLI] Conectado com sucesso")

	ip, err := tm.getLocalIPCLI()
	if err != nil {
		log.Printf("[Tailscale/CLI] Aviso: não foi possível obter IP: %v", err)
	} else {
		tm.localIP = ip
	}

	tm.connected = true

	if tm.statusCb != nil {
		tm.statusCb(true, tm.localIP)
	}

	return nil
}

// connectTsnet conecta usando biblioteca tsnet embutida
// NOTA: deve ser chamado com tm.mu.Lock() ativo
func (tm *TailscaleManager) connectTsnet() error {
	if tm.tsnetServer != nil {
		// Já existe um servidor tsnet em execução
		return nil
	}

	// Diretório para estado do tsnet (cross-platform)
	tsnetDir := filepath.Join(config.AppDataDir(), "tsnet-state")
	os.MkdirAll(tsnetDir, 0755)

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "syncthinggo"
	}

	tm.tsnetServer = &tsnet.Server{
		Hostname: hostname + "-syncthinggo",
		AuthKey:  tm.authKey,
		Dir:      tsnetDir,
	}

	log.Printf("[Tailscale/tsnet] Conectando à rede Tailscale...")

	status, err := tm.tsnetServer.Up(context.Background())
	if err != nil {
		tm.tsnetServer.Close()
		tm.tsnetServer = nil
		log.Printf("[Tailscale/tsnet] Erro ao conectar: %v", err)
		return fmt.Errorf("erro ao conectar via tsnet: %v", err)
	}

	// Obter IP atribuído pelo Tailscale
	if len(status.TailscaleIPs) > 0 {
		tm.localIP = status.TailscaleIPs[0].String()
	}

	tm.connected = true
	log.Printf("[Tailscale/tsnet] Conectado com sucesso (IP: %s)", tm.localIP)

	if tm.statusCb != nil {
		tm.statusCb(true, tm.localIP)
	}

	return nil
}

// Disconnect desconecta do Tailscale
func (tm *TailscaleManager) Disconnect() error {
	tm.mu.RLock()
	mode := tm.mode
	tm.mu.RUnlock()

	switch mode {
	case ModeCLI:
		return tm.disconnectCLI()
	case ModeTsnet:
		return tm.disconnectTsnet()
	default:
		return nil
	}
}

func (tm *TailscaleManager) disconnectCLI() error {
	tsPath := tm.getTailscalePath()
	if tsPath == "" {
		return fmt.Errorf("tailscale não encontrado")
	}

	cmd := exec.Command(tsPath, "down")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("erro ao desconectar: %v - %s", err, string(output))
	}

	tm.mu.Lock()
	tm.connected = false
	tm.localIP = ""
	tm.mu.Unlock()

	if tm.statusCb != nil {
		tm.statusCb(false, "")
	}

	return nil
}

func (tm *TailscaleManager) disconnectTsnet() error {
	tm.mu.Lock()
	if tm.tsnetServer != nil {
		tm.tsnetServer.Close()
		tm.tsnetServer = nil
	}
	tm.connected = false
	tm.localIP = ""
	cb := tm.statusCb
	tm.mu.Unlock()

	if cb != nil {
		cb(false, "")
	}

	return nil
}

// GetStatus retorna o status atual.
// No modo CLI, verifica o status real do Tailscale instalado.
func (tm *TailscaleManager) GetStatus() (connected bool, ip string) {
	tm.mu.RLock()
	mode := tm.mode
	internalConn := tm.connected
	internalIP := tm.localIP
	tm.mu.RUnlock()

	// Se já está marcado como conectado internamente, retornar direto.
	if internalConn {
		return true, internalIP
	}

	// No modo CLI, verificar o status real do Tailscale instalado
	// (o CLI pode estar conectado mesmo sem chamar Connect()).
	if mode == ModeCLI {
		cliIP, err := tm.getLocalIPCLI()
		if err == nil && cliIP != "" {
			// CLI está conectado — atualizar estado interno
			tm.mu.Lock()
			tm.connected = true
			tm.localIP = cliIP
			tm.mu.Unlock()
			return true, cliIP
		}
	}

	return false, ""
}

// CheckStatus verifica o status atual do Tailscale
func (tm *TailscaleManager) CheckStatus() error {
	tm.mu.RLock()
	mode := tm.mode
	tm.mu.RUnlock()

	switch mode {
	case ModeCLI:
		return tm.checkStatusCLI()
	case ModeTsnet:
		return tm.checkStatusTsnet()
	default:
		return fmt.Errorf("modo não detectado")
	}
}

func (tm *TailscaleManager) checkStatusCLI() error {
	tsPath := tm.getTailscalePath()
	if tsPath == "" {
		return fmt.Errorf("tailscale não encontrado")
	}

	cmd := exec.Command(tsPath, "status")
	output, err := cmd.CombinedOutput()
	if err != nil {
		tm.mu.Lock()
		tm.connected = false
		tm.mu.Unlock()
		return fmt.Errorf("tailscale não conectado: %v", err)
	}

	outputStr := string(output)
	if strings.Contains(outputStr, "stopped") || strings.Contains(outputStr, "not running") {
		tm.mu.Lock()
		tm.connected = false
		tm.mu.Unlock()
		return fmt.Errorf("tailscale parado")
	}

	ip, _ := tm.getLocalIPCLI()
	tm.mu.Lock()
	tm.connected = true
	tm.localIP = ip
	tm.mu.Unlock()

	return nil
}

func (tm *TailscaleManager) checkStatusTsnet() error {
	tm.mu.RLock()
	srv := tm.tsnetServer
	tm.mu.RUnlock()

	if srv == nil {
		tm.mu.Lock()
		tm.connected = false
		tm.mu.Unlock()
		return fmt.Errorf("tsnet não iniciado. Configure a AuthKey e conecte primeiro")
	}

	lc, err := srv.LocalClient()
	if err != nil {
		tm.mu.Lock()
		tm.connected = false
		tm.mu.Unlock()
		return fmt.Errorf("erro ao obter client tsnet: %v", err)
	}

	status, err := lc.Status(context.Background())
	if err != nil {
		tm.mu.Lock()
		tm.connected = false
		tm.mu.Unlock()
		return fmt.Errorf("tsnet não conectado: %v", err)
	}

	tm.mu.Lock()
	if len(status.TailscaleIPs) > 0 {
		tm.localIP = status.TailscaleIPs[0].String()
	}
	tm.connected = true
	tm.mu.Unlock()

	return nil
}

// GetPeers retorna a lista de peers do Tailscale
func (tm *TailscaleManager) GetPeers() ([]TailscalePeer, error) {
	tm.mu.RLock()
	mode := tm.mode
	tm.mu.RUnlock()

	switch mode {
	case ModeCLI:
		return tm.getPeersCLI()
	case ModeTsnet:
		return tm.getPeersTsnet()
	default:
		return nil, fmt.Errorf("modo não detectado")
	}
}

func (tm *TailscaleManager) getPeersCLI() ([]TailscalePeer, error) {
	tsPath := tm.getTailscalePath()
	if tsPath == "" {
		return nil, fmt.Errorf("tailscale não encontrado")
	}

	cmd := exec.Command(tsPath, "status", "--json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("erro ao obter status: %v", err)
	}

	return parsePeersFromStatus(output)
}

func (tm *TailscaleManager) getPeersTsnet() ([]TailscalePeer, error) {
	tm.mu.RLock()
	srv := tm.tsnetServer
	tm.mu.RUnlock()

	if srv == nil {
		return nil, fmt.Errorf("tsnet não conectado")
	}

	lc, err := srv.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("erro ao obter client local tsnet: %v", err)
	}

	status, err := lc.Status(context.Background())
	if err != nil {
		return nil, fmt.Errorf("erro ao obter status dos peers: %v", err)
	}

	var peers []TailscalePeer
	for _, p := range status.Peer {
		ip := ""
		if len(p.TailscaleIPs) > 0 {
			ip = p.TailscaleIPs[0].String()
		}
		var tags []string
		if p.Tags != nil {
			for i := range p.Tags.Len() {
				tags = append(tags, p.Tags.At(i))
			}
		}
		peers = append(peers, TailscalePeer{
			ID:       string(p.ID),
			Hostname: p.HostName,
			DNSName:  p.DNSName,
			IP:       ip,
			Online:   p.Online,
			OS:       p.OS,
			Relay:    p.Relay,
			Tags:     tags,
		})
	}

	return peers, nil
}

// Listen cria um net.Listener na rede Tailscale (modo tsnet).
// Retorna erro se não estiver no modo tsnet ou se o servidor não estiver conectado.
func (tm *TailscaleManager) Listen(network string, addr string) (net.Listener, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if tm.mode != ModeTsnet || tm.tsnetServer == nil {
		return nil, fmt.Errorf("tsnet não disponível")
	}

	return tm.tsnetServer.Listen(network, addr)
}

// Dial conecta a um endereço na rede Tailscale (modo tsnet).
// Retorna erro se não estiver no modo tsnet ou se o servidor não estiver conectado.
func (tm *TailscaleManager) Dial(ctx context.Context, network string, addr string) (net.Conn, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if tm.mode != ModeTsnet || tm.tsnetServer == nil {
		return nil, fmt.Errorf("tsnet não disponível")
	}

	return tm.tsnetServer.Dial(ctx, network, addr)
}

// EnablePeerRelay ativa Peer Relay para transferências grandes
func (tm *TailscaleManager) EnablePeerRelay(enable bool) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.peerRelayOn = enable
	log.Printf("[Tailscale] Peer Relay: %v", enable)
	return nil
}

// IsPeerRelayEnabled retorna se o peer relay está ativo
func (tm *TailscaleManager) IsPeerRelayEnabled() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.peerRelayOn
}

// SetAuthKey atualiza a authkey
func (tm *TailscaleManager) SetAuthKey(key string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.authKey = key
}

// getLocalIPCLI obtém o IP do Tailscale via CLI
func (tm *TailscaleManager) getLocalIPCLI() (string, error) {
	tsPath := tm.getTailscalePath()
	if tsPath == "" {
		return "", fmt.Errorf("tailscale não encontrado")
	}

	cmd := exec.Command(tsPath, "ip", "-4")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	ip := strings.TrimSpace(string(output))
	return ip, nil
}

// getTailscalePath retorna o caminho do executável do Tailscale
func (tm *TailscaleManager) getTailscalePath() string {
	if runtime.GOOS == "windows" {
		// Caminhos comuns no Windows
		paths := []string{
			`C:\Program Files\Tailscale\tailscale.exe`,
			`C:\Program Files (x86)\Tailscale\tailscale.exe`,
		}
		for _, p := range paths {
			if _, err := exec.LookPath(p); err == nil {
				return p
			}
		}
		// Tentar no PATH
		if p, err := exec.LookPath("tailscale"); err == nil {
			return p
		}
		if p, err := exec.LookPath("tailscale.exe"); err == nil {
			return p
		}
	}

	// Linux/Mac
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p
	}

	return ""
}

// TailscalePeer representa um peer na rede Tailscale
type TailscalePeer struct {
	ID       string   `json:"id"`
	Hostname string   `json:"hostname"`
	DNSName  string   `json:"dns_name"`
	IP       string   `json:"ip"`
	Online   bool     `json:"online"`
	OS       string   `json:"os"`
	Relay    string   `json:"relay"` // DERP relay usado
	Tags     []string `json:"tags,omitempty"`
}

// DisplayName retorna o melhor nome de exibição para o peer.
// Prioridade: DNSName (primeiro segmento) > HostName (se não for "localhost") > IP
func (p TailscalePeer) DisplayName() string {
	// Extrair nome do DNSName (ex: "motorola-moto-g75-5g.tail3d5df1.ts.net." -> "motorola-moto-g75-5g")
	if p.DNSName != "" {
		name := strings.TrimSuffix(p.DNSName, ".")
		if idx := strings.Index(name, "."); idx > 0 {
			name = name[:idx]
		}
		if name != "" && strings.ToLower(name) != "localhost" {
			return name
		}
	}
	// HostName, se não for "localhost" ou vazio
	if p.Hostname != "" && strings.ToLower(p.Hostname) != "localhost" {
		return p.Hostname
	}
	// Último recurso: IP
	if p.IP != "" {
		return p.IP
	}
	return "desconhecido"
}
