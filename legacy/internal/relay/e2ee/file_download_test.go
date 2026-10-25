package e2ee

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestFileDownloadOpenFrameKeepsPathInsideTunnelMetadata(t *testing.T) {
	size := int64(1234)
	frame, err := NewFileDownloadOpenFrame(42, FileDownloadMetadata{
		Path: "/workspace/build/app-release.apk", FileName: "app-release.apk",
		ContentType: "application/vnd.android.package-archive", Size: &size,
	}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if frame.StreamType != TunnelStreamFileDownload || frame.Metadata["path"] == "" {
		t.Fatalf("invalid file download open frame: %#v", frame)
	}
	raw, err := MarshalTunnelFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalTunnelFrame(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Metadata["fileName"] != "app-release.apk" || decoded.Metadata["size"] != "1234" {
		t.Fatalf("metadata mismatch: %#v", decoded.Metadata)
	}
	withoutSize, err := NewFileDownloadOpenFrame(43, FileDownloadMetadata{
		Path: "/workspace/build/app-release.apk",
	}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := withoutSize.Metadata["size"]; ok {
		t.Fatalf("unexpected default size metadata: %#v", withoutSize.Metadata)
	}

	_, err = NewFileDownloadOpenFrame(42, FileDownloadMetadata{}, 4)
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("expected missing path error, got %v", err)
	}
}

func TestFileDownloadChunkerReadsBoundedChunks(t *testing.T) {
	content := bytes.Repeat([]byte("a"), FileDownloadDefaultChunkSize+17)
	chunker, err := NewFileDownloadChunker(bytes.NewReader(content), FileDownloadDefaultChunkSize)
	if err != nil {
		t.Fatal(err)
	}
	first, err := chunker.Next()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != FileDownloadDefaultChunkSize {
		t.Fatalf("first chunk len: got %d", len(first))
	}
	second, err := chunker.Next()
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 17 {
		t.Fatalf("second chunk len: got %d", len(second))
	}
	if _, err := chunker.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestFileDownloadRejectsOversizedChunks(t *testing.T) {
	_, err := NewFileDownloadDataFrame(42, 1, bytes.Repeat([]byte("x"), FileDownloadMaxChunkSize+1), 0)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversized chunk error, got %v", err)
	}
	_, err = NewFileDownloadDataFrame(42, 1, []byte("ok"), FileDownloadMaxChunkSize+1)
	if err == nil || !strings.Contains(err.Error(), "chunk size") {
		t.Fatalf("expected invalid chunk size error, got %v", err)
	}
}

func TestFileDownloadWindowAckAndCancelFrames(t *testing.T) {
	window, err := NewFileDownloadSendWindow(1)
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewFileDownloadDataFrame(42, 1, []byte("a"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := window.ObserveSend(first); err != nil {
		t.Fatal(err)
	}
	second, err := NewFileDownloadDataFrame(42, 2, []byte("b"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := window.ObserveSend(second); err == nil || !strings.Contains(err.Error(), FileDownloadErrorWindowExceeded) {
		t.Fatalf("expected window exceeded, got %v", err)
	}
	ack, err := NewFileDownloadAckFrame(42, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := window.ObserveAck(ack); err != nil {
		t.Fatal(err)
	}
	if err := window.ObserveSend(second); err != nil {
		t.Fatalf("send after ack failed: %v", err)
	}
	cancel, err := NewFileDownloadCancelFrame(42, "user cancelled")
	if err != nil {
		t.Fatal(err)
	}
	if cancel.Type != TunnelFrameStreamReset || cancel.Metadata["reason"] != FileDownloadErrorCancelled {
		t.Fatalf("invalid cancel frame: %#v", cancel)
	}
}

func TestFileDownloadErrorCodesAreStable(t *testing.T) {
	for _, code := range []string{
		FileDownloadErrorCancelled,
		FileDownloadErrorWindowExceeded,
		FileDownloadErrorDenied,
		FileDownloadErrorFailed,
	} {
		if _, err := NewFileDownloadErrorFrame(42, code, map[string]string{"message": "x"}); err != nil {
			t.Fatalf("code %s rejected: %v", code, err)
		}
	}
	if _, err := NewFileDownloadErrorFrame(42, "unknown", nil); err == nil {
		t.Fatal("accepted unknown file download error code")
	}
}
