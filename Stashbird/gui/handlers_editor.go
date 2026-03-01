package gui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// resolveSafePath resolves folder + subpath safely, preventing path traversal.
func (ws *WebServer) resolveSafePath(folderID, subPath string) (string, string, error) {
	var folderPath string
	folders := ws.cfg.GetFolders()
	for _, f := range folders {
		if f.ID == folderID || f.Label == folderID {
			folderPath = f.Path
			break
		}
	}
	if folderPath == "" {
		log.Printf("[WebUI] resolveSafePath: pasta não encontrada — folderID=%q (pastas disponíveis: %d)", folderID, len(folders))
		return "", "", fmt.Errorf("pasta não encontrada: %s", folderID)
	}

	absFolder, err := filepath.Abs(folderPath)
	if err != nil {
		return "", "", fmt.Errorf("caminho da pasta inválido: %s", folderPath)
	}

	if subPath == "" {
		return absFolder, absFolder, nil
	}

	// Clean and join
	full := filepath.Join(absFolder, filepath.FromSlash(subPath))
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", "", fmt.Errorf("caminho inválido: %s", subPath)
	}

	// Ensure the resolved path is within the folder root
	rel, err := filepath.Rel(absFolder, fullAbs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", "", fmt.Errorf("path traversal denied")
	}

	return absFolder, fullAbs, nil
}

// GET /api/files/read?folder=X&path=Y — read file content as text
func (ws *WebServer) handleFileRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", 405)
		return
	}

	folderID := r.URL.Query().Get("folder")
	filePath := r.URL.Query().Get("path")
	if folderID == "" || filePath == "" {
		log.Printf("[WebUI] handleFileRead: parâmetros faltando — folder=%q path=%q", folderID, filePath)
		jsonError(w, "folder and path required", 400)
		return
	}

	_, absPath, err := ws.resolveSafePath(folderID, filePath)
	if err != nil {
		log.Printf("[WebUI] handleFileRead: resolveSafePath falhou — folder=%q path=%q err=%v", folderID, filePath, err)
		jsonError(w, err.Error(), 400)
		return
	}

	// Verify file exists before reading
	info, err := os.Stat(absPath)
	if err != nil {
		log.Printf("[WebUI] handleFileRead: arquivo não encontrado — %s: %v", absPath, err)
		jsonError(w, "arquivo não encontrado: "+filePath, 404)
		return
	}
	if info.IsDir() {
		jsonError(w, "caminho aponta para um diretório, não um arquivo", 400)
		return
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		log.Printf("[WebUI] handleFileRead: erro ao ler — %s: %v", absPath, err)
		jsonError(w, "erro ao ler arquivo: "+err.Error(), 500)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"folder":  folderID,
		"path":    filePath,
		"content": string(content),
	})
}

