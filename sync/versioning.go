package sync

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	gosync "sync"
	"time"
)

// File versioning: keeps previous copies of synced files under .stversions/.
// Before a file is overwritten, the current version is moved to:
//   .stversions/<relPath>~<YYYYMMDD-HHMMSS>.<ext>
//
// Configurable:
//   - MaxVersions: max versions per file (0 = unlimited)
//   - MaxAge:      max version age      (0 = no limit)
//   - Enabled:     on/off

const versionDir = ".stversions"

// VersionInfo describes a saved version of a file.
type VersionInfo struct {
	RelPath     string    `json:"rel_path"`
	VersionPath string    `json:"version_path"`
	ModTime     time.Time `json:"mod_time"`
	Size        int64     `json:"size"`
	VersionTime time.Time `json:"version_time"`
}

// VersionManager gerencia o versionamento de arquivos
type VersionManager struct {
	mu          gosync.Mutex
	enabled     bool
	maxVersions int           // 0 = ilimitado
	maxAge      time.Duration // 0 = sem limite
}

// NewVersionManager cria um novo gerenciador de versões
func NewVersionManager(enabled bool, maxVersions int, maxAge time.Duration) *VersionManager {
	return &VersionManager{
		enabled:     enabled,
		maxVersions: maxVersions,
		maxAge:      maxAge,
	}
}

// IsEnabled retorna se o versionamento está ativo
func (vm *VersionManager) IsEnabled() bool {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	return vm.enabled
}

// SetEnabled ativa ou desativa o versionamento
func (vm *VersionManager) SetEnabled(enabled bool) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.enabled = enabled
}

// SetMaxVersions define o número máximo de versões por arquivo
func (vm *VersionManager) SetMaxVersions(max int) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.maxVersions = max
}

// SetMaxAge define a idade máxima das versões
func (vm *VersionManager) SetMaxAge(maxAge time.Duration) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.maxAge = maxAge
}

// SaveVersion salva uma versão do arquivo antes de sobrescrevê-lo.
// Copia o arquivo atual para .stversions/ com timestamp.
// Retorna o caminho da versão criada ou erro.
func (vm *VersionManager) SaveVersion(folderPath, relPath string) (string, error) {
	vm.mu.Lock()
	enabled := vm.enabled
	maxVersions := vm.maxVersions
	maxAge := vm.maxAge
	vm.mu.Unlock()

	if !enabled {
		return "", nil
	}

	fullPath := filepath.Join(folderPath, filepath.FromSlash(relPath))

	// Verificar se o arquivo existe
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // nada para versionar
		}
		return "", fmt.Errorf("erro ao verificar arquivo para versionar: %w", err)
	}

	if info.IsDir() {
		return "", nil
	}

	versionBase := filepath.Join(folderPath, versionDir)
	relDir := filepath.Dir(filepath.FromSlash(relPath))
	versionSubDir := filepath.Join(versionBase, relDir)
	if err := os.MkdirAll(versionSubDir, 0755); err != nil {
		return "", fmt.Errorf("erro ao criar diretório de versões: %w", err)
	}

	// Gerar nome da versão: arquivo~YYYYMMDD-HHMMSS.ext
	ext := filepath.Ext(relPath)
	baseName := strings.TrimSuffix(filepath.Base(relPath), ext)
	timestamp := time.Now().Format("20060102-150405")
	versionName := fmt.Sprintf("%s~%s%s", baseName, timestamp, ext)
	versionFullPath := filepath.Join(versionSubDir, versionName)

	// Copiar arquivo para versão
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("erro ao ler arquivo para versionar: %w", err)
	}

	if err := os.WriteFile(versionFullPath, data, 0644); err != nil {
		return "", fmt.Errorf("erro ao salvar versão: %w", err)
	}

	// Preserve the original ModTime on the saved version.
	os.Chtimes(versionFullPath, info.ModTime(), info.ModTime())

	log.Printf("[Versão] Versão salva: %s -> %s", relPath, versionName)

	// Limpar versões antigas deste arquivo
	vm.cleanVersions(folderPath, relPath, maxVersions, maxAge)

	return versionFullPath, nil
}

