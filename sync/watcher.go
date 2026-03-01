package sync

import (
	"encoding/hex"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	gosync "sync"
	"time"

	"Stashbird/config"

	"github.com/karrick/godirwalk"
	"github.com/zeebo/blake3"
)

// FileInfo representa informações sobre um arquivo
type FileInfo struct {
	RelPath   string    `json:"rel_path"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"mod_time"`
	Hash      string    `json:"hash"`
	IsDir     bool      `json:"is_dir"`
	IsDeleted bool      `json:"is_deleted"`
}

// FileIndex é o índice de arquivos de uma pasta
type FileIndex struct {
	mu    gosync.RWMutex
	Files map[string]*FileInfo `json:"files"`
}

// NewFileIndex cria um novo índice
func NewFileIndex() *FileIndex {
	return &FileIndex{
		Files: make(map[string]*FileInfo),
	}
}

// FolderWatcher observa mudanças em uma pasta
type FolderWatcher struct {
	folder      config.FolderConfig
	index       *FileIndex
	onChange    func(changes []FileChange)
	stopChan    chan struct{}
	running     bool
	mu          gosync.Mutex
	interval    time.Duration
	useFallback bool // true após godirwalk falhar — pula direto para filepath.WalkDir
}

// FileChange representa uma mudança em um arquivo
type FileChange struct {
	Type     ChangeType
	FileInfo *FileInfo
	FolderID string
}

// ChangeType tipo de mudança
type ChangeType int

const (
	ChangeCreated ChangeType = iota
	ChangeModified
	ChangeDeleted
)

func (ct ChangeType) String() string {
	switch ct {
	case ChangeCreated:
		return "CRIADO"
	case ChangeModified:
		return "MODIFICADO"
	case ChangeDeleted:
		return "EXCLUÍDO"
	default:
		return "DESCONHECIDO"
	}
}

// NewFolderWatcher cria um novo watcher para uma pasta
func NewFolderWatcher(folder config.FolderConfig, interval time.Duration, onChange func([]FileChange)) *FolderWatcher {
	return &FolderWatcher{
		folder:   folder,
		index:    NewFileIndex(),
		onChange: onChange,
		stopChan: make(chan struct{}),
		interval: interval,
	}
}

// Start inicia o monitoramento da pasta
func (fw *FolderWatcher) Start() {
	fw.mu.Lock()
	if fw.running {
		fw.mu.Unlock()
		return
	}
	fw.running = true
	fw.mu.Unlock()

	log.Printf("[Watcher] Monitorando pasta: %s (%s)", fw.folder.Label, fw.folder.Path)

	// Scan inicial e loop de monitoramento em goroutine separada
	// para evitar deadlock quando chamado dentro de RefreshWatchers
	go func() {
		// Scan inicial
		fw.scan()

		ticker := time.NewTicker(fw.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				fw.scan()
			case <-fw.stopChan:
				return
			}
		}
	}()
}

// Stop para o monitoramento
func (fw *FolderWatcher) Stop() {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if fw.running {
		close(fw.stopChan)
		fw.running = false
	}
}

// GetIndex retorna o índice atual
func (fw *FolderWatcher) GetIndex() map[string]*FileInfo {
	fw.index.mu.RLock()
	defer fw.index.mu.RUnlock()
	result := make(map[string]*FileInfo)
	for k, v := range fw.index.Files {
		result[k] = v
	}
	return result
}

// UpdateFile atualiza (ou insere) um arquivo no índice local imediatamente,
// sem esperar o próximo scan periódico.  Deve ser chamado após receber
// e gravar um arquivo remoto para evitar que a próxima troca de índices
// compare com informações defasadas (hash antigo) e gere falsos conflitos.
func (fw *FolderWatcher) UpdateFile(fi *FileInfo) {
	if fi == nil {
		return
	}
	fw.index.mu.Lock()
	defer fw.index.mu.Unlock()
	fw.index.Files[fi.RelPath] = fi
}

