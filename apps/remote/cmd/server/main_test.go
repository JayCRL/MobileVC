package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestServeDownloadAllowsExistingFile(t *testing.T) {
	filePath := writeDownloadTestFile(t)
	resp := requestDownload(t, filePath, "test")

	if resp.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d body=%q", resp.Code, http.StatusOK, resp.Body.String())
	}
	if resp.Body.String() != "ok" {
		t.Fatalf("body: got %q", resp.Body.String())
	}
}

func TestServeDownloadRejectsMissingToken(t *testing.T) {
	resp := requestDownload(t, writeDownloadTestFile(t), "wrong")

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d body=%q", resp.Code, http.StatusUnauthorized, resp.Body.String())
	}
}

func TestServeDownloadRejectsDirectory(t *testing.T) {
	resp := requestDownload(t, t.TempDir(), "test")

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d body=%q", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}

func writeDownloadTestFile(t *testing.T) string {
	t.Helper()
	filePath := filepath.Join(t.TempDir(), "session.log")
	if err := os.WriteFile(filePath, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	return filePath
}

func requestDownload(t *testing.T, path string, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/download?token="+token+"&path="+path, nil)
	resp := httptest.NewRecorder()
	serveDownload(resp, req, "test")
	return resp
}
