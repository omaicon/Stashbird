package gui

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ──────────────────────────────────────────────
// Download Handlers
// ──────────────────────────────────────────────

// GET /api/folders/files/download?id=X&path=Y
// Serve um arquivo para download (também usado para thumbnails de imagens).
func (ws *WebServer) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", 405)
		return
	}

	folderID := r.URL.Query().Get("id")
	filePath := r.URL.Query().Get("path")
	if folderID == "" || filePath == "" {
		jsonError(w, "id e path são obrigatórios", 400)
		return
	}

	_, absPath, err := ws.resolveSafePath(folderID, filePath)
	if err != nil {
		jsonError(w, err.Error(), 400)
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		jsonError(w, "arquivo não encontrado", 404)
		return
	}
	if info.IsDir() {
		jsonError(w, "use /api/files/download-zip para baixar diretórios", 400)
		return
	}

	// Nome do arquivo para o header
	fileName := filepath.Base(absPath)

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sanitizeFilename(fileName)))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("Cache-Control", "no-cache")

	http.ServeFile(w, r, absPath)
}

// GET /api/files/download-zip?folder=X&path=Y
// Compacta uma pasta inteira (ou um arquivo) e serve como ZIP para download.
func (ws *WebServer) handleDownloadZip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method not allowed", 405)
		return
	}

	folderID := r.URL.Query().Get("folder")
	subPath := r.URL.Query().Get("path")
	if folderID == "" {
		jsonError(w, "folder é obrigatório", 400)
		return
	}

	_, absPath, err := ws.resolveSafePath(folderID, subPath)
	if err != nil {
		jsonError(w, err.Error(), 400)
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		jsonError(w, "caminho não encontrado", 404)
		return
	}

	// Nome do ZIP
	baseName := filepath.Base(absPath)
	if baseName == "." || baseName == "" {
		baseName = folderID
	}
	zipName := strings.TrimSuffix(baseName, filepath.Ext(baseName)) + ".zip"

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sanitizeFilename(zipName)))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Cache-Control", "no-cache")

	zw := zip.NewWriter(w)
	defer zw.Close()

	if info.IsDir() {
		// Compacta recursivamente o diretório
		err = filepath.Walk(absPath, func(path string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return nil // pular erros de leitura
			}

			// Caminho relativo dentro do ZIP
			rel, relErr := filepath.Rel(absPath, path)
			if relErr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)

			// Pular a raiz do walk (rel == ".") — não adicionar como entrada no ZIP
			if rel == "." {
				return nil
			}

			// Pular arquivos/pastas ocultos e de versão interna
			// Verificamos apenas o nome do item corrente (filepath.Base), não os ancestrais,
			// para evitar que "." da raiz dispare o filtro.
			basePart := fi.Name()
			if strings.HasPrefix(basePart, ".") || strings.HasPrefix(basePart, "$") {
				if fi.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			if fi.IsDir() {
				// Criar entrada de diretório no zip
				_, err := zw.Create(rel + "/")
				if err != nil {
					return nil
				}
				return nil
			}

			// Adicionar arquivo
			header, err := zip.FileInfoHeader(fi)
			if err != nil {
				return nil
			}
			header.Name = rel
			header.Method = zip.Deflate

			writer, err := zw.CreateHeader(header)
			if err != nil {
				return nil
			}

			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer f.Close()

			_, _ = io.Copy(writer, f)
			return nil
		})
		if err != nil {
			// A resposta já foi iniciada, não podemos enviar JSON de erro
			return
		}
	} else {
		// Compacta um único arquivo
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return
		}
		header.Method = zip.Deflate

		writer, err := zw.CreateHeader(header)
		if err != nil {
			return
		}

		f, err := os.Open(absPath)
		if err != nil {
			return
		}
		defer f.Close()

		_, _ = io.Copy(writer, f)
	}
}

// sanitizeFilename remove caracteres problemáticos para nomes de arquivo em headers HTTP.
func sanitizeFilename(name string) string {
	// Substituir aspas duplas e barras que poderiam quebrar o Content-Disposition
	r := strings.NewReplacer(`"`, `'`, `\`, `-`, `/`, `-`)
	return r.Replace(name)
}