// scan faz um scan completo da pasta.
// scan faz um scan completo da pasta.
// Tenta usar godirwalk (mais rápido) e, se falhar com EOF
// (comum em pastas de armazenamento cloud como Google Drive),
// faz fallback para filepath.WalkDir da stdlib.
func (fw *FolderWatcher) scan() {
	currentFiles := make(map[string]*FileInfo)

	// Se godirwalk já falhou antes nesta pasta, ir direto para filepath.WalkDir
	// evitando tentar godirwalk toda vez (spam de log + tempo desperdiçado).
	if fw.useFallback {
		if err := fw.scanFallback(currentFiles); err != nil {
			log.Printf("[Watcher] Erro ao escanear %s (fallback): %v", fw.folder.Path, err)
			return
		}
	} else {
		err := fw.scanGodirwalk(currentFiles)
		if err != nil {
			// godirwalk falhou — ativar fallback permanente para esta pasta.
			fw.useFallback = true

			if err == io.EOF {
				log.Printf("[Watcher] godirwalk retornou EOF em %s (%d arquivos parciais), alternando para filepath.WalkDir permanentemente",
					fw.folder.Path, len(currentFiles))
			} else {
				log.Printf("[Watcher] godirwalk erro em %s: %v — alternando para filepath.WalkDir permanentemente",
					fw.folder.Path, err)
			}

			currentFiles = make(map[string]*FileInfo)
			if err2 := fw.scanFallback(currentFiles); err2 != nil {
				log.Printf("[Watcher] Erro no fallback ao escanear %s: %v", fw.folder.Path, err2)
				return
			}
			log.Printf("[Watcher] Fallback OK: %d arquivos encontrados em %s", len(currentFiles), fw.folder.Path)
		}
	}

	// Comparar com índice anterior
	changes := fw.compareIndex(currentFiles)

	// Atualizar índice
	fw.index.mu.Lock()
	fw.index.Files = currentFiles
	fw.index.mu.Unlock()

	// Notificar mudanças
	if len(changes) > 0 && fw.onChange != nil {
		for i := range changes {
			changes[i].FolderID = fw.folder.ID
		}
		fw.onChange(changes)
	}
}

// scanGodirwalk faz scan usando godirwalk (rápido, usa syscalls diretas).
func (fw *FolderWatcher) scanGodirwalk(currentFiles map[string]*FileInfo) error {
	return godirwalk.Walk(fw.folder.Path, &godirwalk.Options{
		Unsorted: true,
		Callback: func(path string, de *godirwalk.Dirent) error {
			name := de.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~") {
				if de.IsDir() {
					return godirwalk.SkipThis
				}
				return nil
			}

			if name == ".stversions" && de.IsDir() {
				return godirwalk.SkipThis
			}

			if IsConflictFile(name) {
				return nil
			}

			relPath, _ := filepath.Rel(fw.folder.Path, path)
			if relPath == "." {
				return nil
			}

			relPath = filepath.ToSlash(relPath)
			isDir := de.IsDir()

			fi := &FileInfo{
				RelPath: relPath,
				IsDir:   isDir,
			}

			if !isDir {
				info, err := os.Stat(path)
				if err != nil {
					return nil
				}
				fi.Size = info.Size()
				fi.ModTime = info.ModTime()

				hash, err := hashFile(path)
				if err == nil {
					fi.Hash = hash
				}
			}

			currentFiles[relPath] = fi
			return nil
		},
		ErrorCallback: func(path string, err error) godirwalk.ErrorAction {
			return godirwalk.SkipNode
		},
	})
}

// scanFallback faz scan usando filepath.WalkDir da stdlib.
// Mais lento que godirwalk mas compatível com todos os filesystems,
// incluindo pastas cloud (Google Drive, OneDrive, etc).
func (fw *FolderWatcher) scanFallback(currentFiles map[string]*FileInfo) error {
	return filepath.WalkDir(fw.folder.Path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Ignorar erros de permissão e continuar
			return nil
		}

		name := d.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if name == ".stversions" && d.IsDir() {
			return filepath.SkipDir
		}

		if IsConflictFile(name) {
			return nil
		}

		relPath, _ := filepath.Rel(fw.folder.Path, path)
		if relPath == "." {
			return nil
		}

		relPath = filepath.ToSlash(relPath)
		isDir := d.IsDir()

		fi := &FileInfo{
			RelPath: relPath,
			IsDir:   isDir,
		}

		if !isDir {
			info, err := d.Info()
			if err != nil {
				return nil
			}
			fi.Size = info.Size()
			fi.ModTime = info.ModTime()

			hash, err := hashFile(path)
			if err == nil {
				fi.Hash = hash
			}
		}

		currentFiles[relPath] = fi
		return nil
	})
}

