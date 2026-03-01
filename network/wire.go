package network

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"Stashbird/config"
	filesync "Stashbird/sync"

	"github.com/hashicorp/yamux"
)

// streamConn embute um stream yamux com referência à sessão pai,
// permitindo que handlers abram streams adicionais para transferências paralelas.
type streamConn struct {
	net.Conn
	session *yamux.Session
}

// getSession extrai a sessão yamux de uma conexão, se disponível.
func getSession(conn net.Conn) *yamux.Session {
	if sc, ok := conn.(*streamConn); ok {
		return sc.session
	}
	return nil
}

// yamuxConfig retorna configuração otimizada do yamux para transferências de arquivos.
func yamuxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.AcceptBacklog = 512                   // backlog de streams pendentes
	cfg.MaxStreamWindowSize = 4 * 1024 * 1024 // 4 MB window por stream (melhor throughput)
	cfg.LogOutput = io.Discard                // silenciar logs internos do yamux
	cfg.StreamOpenTimeout = 60 * time.Second
	cfg.StreamCloseTimeout = 10 * time.Minute     // 10 min para arquivos grandes
	cfg.ConnectionWriteTimeout = 60 * time.Second // 60s para writes lentos (Tailscale relay)
	cfg.KeepAliveInterval = 30 * time.Second
	cfg.EnableKeepAlive = true
	return cfg
}

// --- Funções de protocolo ---

// sendMessage envia uma mensagem pelo protocolo
func sendMessage(conn net.Conn, msg *Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// Enviar tamanho (4 bytes) + dados
	size := uint32(len(data))
	if err := binary.Write(conn, binary.BigEndian, size); err != nil {
		return err
	}
	_, err = conn.Write(data)
	return err
}

// readMessage lê uma mensagem do protocolo
func readMessage(conn net.Conn) (*Message, error) {
	// Ler tamanho (4 bytes)
	var size uint32
	if err := binary.Read(conn, binary.BigEndian, &size); err != nil {
		return nil, err
	}

	// Limitar tamanho máximo (2GB) — necessário para blockmaps de arquivos grandes
	if size > 2*1024*1024*1024 {
		return nil, fmt.Errorf("mensagem muito grande: %d bytes", size)
	}

	// Ler dados
	data := make([]byte, size)
	_, err := io.ReadFull(conn, data)
	if err != nil {
		return nil, err
	}

	msg := &Message{}
	if err := json.Unmarshal(data, msg); err != nil {
		return nil, err
	}

	return msg, nil
}

// --- Funções auxiliares ---

// findFolderPath busca o caminho da pasta pelo ID ou Label
func (ss *SyncServer) findFolderPath(folderRef string) string {
	for _, f := range ss.cfg.GetFolders() {
		if f.ID == folderRef || f.Label == folderRef {
			return f.Path
		}
	}
	return ""
}

// findFolderConfig busca a configuração da pasta pelo ID ou Label
func (ss *SyncServer) findFolderConfig(folderRef string) *config.FolderConfig {
	folders := ss.cfg.GetFolders()
	for _, f := range folders {
		if f.ID == folderRef || f.Label == folderRef {
			fc := f
			return &fc
		}
	}
	return nil
}

// findWatcherByRef busca um watcher pelo ID ou Label da pasta
// NOTA: deve ser chamado com ss.mu.RLock() ou ss.mu.Lock() ativo
func (ss *SyncServer) findWatcherByRef(folderRef string) *filesync.FolderWatcher {
	if w, ok := ss.watchers[folderRef]; ok {
		return w
	}
	for _, f := range ss.cfg.GetFolders() {
		if f.Label == folderRef || f.ID == folderRef {
			if w, ok := ss.watchers[f.ID]; ok {
				return w
			}
		}
	}
	return nil
}

// getFolderLabel retorna o label da pasta pelo ID
func (ss *SyncServer) getFolderLabel(folderID string) string {
	for _, f := range ss.cfg.GetFolders() {
		if f.ID == folderID {
			return f.Label
		}
	}
	return folderID
}

// updateWatcherIndex atualiza o índice do watcher com as informações reais
// do arquivo no disco.  Chamado após receber e gravar um arquivo remoto
// para que a próxima troca de índices não veja hash defasado e gere
// falso conflito.  Se knownHash == "" calcula o hash na hora.
func (ss *SyncServer) updateWatcherIndex(folderID, relPath, fullPath, knownHash string) {
	ss.mu.RLock()
	watcher := ss.findWatcherByRef(folderID)
	ss.mu.RUnlock()
	if watcher == nil {
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		return
	}

	hash := knownHash
	if hash == "" {
		hash, _ = filesync.ComputeFileHash(fullPath)
	}

	watcher.UpdateFile(&filesync.FileInfo{
		RelPath: relPath,
		Size:    info.Size(),
		ModTime: info.ModTime(),
		Hash:    hash,
		IsDir:   info.IsDir(),
	})
}

// tempSyncPath retorna o caminho temporário usado durante a escrita.
// Escrever neste arquivo evita conflitos com processos que mantêm o
// arquivo de destino aberto (e.g. .exe em execução no Windows).
func tempSyncPath(fullPath string) string {
	dir := filepath.Dir(fullPath)
	base := filepath.Base(fullPath)
	return filepath.Join(dir, ".syncthing."+base+".tmp")
}

// atomicRenameWithRetry tenta renomear src → dst com retries.
// No Windows um .exe em execução pode bloquear o rename; tentamos
// até 5 vezes com backoff crescente.
func atomicRenameWithRetry(src, dst string) error {
	var lastErr error
	for i := 0; i < 5; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i) * time.Second)
		}
		// No Windows, Rename falha se dst existir e estiver bloqueado.
		// Tentar remover antes (funciona se não estiver bloqueado).
		os.Remove(dst)
		if err := os.Rename(src, dst); err == nil {
			return nil
		} else {
			lastErr = err
			log.Printf("[Sync] Tentativa %d de rename falhou (%s → %s): %v",
				i+1, filepath.Base(src), filepath.Base(dst), err)
		}
	}
	return fmt.Errorf("rename falhou após 5 tentativas: %w", lastErr)
}

// adaptPathForLocalUser adapta um caminho remoto para o usuário local.
// Funciona cross-platform:
//   - Windows: C:\Users\remoto\Desktop\X → C:\Users\local\Desktop\X
//   - macOS:   /Users/remoto/Desktop/X  → /Users/local/Desktop/X
//   - Linux:   /home/remoto/Desktop/X   → /home/local/Desktop/X
func adaptPathForLocalUser(remotePath string) string {
	normalized := filepath.FromSlash(remotePath)

	// Obter usuário local (cross-platform)
	localUser := ""
	if u := os.Getenv("USERNAME"); u != "" { // Windows
		localUser = u
	} else if u := os.Getenv("USER"); u != "" { // Linux/macOS
		localUser = u
	}
	if localUser == "" {
		return remotePath
	}

	parts := strings.Split(normalized, string(filepath.Separator))

	// Detectar padrões de diretório de usuário:
	// Windows: [..., "Users", "<user>", ...]
	// macOS:   ["", "Users", "<user>", ...]
	// Linux:   ["", "home", "<user>", ...]
	for i := 0; i < len(parts)-1; i++ {
		low := strings.ToLower(parts[i])
		if low == "users" || low == "home" {
			parts[i+1] = localUser
			break
		}
	}

	return strings.Join(parts, string(filepath.Separator))
}
