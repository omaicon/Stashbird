package network

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"Stashbird/config"
	filesync "Stashbird/sync"

	"github.com/hashicorp/yamux"
)

// maxConcurrentTransfers limita o número de streams yamux simultâneos
// para transferência de arquivos em paralelo por sessão.
const maxConcurrentTransfers = 8

// PeerConnection representa uma conexão com um peer
type PeerConnection struct {
	mu       sync.Mutex
	conn     net.Conn       // conexão TCP bruta
	session  *yamux.Session // multiplexador yamux sobre conn
	peer     config.PeerConfig
	alive    bool
	lastPing time.Time
}

// SyncServer é o servidor P2P principal
type SyncServer struct {
	mu          sync.RWMutex
	cfg         *config.AppConfig
	listener    net.Listener
	peers       map[string]*PeerConnection
	watchers    map[string]*filesync.FolderWatcher
	blockStore  *filesync.BlockStore // índice local de blocos CDC
	onSync      func(folderID, relPath, action string)
	onStatus    func(peerID string, connected bool)
	onFolderAdd func(folder config.FolderConfig)
	running     bool
	stopChan    chan struct{}
	tailscale   *TailscaleManager

	// Integridade de dados
	conflictMgr     *filesync.ConflictManager
	versionMgr      *filesync.VersionManager
	integrityReport *filesync.IntegrityReport

	// Stats de sincronização
	bytesSent    int64
	bytesRecv    int64
	filesSynced  int64
	lastSyncTime time.Time

	// Progresso de sincronização
	pendingFiles   int64 // arquivos pendentes para receber
	completedFiles int64 // arquivos recebidos nesta sessão de sync
	isSyncing      int32 // 1 se está sincronizando, 0 se idle

	// Status por arquivo (para overlay de ícones)
	fileStatus *filesync.FileStatusTracker
}

// NewSyncServer cria um novo servidor de sincronização
func NewSyncServer(cfg *config.AppConfig, ts *TailscaleManager) *SyncServer {
	// Inicializar block store para CDC
	var bs *filesync.BlockStore
	bsPath := filesync.DefaultBlockStorePath()
	var err error
	bs, err = filesync.NewBlockStore(bsPath)
	if err != nil {
		log.Printf("[Server] Aviso: não foi possível abrir block store: %v", err)
	}

	// Inicializar gerenciadores de integridade de dados
	conflictStrategy := filesync.ParseConflictStrategy(cfg.ConflictStrategy)
	conflictMgr := filesync.NewConflictManager(conflictStrategy)

	maxAge := time.Duration(cfg.MaxVersionAgeDays) * 24 * time.Hour
	versionMgr := filesync.NewVersionManager(cfg.VersioningEnabled, cfg.MaxFileVersions, maxAge)

	integrityReport := filesync.NewIntegrityReport()

	return &SyncServer{
		cfg:             cfg,
		peers:           make(map[string]*PeerConnection),
		watchers:        make(map[string]*filesync.FolderWatcher),
		blockStore:      bs,
		stopChan:        make(chan struct{}),
		tailscale:       ts,
		conflictMgr:     conflictMgr,
		versionMgr:      versionMgr,
		integrityReport: integrityReport,
		fileStatus:      filesync.NewFileStatusTracker(),
	}
}

// GetFileStatus retorna o tracker de status de arquivos.
func (ss *SyncServer) GetFileStatus() *filesync.FileStatusTracker {
	return ss.fileStatus
}

// SetSyncCallback define callback para eventos de sincronização
func (ss *SyncServer) SetSyncCallback(cb func(folderID, relPath, action string)) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.onSync = cb
}

// SetStatusCallback define callback para status de peers
func (ss *SyncServer) SetStatusCallback(cb func(peerID string, connected bool)) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.onStatus = cb
}

// SetFolderAddCallback define callback para quando uma pasta é criada automaticamente
func (ss *SyncServer) SetFolderAddCallback(cb func(folder config.FolderConfig)) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.onFolderAdd = cb
}