// PUT /api/files/write — save file content
func (ws *WebServer) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		jsonError(w, "method not allowed", 405)
		return
	}

	var req struct {
		Folder  string `json:"folder"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, "invalid json", 400)
		return
	}

	if req.Folder == "" || req.Path == "" {
		jsonError(w, "folder and path required", 400)
		return
	}

	_, absPath, err := ws.resolveSafePath(req.Folder, req.Path)
	if err != nil {
		jsonError(w, err.Error(), 400)
		return
	}

	if err := os.WriteFile(absPath, []byte(req.Content), 0644); err != nil {
		jsonError(w, "error saving file: "+err.Error(), 500)
		return
	}

	// Update note index if it's a markdown file
	if strings.HasSuffix(strings.ToLower(req.Path), ".md") {
		ws.noteIndex.IndexNote(req.Folder, req.Path, []byte(req.Content))
	}

	ws.AddLog(fmt.Sprintf("Arquivo salvo: %s/%s", req.Folder, req.Path))
	jsonResponse(w, map[string]string{"status": "ok"})
}

// POST /api/files/upload-image — upload image file, returns relative path
func (ws *WebServer) handleUploadImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}

	// Max 10MB
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		jsonError(w, "file too large or invalid form", 400)
		return
	}

	folderID := r.FormValue("folder")
	if folderID == "" {
		jsonError(w, "folder required", 400)
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		jsonError(w, "image file required", 400)
		return
	}
	defer file.Close()

	// Validate image type
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" && ext != ".webp" && ext != ".bmp" && ext != ".svg" {
		jsonError(w, "unsupported image format", 400)
		return
	}

	folderRoot, _, err := ws.resolveSafePath(folderID, "")
	if err != nil {
		jsonError(w, err.Error(), 400)
		return
	}

	// Create image directory using configured image folder
	imageFolder := ws.cfg.GetFolderImageFolder(folderID)
	if imageFolder == "" {
		imageFolder = "attachments"
	}
	attachDir := filepath.Join(folderRoot, imageFolder)
	if err := os.MkdirAll(attachDir, 0755); err != nil {
		jsonError(w, "error creating attachments dir: "+err.Error(), 500)
		return
	}

	// Generate unique filename
	randBytes := make([]byte, 8)
	_, _ = rand.Read(randBytes)
	filename := fmt.Sprintf("%s_%s%s",
		time.Now().Format("20060102_150405"),
		hex.EncodeToString(randBytes),
		ext,
	)

	destPath := filepath.Join(attachDir, filename)
	destFile, err := os.Create(destPath)
	if err != nil {
		jsonError(w, "error creating file: "+err.Error(), 500)
		return
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, file); err != nil {
		jsonError(w, "error writing file: "+err.Error(), 500)
		return
	}

	// Return relative path and filename for markdown insertion
	relPath := filepath.ToSlash(filepath.Join(imageFolder, filename))

	ws.AddLog(fmt.Sprintf("Imagem enviada: %s", filename))
	jsonResponse(w, map[string]string{
		"path":         relPath,
		"filename":     filename,
		"image_folder": imageFolder,
	})
}

// GET /api/files/serve?folder=X&path=Y — serve any file with correct Content-Type
func (ws *WebServer) handleServeFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", 405)
		return
	}

	folderID := r.URL.Query().Get("folder")
	filePath := r.URL.Query().Get("path")
	if folderID == "" || filePath == "" {
		jsonError(w, "folder and path required", 400)
		return
	}

	_, absPath, err := ws.resolveSafePath(folderID, filePath)
	if err != nil {
		jsonError(w, err.Error(), 400)
		return
	}

	info, statErr := os.Stat(absPath)
	if os.IsNotExist(statErr) {
		jsonError(w, "file not found", 404)
		return
	}
	if statErr != nil {
		jsonError(w, "error accessing file: "+statErr.Error(), 500)
		return
	}
	if info.IsDir() {
		jsonError(w, "path is a directory", 400)
		return
	}

	// Determine Content-Type from extension
	ext := strings.ToLower(filepath.Ext(absPath))
	mimeMap := map[string]string{
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".gif":  "image/gif",
		".webp": "image/webp",
		".bmp":  "image/bmp",
		".svg":  "image/svg+xml",
		".ico":  "image/x-icon",
		".mp4":  "video/mp4",
		".webm": "video/webm",
		".ogg":  "video/ogg",
		".ogv":  "video/ogg",
		".avi":  "video/x-msvideo",
		".mkv":  "video/x-matroska",
		".mov":  "video/quicktime",
		".mp3":  "audio/mpeg",
		".wav":  "audio/wav",
		".flac": "audio/flac",
		".aac":  "audio/aac",
		".oga":  "audio/ogg",
		".wma":  "audio/x-ms-wma",
		".pdf":  "application/pdf",
		".txt":  "text/plain; charset=utf-8",
		".md":   "text/plain; charset=utf-8",
		".log":  "text/plain; charset=utf-8",
		".csv":  "text/csv; charset=utf-8",
		".json": "application/json; charset=utf-8",
		".xml":  "text/xml; charset=utf-8",
		".html": "text/html; charset=utf-8",
		".css":  "text/css; charset=utf-8",
		".js":   "application/javascript; charset=utf-8",
		".go":   "text/plain; charset=utf-8",
		".py":   "text/plain; charset=utf-8",
		".java": "text/plain; charset=utf-8",
		".c":    "text/plain; charset=utf-8",
		".cpp":  "text/plain; charset=utf-8",
		".h":    "text/plain; charset=utf-8",
		".rs":   "text/plain; charset=utf-8",
		".ts":   "text/plain; charset=utf-8",
		".yaml": "text/plain; charset=utf-8",
		".yml":  "text/plain; charset=utf-8",
		".toml": "text/plain; charset=utf-8",
		".ini":  "text/plain; charset=utf-8",
		".sh":   "text/plain; charset=utf-8",
		".bat":  "text/plain; charset=utf-8",
		".ps1":  "text/plain; charset=utf-8",
	}

	if ct, ok := mimeMap[ext]; ok {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "no-cache")

	// For download query parameter, set Content-Disposition
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(absPath)))
	}

	http.ServeFile(w, r, absPath)
}

// GET /api/files/image?folder=X&path=Y — serve image file
func (ws *WebServer) handleServeImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", 405)
		return
	}

	folderID := r.URL.Query().Get("folder")
	filePath := r.URL.Query().Get("path")
	if folderID == "" || filePath == "" {
		jsonError(w, "folder and path required", 400)
		return
	}

	_, absPath, err := ws.resolveSafePath(folderID, filePath)
	if err != nil {
		jsonError(w, err.Error(), 400)
		return
	}

	// If file doesn't exist at direct path, search in configured image folder
	if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
		imageFolder := ws.cfg.GetFolderImageFolder(folderID)
		if imageFolder == "" {
			imageFolder = "attachments"
		}
		folderRoot, _, _ := ws.resolveSafePath(folderID, "")
		if folderRoot != "" {
			// Try: <folder_root>/<image_folder>/<filename>
			altPath := filepath.Join(folderRoot, imageFolder, filepath.Base(filePath))
			if _, err := os.Stat(altPath); err == nil {
				absPath = altPath
			} else {
				// Also try common image folder names as fallback
				for _, alt := range []string{"attachments", "images", "assets", "imgs", "_resources"} {
					if alt == imageFolder {
						continue
					}
					candidatePath := filepath.Join(folderRoot, alt, filepath.Base(filePath))
					if _, err := os.Stat(candidatePath); err == nil {
						absPath = candidatePath
						break
					}
				}
			}
		}
	}

	// Only serve image files
	ext := strings.ToLower(filepath.Ext(absPath))
	mimeTypes := map[string]string{
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".gif":  "image/gif",
		".webp": "image/webp",
		".bmp":  "image/bmp",
		".svg":  "image/svg+xml",
	}

	mime, ok := mimeTypes[ext]
	if !ok {
		jsonError(w, "not an image file", 400)
		return
	}

	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, absPath)
}

// POST /api/markdown/render — render markdown to HTML
func (ws *WebServer) handleMarkdownRender(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}

	var req struct {
		Content  string `json:"content"`
		FolderID string `json:"folder_id"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, "invalid json", 400)
		return
	}

	result, err := ws.mdRenderer.Render([]byte(req.Content))
	if err != nil {
		jsonError(w, "render error: "+err.Error(), 500)
		return
	}

	// Replace image src paths with API URLs if folder is specified
	if req.FolderID != "" {
		result.HTML = replaceImagePaths(result.HTML, req.FolderID)
	}

	jsonResponse(w, result)
}