// ListVersions lista todas as versões de um arquivo
func (vm *VersionManager) ListVersions(folderPath, relPath string) ([]VersionInfo, error) {
	versionBase := filepath.Join(folderPath, versionDir)
	relDir := filepath.Dir(filepath.FromSlash(relPath))
	versionSubDir := filepath.Join(versionBase, relDir)

	ext := filepath.Ext(relPath)
	baseName := strings.TrimSuffix(filepath.Base(relPath), ext)
	prefix := baseName + "~"

	entries, err := os.ReadDir(versionSubDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var versions []VersionInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ext) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Extrair timestamp da versão
		tsStr := strings.TrimPrefix(name, prefix)
		tsStr = strings.TrimSuffix(tsStr, ext)
		versionTime, _ := time.Parse("20060102-150405", tsStr)

		versionRelPath := filepath.ToSlash(filepath.Join(relDir, name))

		versions = append(versions, VersionInfo{
			RelPath:     relPath,
			VersionPath: versionRelPath,
			ModTime:     info.ModTime(),
			Size:        info.Size(),
			VersionTime: versionTime,
		})
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].VersionTime.After(versions[j].VersionTime)
	})

	return versions, nil
}

// RestoreVersion restores a specific version of a file.
func (vm *VersionManager) RestoreVersion(folderPath, relPath, versionPath string) error {
	versionBase := filepath.Join(folderPath, versionDir)
	versionFullPath := filepath.Join(versionBase, filepath.FromSlash(versionPath))
	fullPath := filepath.Join(folderPath, filepath.FromSlash(relPath))

	if _, err := os.Stat(versionFullPath); os.IsNotExist(err) {
		return fmt.Errorf("versão não encontrada: %s", versionPath)
	}

	// Save current version before overwriting
	if _, err := os.Stat(fullPath); err == nil {
		vm.SaveVersion(folderPath, relPath)
	}

	// Copiar versão para o arquivo original
	data, err := os.ReadFile(versionFullPath)
	if err != nil {
		return fmt.Errorf("erro ao ler versão: %w", err)
	}

	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		return fmt.Errorf("erro ao restaurar versão: %w", err)
	}

	log.Printf("[Versão] Restaurado: %s <- %s", relPath, versionPath)
	return nil
}

// cleanVersions limpa versões antigas de um arquivo
func (vm *VersionManager) cleanVersions(folderPath, relPath string, maxVersions int, maxAge time.Duration) {
	versions, err := vm.ListVersions(folderPath, relPath)
	if err != nil || len(versions) == 0 {
		return
	}

	now := time.Now()

	for i, v := range versions {
		shouldRemove := false

		// Verificar limite de quantidade
		if maxVersions > 0 && i >= maxVersions {
			shouldRemove = true
		}

		// Verificar idade máxima
		if maxAge > 0 && now.Sub(v.VersionTime) > maxAge {
			shouldRemove = true
		}

		if shouldRemove {
			versionBase := filepath.Join(folderPath, versionDir)
			fullVersionPath := filepath.Join(versionBase, filepath.FromSlash(v.VersionPath))
			if err := os.Remove(fullVersionPath); err == nil {
				log.Printf("[Versão] Removida versão antiga: %s", v.VersionPath)
			}
		}
	}
}

// CleanAllVersions limpa versões antigas de todos os arquivos em uma pasta
func (vm *VersionManager) CleanAllVersions(folderPath string) (int, error) {
	vm.mu.Lock()
	_ = vm.maxVersions // per-file cleanup handled in cleanVersions
	maxAge := vm.maxAge
	vm.mu.Unlock()

	versionBase := filepath.Join(folderPath, versionDir)
	if _, err := os.Stat(versionBase); os.IsNotExist(err) {
		return 0, nil
	}

	removed := 0

	err := filepath.Walk(versionBase, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}

		shouldRemove := false
		now := time.Now()

		if maxAge > 0 && now.Sub(info.ModTime()) > maxAge {
			shouldRemove = true
		}

		if shouldRemove {
			if err := os.Remove(path); err == nil {
				removed++
			}
		}

		return nil
	})

	// Verificar maxVersions por arquivo (agrupar por base)
	// Isso exige listagem por arquivo, já tratado em cleanVersions

	return removed, err
}

// GetVersionDirSize retorna o tamanho total do diretório de versões
func GetVersionDirSize(folderPath string) (int64, int, error) {
	versionBase := filepath.Join(folderPath, versionDir)
	if _, err := os.Stat(versionBase); os.IsNotExist(err) {
		return 0, 0, nil
	}

	var totalSize int64
	var fileCount int

	err := filepath.Walk(versionBase, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			totalSize += info.Size()
			fileCount++
		}
		return nil
	})

	return totalSize, fileCount, err
}

// IsVersionDir verifica se um caminho é o diretório de versões
func IsVersionDir(name string) bool {
	return name == versionDir || strings.HasPrefix(name, versionDir+"/") || strings.HasPrefix(name, versionDir+"\\")
}
