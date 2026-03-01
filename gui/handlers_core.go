package gui

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"Stashbird/config"
	filesync "Stashbird/sync"
)

// GET /api/status
func (ws *WebServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", 405)
		return
	}

	stats := ws.syncServer.GetStats()
	connected, ip := ws.tailscale.GetStatus()

	// Montar URL de acesso remoto se habilitado e Tailscale conectado
	remoteURL := ""
	if ws.cfg.WebRemoteAccess && connected && ip != "" {
		port := ws.cfg.WebPort
		if port == 0 {
			port = 8384
		}
		remoteURL = fmt.Sprintf("http://%s:%d", ip, port)
	}

	jsonResponse(w, map[string]interface{}{
		"device_id":         ws.cfg.DeviceID,
		"device_name":       ws.cfg.DeviceName,
		"listen_port":       ws.cfg.ListenPort,
		"tailscale":         map[string]interface{}{"connected": connected, "ip": ip, "mode": ws.tailscale.GetMode().String()},
		"sync":              stats,
		"folders_count":     len(ws.cfg.GetFolders()),
		"peers_count":       len(ws.cfg.GetPeers()),
		"version":           "2.0.0",
		"web_remote_access": ws.cfg.WebRemoteAccess,
		"remote_url":        remoteURL,
	})
}

// GET/POST/DELETE /api/folders
func (ws *WebServer) handleFolders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		folders := ws.cfg.GetFolders()
		jsonResponse(w, folders)

	case http.MethodPost:
		var req struct {
			Label      string `json:"label"`
			Path       string `json:"path"`
			SyncDelete bool   `json:"sync_delete"`
			Enabled    bool   `json:"enabled"`
		}
		if err := readJSON(r, &req); err != nil {
			jsonError(w, "invalid json", 400)
			return
		}
		if req.Label == "" || req.Path == "" {
			jsonError(w, "label and path are required", 400)
			return
		}
		folder := config.FolderConfig{
			ID:         req.Label,
			Label:      req.Label,
			Path:       req.Path,
			Enabled:    req.Enabled,
			SyncDelete: req.SyncDelete,
		}
		ws.cfg.AddFolder(folder)
		ws.cfg.Save()
		ws.AddLog("Pasta adicionada: " + folder.Label)

		go func() {
			ws.syncServer.RefreshWatchers()
			ws.syncServer.AnnounceFolderToPeers(folder)
			time.Sleep(5 * time.Second)
			ws.syncServer.TriggerSync()
		}()

		jsonResponse(w, folder)

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			jsonError(w, "id required", 400)
			return
		}
		ws.cfg.RemoveFolder(id)
		ws.cfg.Save()
		go ws.syncServer.RefreshWatchers()
		ws.AddLog("Pasta removida: " + id)
		jsonResponse(w, map[string]string{"status": "ok"})

	default:
		jsonError(w, "method not allowed", 405)
	}
}

// GET /api/folders/files?id=xxx&path=yyy
// POST /api/folders/create-subfolder — create a new subfolder inside a synced folder
func (ws *WebServer) handleCreateSubfolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}

	var req struct {
		FolderID string `json:"folder_id"`
		Path     string `json:"path"`
		Name     string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, "invalid json", 400)
		return
	}
	if req.FolderID == "" || req.Name == "" {
		jsonError(w, "folder_id and name are required", 400)
		return
	}

	// Validate name: no path separators or traversal
	if strings.ContainsAny(req.Name, "/\\:") || req.Name == ".." || req.Name == "." {
		jsonError(w, "invalid folder name", 400)
		return
	}

	// Find folder path
	var folderPath string
	for _, f := range ws.cfg.GetFolders() {
		if f.ID == req.FolderID || f.Label == req.FolderID {
			folderPath = f.Path
			break
		}
	}
	if folderPath == "" {
		jsonError(w, "folder not found", 404)
		return
	}

	targetDir := folderPath
	if req.Path != "" {
		targetDir = filepath.Join(folderPath, req.Path)
	}
	newDir := filepath.Join(targetDir, req.Name)

	// Safety: ensure within folder root
	absFolder, _ := filepath.Abs(folderPath)
	absNew, _ := filepath.Abs(newDir)
	rel, err := filepath.Rel(absFolder, absNew)
	if err != nil || strings.HasPrefix(rel, "..") {
		jsonError(w, "path traversal denied", 400)
		return
	}

	if err := os.MkdirAll(newDir, 0755); err != nil {
		jsonError(w, "error creating folder: "+err.Error(), 500)
		return
	}

	ws.AddLog("Subpasta criada: " + req.Name)
	jsonResponse(w, map[string]string{"status": "ok", "name": req.Name})
}

