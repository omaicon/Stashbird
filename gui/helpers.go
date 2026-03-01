package gui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

func maskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

func guessMimeType(name string, isDir bool) string {
	if isDir {
		return "folder"
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg":
		return "image"
	case ".mp4", ".avi", ".mkv", ".mov", ".wmv":
		return "video"
	case ".mp3", ".wav", ".flac", ".aac", ".ogg":
		return "audio"
	case ".pdf":
		return "pdf"
	case ".doc", ".docx":
		return "document"
	case ".xls", ".xlsx":
		return "spreadsheet"
	case ".ppt", ".pptx":
		return "presentation"
	case ".zip", ".rar", ".7z", ".tar", ".gz":
		return "archive"
	case ".txt", ".md", ".log":
		return "text"
	case ".go", ".py", ".js", ".ts", ".html", ".css", ".java", ".c", ".cpp", ".h":
		return "code"
	case ".exe", ".msi":
		return "executable"
	default:
		return "file"
	}
}

// formatBytes formats bytes into human-readable string.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return strconv.FormatInt(b, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %s", float64(b)/float64(div), []string{"KB", "MB", "GB", "TB"}[exp])
}