// imgSrcRegex matches <img ... src="..." ...> and captures the parts.
var imgSrcRegex = regexp.MustCompile(`(<img\b[^>]*?\bsrc=")([^"]+)(")`)

// replaceImagePaths rewrites relative image src to the /api/files/image endpoint.
func replaceImagePaths(html string, folderID string) string {
	return imgSrcRegex.ReplaceAllStringFunc(html, func(match string) string {
		parts := imgSrcRegex.FindStringSubmatch(match)
		if len(parts) < 4 {
			return match
		}
		src := parts[2]
		// Skip absolute URLs, API URLs, and data URIs
		if strings.HasPrefix(src, "http") || strings.HasPrefix(src, "/api/") || strings.HasPrefix(src, "data:") {
			return match
		}
		// Decode any percent-encoding from the HTML src first to avoid double-encoding
		decodedSrc, err := url.PathUnescape(src)
		if err != nil {
			decodedSrc = src
		}
		apiURL := fmt.Sprintf("/api/files/image?folder=%s&path=%s",
			url.QueryEscape(folderID), url.QueryEscape(decodedSrc))
		return parts[1] + apiURL + parts[3]
	})
}

// GET /api/notes/backlinks?folder=X&path=Y — get notes linking to this note
func (ws *WebServer) handleBacklinks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", 405)
		return
	}

	folderID := r.URL.Query().Get("folder")
	filePath := r.URL.Query().Get("path")
	if folderID == "" || filePath == "" {
		jsonError(w, "folder and path required", 400)
		return
	}

	backlinks := ws.noteIndex.GetBacklinks(folderID, filePath)
	if backlinks == nil {
		backlinks = []BacklinkInfo{}
	}
	jsonResponse(w, backlinks)
}

