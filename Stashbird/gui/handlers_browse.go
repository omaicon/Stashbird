package gui

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ──────────────────────────────────────────────
// Browse: Folder Picker API
// ──────────────────────────────────────────────

// GET /api/browse?path=<dir>
// Lista diretórios da máquina para o usuário selecionar a pasta desejada.
func (ws *WebServer) handleBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", 405)
		return
	}

	reqPath := r.URL.Query().Get("path")

	type dirItem struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	// Se nenhum path foi passado, retornar raízes do sistema
	if reqPath == "" {
		roots := getSystemRoots()
		jsonResponse(w, map[string]interface{}{
			"current": "",
			"parent":  "",
			"dirs":    roots,
		})
		return
	}

	// Limpar o path
	reqPath = filepath.Clean(reqPath)

	info, err := os.Stat(reqPath)
	if err != nil || !info.IsDir() {
		jsonError(w, "diretório não encontrado", 404)
		return
	}

	entries, err := os.ReadDir(reqPath)
	if err != nil {
		jsonError(w, "erro ao ler diretório: "+err.Error(), 500)
		return
	}

	var dirs []dirItem
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Pular diretórios ocultos e de sistema
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "$") {
			continue
		}
		// Pular dirs comuns de sistema no Windows
		if runtime.GOOS == "windows" {
			lower := strings.ToLower(name)
			if lower == "windows" || lower == "programdata" || lower == "system volume information" ||
				lower == "recovery" || lower == "config.msi" {
				continue
			}
		}
		dirs = append(dirs, dirItem{
			Name: name,
			Path: filepath.Join(reqPath, name),
		})
	}

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})

	parent := filepath.Dir(reqPath)
	if parent == reqPath {
		parent = "" // raiz do drive
	}

	jsonResponse(w, map[string]interface{}{
		"current": reqPath,
		"parent":  parent,
		"dirs":    dirs,
	})
}

// getSystemRoots retorna as raízes do sistema de arquivos.
type browseDir struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func getSystemRoots() []browseDir {
	if runtime.GOOS == "windows" {
		// Listar drives disponíveis
		var roots []browseDir
		for _, drive := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
			p := string(drive) + ":\\"
			if _, err := os.Stat(p); err == nil {
				roots = append(roots, browseDir{Name: string(drive) + ":", Path: p})
			}
		}

		// Adicionar atalhos comuns
		if home, err := os.UserHomeDir(); err == nil {
			roots = append(roots, browseDir{Name: "📁 Pasta do Usuário", Path: home})
			desktop := filepath.Join(home, "Desktop")
			if _, err := os.Stat(desktop); err == nil {
				roots = append(roots, browseDir{Name: "🖥️ Área de Trabalho", Path: desktop})
			}
			docs := filepath.Join(home, "Documents")
			if _, err := os.Stat(docs); err == nil {
				roots = append(roots, browseDir{Name: "📄 Documentos", Path: docs})
			}
			downloads := filepath.Join(home, "Downloads")
			if _, err := os.Stat(downloads); err == nil {
				roots = append(roots, browseDir{Name: "⬇️ Downloads", Path: downloads})
			}
		}
		return roots
	}

	// Linux/Mac
	var roots []browseDir
	roots = append(roots, browseDir{Name: "/", Path: "/"})
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, browseDir{Name: "~ Home", Path: home})
	}
	return roots
}

// POST /api/browse/create-folder — create a new folder on the filesystem (used by folder picker)
func (ws *WebServer) handleBrowseCreateFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, "invalid json", 400)
		return
	}
	if req.Path == "" {
		jsonError(w, "path required", 400)
		return
	}

	cleaned := filepath.Clean(req.Path)
	if strings.Contains(cleaned, "..") {
		jsonError(w, "path traversal denied", 400)
		return
	}

	if err := os.MkdirAll(cleaned, 0755); err != nil {
		jsonError(w, "error creating folder: "+err.Error(), 500)
		return
	}

	ws.AddLog("Pasta criada via picker: " + cleaned)
	jsonResponse(w, map[string]string{"status": "ok", "path": cleaned})
}
