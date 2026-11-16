package main

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func serveDownload(w http.ResponseWriter, r *http.Request, authToken string) {
	if r.URL.Query().Get("token") != authToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	target := strings.TrimSpace(r.URL.Query().Get("path"))
	if target == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	absPath, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	if err := validateDownloadTarget(absPath); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(absPath)))
	w.Header().Set("Content-Type", downloadContentType(absPath))
	http.ServeFile(w, r, absPath)
}

func validateDownloadTarget(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("file not found")
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory")
	}
	return nil
}

func downloadContentType(path string) string {
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}