// GET /api/notes/graph?folder=X — get full graph data
func (ws *WebServer) handleGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", 405)
		return
	}

	folderID := r.URL.Query().Get("folder")
	graph := ws.noteIndex.GetGraph(folderID)
	jsonResponse(w, graph)
}

// GET /api/notes/list?folder=X — list all .md files for wikilink autocomplete
func (ws *WebServer) handleNotesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", 405)
		return
	}

	folderID := r.URL.Query().Get("folder")
	if folderID == "" {
		jsonError(w, "folder required", 400)
		return
	}

	folderRoot, _, err := ws.resolveSafePath(folderID, "")
	if err != nil {
		jsonError(w, err.Error(), 400)
		return
	}

	type noteItem struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	var notes []noteItem
	_ = filepath.Walk(folderRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == ".stversions" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) == ".md" {
			rel, err := filepath.Rel(folderRoot, path)
			if err != nil {
				return nil
			}
			name := strings.TrimSuffix(info.Name(), filepath.Ext(info.Name()))
			notes = append(notes, noteItem{
				Name: name,
				Path: filepath.ToSlash(rel),
			})
		}
		return nil
	})

	if notes == nil {
		notes = []noteItem{}
	}
	jsonResponse(w, notes)
}

// GET/PUT /api/folders/image-folder — get or set image folder for a sync folder
func (ws *WebServer) handleImageFolderConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		folderID := r.URL.Query().Get("id")
		if folderID == "" {
			jsonError(w, "id required", 400)
			return
		}
		imageFolder := ws.cfg.GetFolderImageFolder(folderID)
		if imageFolder == "" {
			imageFolder = "attachments"
		}
		jsonResponse(w, map[string]string{
			"folder_id":    folderID,
			"image_folder": imageFolder,
		})

	case http.MethodPut:
		var req struct {
			FolderID    string `json:"folder_id"`
			ImageFolder string `json:"image_folder"`
		}
		if err := readJSON(r, &req); err != nil {
			jsonError(w, "invalid json", 400)
			return
		}
		if req.FolderID == "" || req.ImageFolder == "" {
			jsonError(w, "folder_id and image_folder required", 400)
			return
		}

		// Sanitize: only relative paths, no traversal
		cleaned := filepath.ToSlash(filepath.Clean(req.ImageFolder))
		if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
			jsonError(w, "image_folder must be a relative path within the sync folder", 400)
			return
		}

		ws.cfg.SetFolderImageFolder(req.FolderID, cleaned)
		ws.cfg.Save()

		// Ensure directory exists
		folderRoot, _, err := ws.resolveSafePath(req.FolderID, "")
		if err == nil {
			os.MkdirAll(filepath.Join(folderRoot, cleaned), 0755)
		}

		ws.AddLog(fmt.Sprintf("Pasta de imagens configurada: %s → %s", req.FolderID, cleaned))
		jsonResponse(w, map[string]string{"status": "ok", "image_folder": cleaned})

	default:
		jsonError(w, "method not allowed", 405)
	}
}
