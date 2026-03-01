package sync

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Conflict resolution strategy:
//  1. Detect conflict by comparing hashes and timestamps
//  2. Save a local conflict copy of the file
//  3. Accept the remote version as the main file
//  4. The user can later choose which version to keep
//
// Conflict file naming:
//   <name>.sync-conflict-<YYYYMMDD-HHMMSS>-<deviceID>.<ext>

// ConflictStrategy define a estratégia de resolução de conflitos
type ConflictStrategy int

const (
	ConflictStrategyRename ConflictStrategy = iota // keep both versions (rename local as conflict copy)
	ConflictStrategyNewest                         // accept the most recent version
	ConflictStrategyOldest                         // keep the oldest version
)

// ParseConflictStrategy converte string para ConflictStrategy
func ParseConflictStrategy(s string) ConflictStrategy {
	switch strings.ToLower(s) {
	case "newest":
		return ConflictStrategyNewest
	case "oldest":
		return ConflictStrategyOldest
	default:
		return ConflictStrategyRename
	}
}

// ConflictStrategyString converte ConflictStrategy para string
func (cs ConflictStrategy) String() string {
	switch cs {
	case ConflictStrategyNewest:
		return "newest"
	case ConflictStrategyOldest:
		return "oldest"
	default:
		return "rename"
	}
}

// ConflictInfo descreve um conflito detectado
type ConflictInfo struct {
	FolderID       string    `json:"folder_id"`
	RelPath        string    `json:"rel_path"`
	LocalHash      string    `json:"local_hash"`
	RemoteHash     string    `json:"remote_hash"`
	LocalModTime   time.Time `json:"local_mod_time"`
	RemoteModTime  time.Time `json:"remote_mod_time"`
	LocalDeviceID  string    `json:"local_device_id"`
	RemoteDeviceID string    `json:"remote_device_id"`
	ConflictPath   string    `json:"conflict_path"` // caminho do arquivo de conflito criado
	DetectedAt     time.Time `json:"detected_at"`
}

// ConflictManager gerencia detecção e resolução de conflitos
type ConflictManager struct {
	strategy ConflictStrategy
}

// NewConflictManager cria um novo gerenciador de conflitos
func NewConflictManager(strategy ConflictStrategy) *ConflictManager {
	return &ConflictManager{
		strategy: strategy,
	}
}

// SetStrategy altera a estratégia de resolução
func (cm *ConflictManager) SetStrategy(strategy ConflictStrategy) {
	cm.strategy = strategy
}

// GetStrategy retorna a estratégia atual
func (cm *ConflictManager) GetStrategy() ConflictStrategy {
	return cm.strategy
}

// DetectConflict verifica se há conflito REAL entre versões local e remota.
// Um conflito real ocorre quando AMBOS os lados modificaram o arquivo
// independentemente em um intervalo de tempo próximo (< 60s).
// Se um lado é claramente mais recente que o outro, NÃO é conflito —
// é apenas um arquivo desatualizado que precisa ser sincronizado.
func (cm *ConflictManager) DetectConflict(localFile, remoteFile *FileInfo) bool {
	if localFile == nil || remoteFile == nil {
		return false
	}
	// Se os hashes são iguais, não há conflito
	if localFile.Hash == remoteFile.Hash {
		return false
	}
	// Se o arquivo local não existe (é novo), não há conflito
	if localFile.IsDeleted {
		return false
	}
	// Se o arquivo remoto é um diretório, não há conflito de conteúdo
	if remoteFile.IsDir || localFile.IsDir {
		return false
	}
	// Se algum hash está vazio, não temos informação suficiente para
	// declarar conflito — apenas sincronizar.
	if localFile.Hash == "" || remoteFile.Hash == "" {
		return false
	}
	// Hashes differ — decide if this is a real conflict (both sides edited concurrently)
	// or just one side being out-of-date.
	// Real conflict: both modified within 60 s of each other.
	// Otherwise, one side is clearly newer → normal sync.
	timeDiff := localFile.ModTime.Sub(remoteFile.ModTime)
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}
	if timeDiff > 60*time.Second {
		return false // one side is clearly newer, not a conflict
	}
	// Modificações próximas no tempo com conteúdo diferente → conflito real
	return true
}