// Start inicia o servidor
func (ss *SyncServer) Start() error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	var listener net.Listener
	var err error

	addr := fmt.Sprintf(":%d", ss.cfg.ListenPort)

	// Se estamos no modo tsnet e conectado, usar listener do tsnet
	if ss.tailscale != nil && ss.tailscale.GetMode() == ModeTsnet {
		connected, _ := ss.tailscale.GetStatus()
		if connected {
			listener, err = ss.tailscale.Listen("tcp", addr)
			if err != nil {
				log.Printf("[Server] Erro ao criar listener tsnet, usando listener padrão: %v", err)
				listener, err = net.Listen("tcp", addr)
			} else {
				log.Printf("[Server] Usando listener tsnet na rede Tailscale")
			}
		} else {
			log.Printf("[Server] tsnet não conectado ainda, usando listener padrão")
			listener, err = net.Listen("tcp", addr)
		}
	} else {
		listener, err = net.Listen("tcp", addr)
	}

	if err != nil {
		return fmt.Errorf("erro ao iniciar listener: %v", err)
	}

	ss.listener = listener
	ss.running = true

	log.Printf("[Server] Ouvindo em %s (DeviceID=%s, Nome=%s)", addr, ss.cfg.DeviceID, ss.cfg.DeviceName)
	log.Printf("[Server] Pastas configuradas: %d, Peers configurados: %d",
		len(ss.cfg.GetFolders()), len(ss.cfg.GetPeers()))
	for _, p := range ss.cfg.GetPeers() {
		log.Printf("[Server]   Peer: %s (IP=%s, Porta=%d, Ativo=%v)", p.Name, p.TailscaleIP, p.Port, p.Enabled)
	}

	// Aceitar conexões
	go ss.acceptLoop()

	// Iniciar watchers para todas as pastas
	ss.startWatchers()

	// Conectar aos peers conhecidos
	go ss.connectToPeers()

	// Ping loop
	go ss.pingLoop()

	return nil
}

// Stop para o servidor
func (ss *SyncServer) Stop() {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if !ss.running {
		return
	}

	close(ss.stopChan)
	ss.running = false

	if ss.listener != nil {
		ss.listener.Close()
	}

	// Fechar todas as sessões yamux e conexões
	for _, pc := range ss.peers {
		pc.mu.Lock()
		if pc.session != nil {
			pc.session.Close()
		}
		if pc.conn != nil {
			pc.conn.Close()
		}
		pc.mu.Unlock()
	}

	// Parar watchers
	for _, w := range ss.watchers {
		w.Stop()
	}

	// Fechar block store
	if ss.blockStore != nil {
		ss.blockStore.Close()
	}

	log.Printf("[Server] Parado")
}

// IsRunning retorna se o servidor está ativo
func (ss *SyncServer) IsRunning() bool {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.running
}

// GetConnectedPeerIDs retorna mapa de IDs de peers e se estão conectados
func (ss *SyncServer) GetConnectedPeerIDs() map[string]bool {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	result := make(map[string]bool)
	for id, pc := range ss.peers {
		pc.mu.Lock()
		result[id] = pc.alive
		pc.mu.Unlock()
	}
	return result
}

// GetStats retorna estatísticas de sincronização
func (ss *SyncServer) GetStats() SyncStats {
	ss.mu.RLock()
	running := ss.running
	connectedCount := 0
	for _, pc := range ss.peers {
		pc.mu.Lock()
		if pc.alive {
			connectedCount++
		}
		pc.mu.Unlock()
	}
	lastSync := ss.lastSyncTime
	ss.mu.RUnlock()

	totalPeers := len(ss.cfg.GetPeers())

	pending := atomic.LoadInt64(&ss.pendingFiles)
	completed := atomic.LoadInt64(&ss.completedFiles)
	syncing := atomic.LoadInt32(&ss.isSyncing) == 1

	var progress float64
	if pending > 0 {
		progress = float64(completed) / float64(pending)
		if progress > 1.0 {
			progress = 1.0
		}
	} else if syncing {
		progress = 1.0
	} else {
		progress = 1.0
	}

	return SyncStats{
		BytesSent:      atomic.LoadInt64(&ss.bytesSent),
		BytesReceived:  atomic.LoadInt64(&ss.bytesRecv),
		FilesSynced:    atomic.LoadInt64(&ss.filesSynced),
		LastSyncTime:   lastSync,
		ConnectedPeers: connectedCount,
		TotalPeers:     totalPeers,
		IsRunning:      running,
		PendingFiles:   pending,
		CompletedFiles: completed,
		SyncProgress:   progress,
		IsSyncing:      syncing,
	}
}

