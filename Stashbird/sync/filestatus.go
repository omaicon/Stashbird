package sync

import (
	gosync "sync"
	"time"
)

// FileStatus representa o estado de sincronização de um arquivo.
type FileStatus int

const (
	// StatusSynced — arquivo sincronizado em ambos os lados.
	StatusSynced FileStatus = iota
	// StatusSyncing — arquivo sendo transferido (upload ou download).
	StatusSyncing
	// StatusPending — arquivo detectado mas ainda não sincronizado.
	StatusPending
	// StatusError — erro na sincronização deste arquivo.
	StatusError
	// StatusLocal — arquivo existe apenas localmente (aguardando envio).
	StatusLocal
)

// String retorna a representação textual do status.
func (s FileStatus) String() string {
	switch s {
	case StatusSynced:
		return "Sincronizado"
	case StatusSyncing:
		return "Sincronizando"
	case StatusPending:
		return "Pendente"
	case StatusError:
		return "Erro"
	case StatusLocal:
		return "Somente local"
	default:
		return "Desconhecido"
	}
}

// Icon retorna o ícone unicode correspondente ao status.
func (s FileStatus) Icon() string {
	switch s {
	case StatusSynced:
		return "✅"
	case StatusSyncing:
		return "🔄"
	case StatusPending:
		return "⏳"
	case StatusError:
		return "❌"
	case StatusLocal:
		return "📄"
	default:
		return "❓"
	}
}

// FileStatusEntry contém informações detalhadas sobre o status de um arquivo.
type FileStatusEntry struct {
	RelPath   string
	FolderID  string
	Status    FileStatus
	Size      int64
	Progress  float64 // 0.0 a 1.0 para arquivos em transferência
	UpdatedAt time.Time
	Error     string // mensagem de erro, se houver
}

// FileStatusTracker mantém o status de sincronização de todos os arquivos.
type FileStatusTracker struct {
	mu       gosync.RWMutex
	files    map[string]*FileStatusEntry // key: "folderID/relPath"
	onChange func()                      // callback para atualizar GUI
}

// NewFileStatusTracker cria um novo tracker de status.
func NewFileStatusTracker() *FileStatusTracker {
	return &FileStatusTracker{
		files: make(map[string]*FileStatusEntry),
	}
}

// SetOnChange define o callback chamado quando o status de qualquer arquivo muda.
func (t *FileStatusTracker) SetOnChange(fn func()) {
	t.mu.Lock()
	t.onChange = fn
	t.mu.Unlock()
}

func (t *FileStatusTracker) key(folderID, relPath string) string {
	return folderID + "/" + relPath
}

func (t *FileStatusTracker) notify() {
	if t.onChange != nil {
		t.onChange()
	}
}

// SetStatus define o status de um arquivo.
func (t *FileStatusTracker) SetStatus(folderID, relPath string, status FileStatus, size int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := t.key(folderID, relPath)
	entry, ok := t.files[k]
	if !ok {
		entry = &FileStatusEntry{
			RelPath:  relPath,
			FolderID: folderID,
		}
		t.files[k] = entry
	}
	entry.Status = status
	entry.Size = size
	entry.UpdatedAt = time.Now()
	if status != StatusError {
		entry.Error = ""
	}
	if status == StatusSynced {
		entry.Progress = 1.0
	}
	t.notify()
}

// SetProgress atualiza o progresso de um arquivo em transferência.
func (t *FileStatusTracker) SetProgress(folderID, relPath string, progress float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := t.key(folderID, relPath)
	if entry, ok := t.files[k]; ok {
		entry.Progress = progress
		entry.UpdatedAt = time.Now()
		t.notify()
	}
}

// SetError marca um arquivo como tendo erro na sincronização.
func (t *FileStatusTracker) SetError(folderID, relPath string, errMsg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := t.key(folderID, relPath)
	entry, ok := t.files[k]
	if !ok {
		entry = &FileStatusEntry{
			RelPath:  relPath,
			FolderID: folderID,
		}
		t.files[k] = entry
	}
	entry.Status = StatusError
	entry.Error = errMsg
	entry.UpdatedAt = time.Now()
	t.notify()
}

// GetStatus retorna o status de um arquivo.
func (t *FileStatusTracker) GetStatus(folderID, relPath string) *FileStatusEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.files[t.key(folderID, relPath)]
}

// GetAllByFolder retorna todos os arquivos de uma pasta, ordenados por status.
func (t *FileStatusTracker) GetAllByFolder(folderID string) []*FileStatusEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []*FileStatusEntry
	prefix := folderID + "/"
	for k, entry := range t.files {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			// Copiar para evitar race condition
			cpy := *entry
			result = append(result, &cpy)
		}
	}
	return result
}

// GetAll retorna todos os arquivos rastreados.
func (t *FileStatusTracker) GetAll() []*FileStatusEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]*FileStatusEntry, 0, len(t.files))
	for _, entry := range t.files {
		cpy := *entry
		result = append(result, &cpy)
	}
	return result
}

// GetCounts retorna contagem por status.
func (t *FileStatusTracker) GetCounts() map[FileStatus]int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	counts := make(map[FileStatus]int)
	for _, entry := range t.files {
		counts[entry.Status]++
	}
	return counts
}

// Remove remove um arquivo do tracker.
func (t *FileStatusTracker) Remove(folderID, relPath string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.files, t.key(folderID, relPath))
	t.notify()
}

// Clear limpa todos os status de uma pasta.
func (t *FileStatusTracker) Clear(folderID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	prefix := folderID + "/"
	for k := range t.files {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			delete(t.files, k)
		}
	}
	t.notify()
}

// MarkAllSynced marca todos os arquivos de uma pasta como sincronizados.
func (t *FileStatusTracker) MarkAllSynced(folderID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	prefix := folderID + "/"
	for k, entry := range t.files {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			if entry.Status == StatusPending || entry.Status == StatusSyncing {
				entry.Status = StatusSynced
				entry.Progress = 1.0
				entry.UpdatedAt = time.Now()
			}
		}
	}
	t.notify()
}