// Resolve resolve um conflito de acordo com a estratégia configurada.
// Retorna:
//   - shouldAcceptRemote: se deve aceitar a versão remota
//   - conflictInfo: informações do conflito (nil se nenhum conflito criado)
//   - error: erro, se houver
func (cm *ConflictManager) Resolve(
	folderPath string,
	localFile, remoteFile *FileInfo,
	localDeviceID, remoteDeviceID string,
) (shouldAcceptRemote bool, conflict *ConflictInfo, err error) {

	relPath := remoteFile.RelPath
	fullPath := filepath.Join(folderPath, filepath.FromSlash(relPath))

	conflict = &ConflictInfo{
		RelPath:        relPath,
		LocalHash:      localFile.Hash,
		RemoteHash:     remoteFile.Hash,
		LocalModTime:   localFile.ModTime,
		RemoteModTime:  remoteFile.ModTime,
		LocalDeviceID:  localDeviceID,
		RemoteDeviceID: remoteDeviceID,
		DetectedAt:     time.Now(),
	}

	switch cm.strategy {
	case ConflictStrategyNewest:
		if remoteFile.ModTime.After(localFile.ModTime) {
			log.Printf("[Conflito] %s: versão remota é mais recente, aceitando", relPath)
			return true, conflict, nil
		}
		log.Printf("[Conflito] %s: versão local é mais recente, mantendo", relPath)
		return false, conflict, nil

	case ConflictStrategyOldest:
		if remoteFile.ModTime.Before(localFile.ModTime) {
			log.Printf("[Conflito] %s: versão remota é mais antiga, aceitando", relPath)
			return true, conflict, nil
		}
		log.Printf("[Conflito] %s: versão local é mais antiga, mantendo", relPath)
		return false, conflict, nil

	default: // ConflictStrategyRename
		// Salvar versão local como cópia de conflito, aceitar remota
		conflictPath, err := cm.saveConflictCopy(fullPath, relPath, localDeviceID)
		if err != nil {
			log.Printf("[Conflito] Erro ao salvar cópia de conflito de %s: %v", relPath, err)
			return true, conflict, err // accept remote even on error
		}
		conflict.ConflictPath = conflictPath
		log.Printf("[Conflito] %s: salva cópia local como %s, aceitando versão remota",
			relPath, filepath.Base(conflictPath))
		return true, conflict, nil
	}
}

// saveConflictCopy salva uma cópia de conflito do arquivo local.
// Retorna o caminho completo do arquivo de conflito criado.
func (cm *ConflictManager) saveConflictCopy(fullPath, relPath, deviceID string) (string, error) {
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return "", fmt.Errorf("arquivo original não encontrado: %s", fullPath)
	}

	dir := filepath.Dir(fullPath)
	ext := filepath.Ext(fullPath)
	base := strings.TrimSuffix(filepath.Base(fullPath), ext)

	// Truncate deviceID to 8 chars for the filename
	shortDevice := deviceID
	if len(shortDevice) > 8 {
		shortDevice = shortDevice[:8]
	}

	timestamp := time.Now().Format("20060102-150405")
	conflictName := fmt.Sprintf("%s.sync-conflict-%s-%s%s", base, timestamp, shortDevice, ext)
	conflictPath := filepath.Join(dir, conflictName)

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("erro ao ler arquivo para conflito: %w", err)
	}

	if err := os.WriteFile(conflictPath, data, 0644); err != nil {
		return "", fmt.Errorf("erro ao escrever cópia de conflito: %w", err)
	}

	log.Printf("[Conflito] Cópia de conflito criada: %s", conflictPath)
	return conflictPath, nil
}

// IsConflictFile verifica se um arquivo é uma cópia de conflito
func IsConflictFile(name string) bool {
	return strings.Contains(name, ".sync-conflict-")
}

// ListConflicts lista todos os arquivos de conflito em uma pasta
func ListConflicts(folderPath string) ([]string, error) {
	var conflicts []string

	err := filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if IsConflictFile(info.Name()) {
			relPath, _ := filepath.Rel(folderPath, path)
			conflicts = append(conflicts, filepath.ToSlash(relPath))
		}
		return nil
	})

	return conflicts, err
}

// CleanConflicts remove arquivos de conflito com mais de maxAge
func CleanConflicts(folderPath string, maxAge time.Duration) (int, error) {
	removed := 0
	cutoff := time.Now().Add(-maxAge)

	err := filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if IsConflictFile(info.Name()) && info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err == nil {
				removed++
				log.Printf("[Conflito] Removido conflito antigo: %s", path)
			}
		}
		return nil
	})

	return removed, err
}