// checkSyncComplete verifica se a sincronização atual terminou
func (ss *SyncServer) checkSyncComplete() {
	pending := atomic.LoadInt64(&ss.pendingFiles)
	completed := atomic.LoadInt64(&ss.completedFiles)
	if pending > 0 && completed >= pending {
		// Sincronização completa, resetar contadores
		atomic.StoreInt64(&ss.pendingFiles, 0)
		atomic.StoreInt64(&ss.completedFiles, 0)
		atomic.StoreInt32(&ss.isSyncing, 0)
		ss.mu.Lock()
		ss.lastSyncTime = time.Now()
		ss.mu.Unlock()
		log.Printf("[Server] Sincronização completa (%d arquivos)", completed)
	}
}

// GetConflictManager retorna o gerenciador de conflitos
func (ss *SyncServer) GetConflictManager() *filesync.ConflictManager {
	return ss.conflictMgr
}

// GetVersionManager retorna o gerenciador de versões
func (ss *SyncServer) GetVersionManager() *filesync.VersionManager {
	return ss.versionMgr
}

// GetIntegrityReport retorna o relatório de integridade da sessão
func (ss *SyncServer) GetIntegrityReport() *filesync.IntegrityReport {
	return ss.integrityReport
}

// ConnectToNewPeer conecta a um peer recém-adicionado
func (ss *SyncServer) ConnectToNewPeer(peer config.PeerConfig) {
	if !peer.Enabled {
		return
	}
	ss.mu.RLock()
	_, exists := ss.peers[peer.ID]
	ss.mu.RUnlock()
	if exists {
		return
	}
	go ss.connectToPeer(peer)
}

// DisconnectPeer desconecta um peer específico
func (ss *SyncServer) DisconnectPeer(peerID string) {
	ss.mu.Lock()
	pc, exists := ss.peers[peerID]
	if exists {
		delete(ss.peers, peerID)
	}
	ss.mu.Unlock()

	if exists && pc != nil {
		pc.mu.Lock()
		pc.alive = false
		if pc.session != nil {
			pc.session.Close()
		}
		if pc.conn != nil {
			pc.conn.Close()
		}
		pc.mu.Unlock()
	}
}

// TriggerSync força uma sincronização completa com todos os peers conectados
func (ss *SyncServer) TriggerSync() {
	log.Printf("[Server] Sincronização manual iniciada")

	// Resetar contadores de progresso para nova sessão
	atomic.StoreInt64(&ss.pendingFiles, 0)
	atomic.StoreInt64(&ss.completedFiles, 0)
	atomic.StoreInt32(&ss.isSyncing, 1)

	// Coletar sessões yamux ativas
	ss.mu.RLock()
	type peerSession struct {
		name    string
		session *yamux.Session
	}
	var sessions []peerSession
	for _, pc := range ss.peers {
		pc.mu.Lock()
		if pc.alive && pc.session != nil {
			sessions = append(sessions, peerSession{pc.peer.Name, pc.session})
		}
		pc.mu.Unlock()
	}
	ss.mu.RUnlock()

	if len(sessions) == 0 {
		log.Printf("[Server] Nenhum peer conectado para sincronizar")
		return
	}

	for _, ps := range sessions {
		stream, err := ps.session.Open()
		if err != nil {
			log.Printf("[Server] Erro ao abrir stream para %s: %v", ps.name, err)
			continue
		}
		sc := &streamConn{Conn: stream, session: ps.session}
		ss.sendFullIndex(sc)
		stream.Close()
	}

	ss.mu.Lock()
	ss.lastSyncTime = time.Now()
	ss.mu.Unlock()

	log.Printf("[Server] Sincronização enviada para %d peers", len(sessions))
}