// compareIndex compara o scan atual com o índice anterior
func (fw *FolderWatcher) compareIndex(current map[string]*FileInfo) []FileChange {
	fw.index.mu.RLock()
	defer fw.index.mu.RUnlock()

	var changes []FileChange

	// Verificar arquivos novos e modificados
	for path, newInfo := range current {
		oldInfo, exists := fw.index.Files[path]
		if !exists {
			changes = append(changes, FileChange{
				Type:     ChangeCreated,
				FileInfo: newInfo,
			})
		} else if !newInfo.IsDir && (newInfo.Hash != oldInfo.Hash || newInfo.Size != oldInfo.Size) {
			changes = append(changes, FileChange{
				Type:     ChangeModified,
				FileInfo: newInfo,
			})
		}
	}

	// Verificar arquivos excluídos
	for path, oldInfo := range fw.index.Files {
		if _, exists := current[path]; !exists {
			deletedInfo := *oldInfo
			deletedInfo.IsDeleted = true
			changes = append(changes, FileChange{
				Type:     ChangeDeleted,
				FileInfo: &deletedInfo,
			})
		}
	}

	return changes
}

// blockSize é o tamanho de cada bloco para hashing paralelo (1 MB).
const blockSize = 1 << 20 // 1 MiB

// smallFileThreshold define o limiar abaixo do qual o arquivo é hasheado
// sequencialmente (sem overhead de goroutines).  4 MB.
const smallFileThreshold = 4 * blockSize

// hashFile calcula o BLAKE3 de um arquivo.
// Arquivos pequenos (< 4 MB) são hasheados sequencialmente.
// Arquivos maiores são divididos em blocos de 1 MB cujos hashes são
// calculados em paralelo e depois combinados em um hash final.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}

	// Arquivo pequeno: hash direto sem paralelismo.
	if info.Size() < smallFileThreshold {
		h := blake3.New()
		if _, err := io.Copy(h, f); err != nil {
			return "", err
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}

	// Arquivo grande: hashing paralelo por blocos.
	size := info.Size()
	nBlocks := int((size + blockSize - 1) / blockSize)

	// Limitar concorrência ao número de CPUs.
	workers := runtime.NumCPU()
	if workers > nBlocks {
		workers = nBlocks
	}

	type blockHash struct {
		index int
		hash  [32]byte
		err   error
	}

	results := make([]blockHash, nBlocks)
	sem := make(chan struct{}, workers)
	var wg gosync.WaitGroup

	for i := 0; i < nBlocks; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}        // adquirir slot
			defer func() { <-sem }() // liberar slot

			// Abrir cópia própria do arquivo para leitura paralela.
			bf, err := os.Open(path)
			if err != nil {
				results[idx] = blockHash{index: idx, err: err}
				return
			}
			defer bf.Close()

			offset := int64(idx) * blockSize
			readLen := blockSize
			if offset+int64(readLen) > size {
				readLen = int(size - offset)
			}

			buf := make([]byte, readLen)
			if _, err := io.ReadFull(io.NewSectionReader(bf, offset, int64(readLen)), buf); err != nil {
				results[idx] = blockHash{index: idx, err: err}
				return
			}

			h := blake3.Sum256(buf)
			results[idx] = blockHash{index: idx, hash: h}
		}(i)
	}

	wg.Wait()

	// Combinar hashes dos blocos em ordem para produzir o hash final.
	final := blake3.New()
	for _, r := range results {
		if r.err != nil {
			return "", r.err
		}
		final.Write(r.hash[:])
	}

	return hex.EncodeToString(final.Sum(nil)), nil
}