// POST /api/files/create — create a new empty file inside a synced folder
func (ws *WebServer) handleCreateFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}

	var req struct {
		FolderID string `json:"folder_id"`
		Path     string `json:"path"`
		Name     string `json:"name"`
		Content  string `json:"content"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, "invalid json", 400)
		return
	}
	if req.FolderID == "" || req.Name == "" {
		jsonError(w, "folder_id and name are required", 400)
		return
	}

	// Validate name
	if strings.ContainsAny(req.Name, "/\\:") || req.Name == ".." || req.Name == "." {
		jsonError(w, "invalid file name", 400)
		return
	}

	// Find folder path
	var folderPath string
	for _, f := range ws.cfg.GetFolders() {
		if f.ID == req.FolderID || f.Label == req.FolderID {
			folderPath = f.Path
			break
		}
	}
	if folderPath == "" {
		jsonError(w, "folder not found", 404)
		return
	}

	targetDir := folderPath
	if req.Path != "" {
		targetDir = filepath.Join(folderPath, req.Path)
	}
	newFile := filepath.Join(targetDir, req.Name)

	// Safety: ensure within folder root
	absFolder, _ := filepath.Abs(folderPath)
	absNew, _ := filepath.Abs(newFile)
	rel, err := filepath.Rel(absFolder, absNew)
	if err != nil || strings.HasPrefix(rel, "..") {
		jsonError(w, "path traversal denied", 400)
		return
	}

	// Check if file already exists
	if _, err := os.Stat(newFile); err == nil {
		jsonError(w, "file already exists", 409)
		return
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		jsonError(w, "error creating directory: "+err.Error(), 500)
		return
	}

	content := req.Content
	if content == "" {
		// Default content for .md files
		if strings.HasSuffix(strings.ToLower(req.Name), ".md") {
			nameWithoutExt := strings.TrimSuffix(req.Name, filepath.Ext(req.Name))
			content = "# " + nameWithoutExt + "\n\n"
		}
	}

	if err := os.WriteFile(newFile, []byte(content), 0644); err != nil {
		jsonError(w, "error creating file: "+err.Error(), 500)
		return
	}

	// Build relative path for response
	relPath := req.Name
	if req.Path != "" {
		relPath = req.Path + "/" + req.Name
	}

	// Index if markdown
	if strings.HasSuffix(strings.ToLower(req.Name), ".md") {
		ws.noteIndex.IndexNote(req.FolderID, relPath, []byte(content))
	}

	ws.AddLog("Arquivo criado: " + req.Name)
	jsonResponse(w, map[string]string{"status": "ok", "name": req.Name, "path": relPath})
}

// GET /api/search?q=xxx — busca recursiva em todas as pastas configuradas
func (ws *WebServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", 405)
		return
	}

	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if query == "" {
		jsonResponse(w, map[string]interface{}{"results": []interface{}{}})
		return
	}

	type searchResult struct {
		Name     string `json:"name"`
		Path     string `json:"path"`
		FolderID string `json:"folder_id"`
		Folder   string `json:"folder_label"`
		IsDir    bool   `json:"is_dir"`
		Size     int64  `json:"size"`
		ModTime  string `json:"mod_time"`
		MimeType string `json:"mime_type"`
	}

	var results []searchResult
	const maxResults = 20

	for _, folder := range ws.cfg.GetFolders() {
		if !folder.Enabled {
			continue
		}
		folderPath := folder.Path
		folderID := folder.ID
		folderLabel := folder.Label

		_ = filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // skip unreadable
			}
			if len(results) >= maxResults {
				return filepath.SkipAll
			}

			name := info.Name()
			// Skip hidden files/dirs
			if strings.HasPrefix(name, ".") || name == ".stversions" {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if filesync.IsConflictFile(name) || filesync.IsVersionDir(name) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			// Match name
			if !strings.Contains(strings.ToLower(name), query) {
				return nil
			}

			// Compute relative path from folder root
			relPath, relErr := filepath.Rel(folderPath, path)
			if relErr != nil {
				return nil
			}
			relPath = filepath.ToSlash(relPath)

			results = append(results, searchResult{
				Name:     name,
				Path:     relPath,
				FolderID: folderID,
				Folder:   folderLabel,
				IsDir:    info.IsDir(),
				Size:     info.Size(),
				ModTime:  info.ModTime().Format("2006-01-02 15:04"),
				MimeType: guessMimeType(name, info.IsDir()),
			})
			return nil
		})
	}

	if results == nil {
		results = []searchResult{}
	}

	jsonResponse(w, map[string]interface{}{"results": results})
}

// GET /api/folders/files?id=xxx&path=yyy
func (ws *WebServer) handleFolderFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", 405)
		return
	}

	folderID := r.URL.Query().Get("id")
	subPath := r.URL.Query().Get("path")
	if folderID == "" {
		jsonError(w, "id required", 400)
		return
	}

	// Find folder path
	var folderPath string
	for _, f := range ws.cfg.GetFolders() {
		if f.ID == folderID || f.Label == folderID {
			folderPath = f.Path
			break
		}
	}
	if folderPath == "" {
		jsonError(w, "folder not found", 404)
		return
	}

	browsePath := folderPath
	if subPath != "" {
		browsePath = filepath.Join(folderPath, subPath)
	}

	type fileItem struct {
		Name     string  `json:"name"`
		Path     string  `json:"path"`
		IsDir    bool    `json:"is_dir"`
		Size     int64   `json:"size"`
		ModTime  string  `json:"mod_time"`
		Status   string  `json:"status"`
		Icon     string  `json:"icon"`
		MimeType string  `json:"mime_type"`
		Progress float64 `json:"progress"`
	}

	var items []fileItem
	entries, err := os.ReadDir(browsePath)
	if err != nil {
		jsonError(w, "error reading directory: "+err.Error(), 500)
		return
	}

	fileStatusTracker := ws.syncServer.GetFileStatus()

	for _, e := range entries {
		// Skip hidden / system files
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == ".stversions" {
			continue
		}
		if filesync.IsConflictFile(name) || filesync.IsVersionDir(name) {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		relPath := name
		if subPath != "" {
			relPath = subPath + "/" + name
		}

		// Get sync status
		status := "synced"
		icon := "✅"
		progress := 1.0
		if entry := fileStatusTracker.GetStatus(folderID, relPath); entry != nil {
			status = entry.Status.String()
			icon = entry.Status.Icon()
			progress = entry.Progress
		}

		items = append(items, fileItem{
			Name:     name,
			Path:     relPath,
			IsDir:    e.IsDir(),
			Size:     info.Size(),
			ModTime:  info.ModTime().Format("2006-01-02 15:04"),
			Status:   status,
			Icon:     icon,
			MimeType: guessMimeType(name, e.IsDir()),
			Progress: progress,
		})
	}

	if items == nil {
		items = []fileItem{}
	}

	jsonResponse(w, map[string]interface{}{
		"folder_id": folderID,
		"path":      subPath,
		"items":     items,
	})
}

// DELETE /api/files/delete — remove um arquivo ou diretório de uma pasta sincronizada
func (ws *WebServer) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		jsonError(w, "method not allowed", 405)
		return
	}

	folderID := r.URL.Query().Get("id")
	relPath := r.URL.Query().Get("path")
	if folderID == "" || relPath == "" {
		jsonError(w, "id and path are required", 400)
		return
	}

	// Find folder path
	var folderPath string
	for _, f := range ws.cfg.GetFolders() {
		if f.ID == folderID || f.Label == folderID {
			folderPath = f.Path
			break
		}
	}
	if folderPath == "" {
		jsonError(w, "folder not found", 404)
		return
	}

	target := filepath.Join(folderPath, relPath)

	// Safety: ensure within folder root
	absFolder, _ := filepath.Abs(folderPath)
	absTarget, _ := filepath.Abs(target)
	rel, err := filepath.Rel(absFolder, absTarget)
	if err != nil || strings.HasPrefix(rel, "..") {
		jsonError(w, "path traversal denied", 400)
		return
	}

	info, err := os.Stat(target)
	if err != nil {
		jsonError(w, "arquivo ou pasta não encontrado", 404)
		return
	}

	if info.IsDir() {
		err = os.RemoveAll(target)
	} else {
		err = os.Remove(target)
	}
	if err != nil {
		jsonError(w, "erro ao excluir: "+err.Error(), 500)
		return
	}

	ws.AddLog("Excluído: " + relPath)
	jsonResponse(w, map[string]string{"status": "ok"})
}

// POST /api/files/rename — renomeia um arquivo ou diretório dentro de uma pasta sincronizada
func (ws *WebServer) handleRenameFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}

	var req struct {
		FolderID string `json:"folder_id"`
		OldPath  string `json:"old_path"`
		NewName  string `json:"new_name"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, "invalid json", 400)
		return
	}
	if req.FolderID == "" || req.OldPath == "" || req.NewName == "" {
		jsonError(w, "folder_id, old_path and new_name are required", 400)
		return
	}

	// Validate new name: no path separators or traversal
	if strings.ContainsAny(req.NewName, "/\\:") || req.NewName == ".." || req.NewName == "." {
		jsonError(w, "invalid name", 400)
		return
	}

	// Find folder path
	var folderPath string
	for _, f := range ws.cfg.GetFolders() {
		if f.ID == req.FolderID || f.Label == req.FolderID {
			folderPath = f.Path
			break
		}
	}
	if folderPath == "" {
		jsonError(w, "folder not found", 404)
		return
	}

	oldFull := filepath.Join(folderPath, req.OldPath)

	// Safety: ensure within folder root
	absFolder, _ := filepath.Abs(folderPath)
	absOld, _ := filepath.Abs(oldFull)
	relOld, err := filepath.Rel(absFolder, absOld)
	if err != nil || strings.HasPrefix(relOld, "..") {
		jsonError(w, "path traversal denied", 400)
		return
	}

	// Check if source exists
	if _, err := os.Stat(oldFull); err != nil {
		jsonError(w, "arquivo ou pasta não encontrado", 404)
		return
	}

	// Build new full path (same parent directory, new name)
	parentDir := filepath.Dir(oldFull)
	newFull := filepath.Join(parentDir, req.NewName)

	// Safety: ensure new path is also within folder root
	absNew, _ := filepath.Abs(newFull)
	relNew, err := filepath.Rel(absFolder, absNew)
	if err != nil || strings.HasPrefix(relNew, "..") {
		jsonError(w, "path traversal denied", 400)
		return
	}

	// Check if destination already exists
	if _, err := os.Stat(newFull); err == nil {
		jsonError(w, "já existe um arquivo ou pasta com esse nome", 409)
		return
	}

	if err := os.Rename(oldFull, newFull); err != nil {
		jsonError(w, "erro ao renomear: "+err.Error(), 500)
		return
	}

	// Build the new relative path
	newRelPath := filepath.ToSlash(relNew)

	ws.AddLog(fmt.Sprintf("Renomeado: %s → %s", req.OldPath, req.NewName))
	jsonResponse(w, map[string]string{"status": "ok", "new_name": req.NewName, "new_path": newRelPath})
}

// POST /api/shutdown — encerra a aplicação
func (ws *WebServer) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}
	ws.AddLog("Encerramento solicitado via interface web")
	jsonResponse(w, map[string]string{"status": "shutting down"})

	// Encerra o processo após enviar a resposta
	go func() {
		time.Sleep(500 * time.Millisecond)
		log.Println("[WebUI] Encerrando aplicação por solicitação do usuário...")
		os.Exit(0)
	}()
}