// AnnounceFolderToPeers anuncia uma nova pasta para todos os peers conectados
func (ss *SyncServer) AnnounceFolderToPeers(folder config.FolderConfig) {
	announceMsg := FolderAnnounceMessage{
		DeviceID:   ss.cfg.DeviceID,
		Label:      folder.Label,
		Path:       folder.Path,
		SyncDelete: folder.SyncDelete,
	}
	payload, err := json.Marshal(announceMsg)
	if err != nil {
		log.Printf("[Server] Erro ao codificar anúncio de pasta: %v", err)
		return
	}

	msg := &Message{
		Type:    MsgTypeFolderAnnounce,
		Payload: payload,
	}

	ss.mu.RLock()
	var sent int
	for _, pc := range ss.peers {
		pc.mu.Lock()
		alive := pc.alive
		session := pc.session
		pc.mu.Unlock()

		if alive && session != nil {
			if stream, err := session.Open(); err == nil {
				sendMessage(stream, msg)
				stream.Close()
				sent++
			}
		}
	}
	ss.mu.RUnlock()

	log.Printf("[Server] Pasta '%s' anunciada para %d peers", folder.Label, sent)
	if sent == 0 {
		log.Printf("[Server] AVISO: Nenhum peer conectado para receber anúncio da pasta '%s'. A sincronização acontecerá quando um peer conectar.", folder.Label)
	}
}

// sendFullIndex envia índice completo para um peer
func (ss *SyncServer) sendFullIndex(conn net.Conn) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	var totalFiles int
	for folderID, watcher := range ss.watchers {
		index := watcher.GetIndex()
		label := ss.getFolderLabel(folderID)
		totalFiles += len(index)
		indexMsg := IndexMessage{
			DeviceID:    ss.cfg.DeviceID,
			FolderID:    label,
			FolderLabel: label,
			Files:       index,
		}
		payload, _ := json.Marshal(indexMsg)
		if err := sendMessage(conn, &Message{
			Type:     MsgTypeIndex,
			FolderID: label,
			Payload:  payload,
		}); err != nil {
			log.Printf("[Server] Erro ao enviar índice da pasta %s: %v", label, err)
			return
		}
	}
	log.Printf("[Server] Índice completo enviado: %d pastas, %d arquivos", len(ss.watchers), totalFiles)
}

// sendFolderAnnouncementsToSession anuncia todas as pastas configuradas para
// um peer via sessão yamux, permitindo que o peer crie automaticamente pastas
// que ainda não existem no seu lado. Deve ser chamado ANTES de sendFullIndex.
func (ss *SyncServer) sendFolderAnnouncementsToSession(session *yamux.Session) {
	folders := ss.cfg.GetFolders()
	if len(folders) == 0 {
		return
	}

	var sent int
	for _, folder := range folders {
		if !folder.Enabled {
			continue
		}
		announceMsg := FolderAnnounceMessage{
			DeviceID:   ss.cfg.DeviceID,
			Label:      folder.Label,
			Path:       folder.Path,
			SyncDelete: folder.SyncDelete,
		}
		payload, err := json.Marshal(announceMsg)
		if err != nil {
			continue
		}
		stream, err := session.Open()
		if err != nil {
			log.Printf("[Server] Erro ao abrir stream para anúncio de pasta: %v", err)
			break
		}
		sendMessage(stream, &Message{
			Type:    MsgTypeFolderAnnounce,
			Payload: payload,
		})
		stream.Close()
		sent++
	}
	if sent > 0 {
		log.Printf("[Server] %d pastas anunciadas para o peer na conexão inicial", sent)
	}
}
