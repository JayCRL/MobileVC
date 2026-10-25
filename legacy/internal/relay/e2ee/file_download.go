package e2ee

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	FileDownloadDefaultChunkSize = 256 * 1024
	FileDownloadMaxChunkSize     = 512 * 1024

	FileDownloadErrorCancelled      = "stream_cancelled"
	FileDownloadErrorWindowExceeded = "stream_window_exceeded"
	FileDownloadErrorDenied         = "download_denied"
	FileDownloadErrorFailed         = "download_failed"
)

type FileDownloadMetadata struct {
	Path        string
	FileName    string
	ContentType string
	Size        *int64
}

func NewFileDownloadOpenFrame(streamID uint64, metadata FileDownloadMetadata, window uint32) (TunnelFrame, error) {
	frame := TunnelFrame{
		Type: TunnelFrameStreamOpen, Version: TunnelVersion, StreamID: streamID,
		StreamType: TunnelStreamFileDownload, Window: window,
		Metadata: metadata.toMap(),
	}
	return frame, ValidateFileDownloadFrame(frame, FileDownloadDefaultChunkSize)
}

func NewFileDownloadDataFrame(streamID, seq uint64, chunk []byte, maxChunkSize int) (TunnelFrame, error) {
	frame := TunnelFrame{
		Type: TunnelFrameStreamData, Version: TunnelVersion,
		StreamID: streamID, Seq: seq, Payload: append([]byte(nil), chunk...),
	}
	return frame, ValidateFileDownloadFrame(frame, maxChunkSize)
}

func NewFileDownloadAckFrame(streamID, ack uint64, window uint32) (TunnelFrame, error) {
	frame := TunnelFrame{
		Type: TunnelFrameStreamAck, Version: TunnelVersion,
		StreamID: streamID, Ack: ack, Window: window,
	}
	return frame, ValidateFileDownloadFrame(frame, FileDownloadDefaultChunkSize)
}

func NewFileDownloadCloseFrame(streamID, seq uint64) (TunnelFrame, error) {
	frame := TunnelFrame{
		Type: TunnelFrameStreamClose, Version: TunnelVersion,
		StreamID: streamID, Seq: seq,
	}
	return frame, ValidateFileDownloadFrame(frame, FileDownloadDefaultChunkSize)
}

func NewFileDownloadCancelFrame(streamID uint64, reason string) (TunnelFrame, error) {
	frame := TunnelFrame{
		Type: TunnelFrameStreamReset, Version: TunnelVersion,
		StreamID: streamID, Metadata: map[string]string{"reason": FileDownloadErrorCancelled},
	}
	if strings.TrimSpace(reason) != "" {
		frame.Metadata["message"] = reason
	}
	return frame, ValidateFileDownloadFrame(frame, FileDownloadDefaultChunkSize)
}

func NewFileDownloadErrorFrame(streamID uint64, code string, metadata map[string]string) (TunnelFrame, error) {
	frame := TunnelFrame{
		Type: TunnelFrameStreamError, Version: TunnelVersion,
		StreamID: streamID, ErrorCode: code, Metadata: sortedMetadata(metadata),
	}
	return frame, ValidateFileDownloadFrame(frame, FileDownloadDefaultChunkSize)
}

func ValidateFileDownloadFrame(frame TunnelFrame, maxChunkSize int) error {
	if err := ValidateTunnelFrame(frame); err != nil {
		return err
	}
	if err := validateFileDownloadShape(frame); err != nil {
		return err
	}
	if frame.Type == TunnelFrameStreamData {
		return validateFileDownloadChunk(frame.Payload, maxChunkSize)
	}
	return nil
}

type FileDownloadChunker struct {
	reader    io.Reader
	chunkSize int
}

func NewFileDownloadChunker(reader io.Reader, chunkSize int) (*FileDownloadChunker, error) {
	if reader == nil {
		return nil, errors.New("download reader is required")
	}
	normalized, err := normalizeFileDownloadChunkSize(chunkSize)
	if err != nil {
		return nil, err
	}
	return &FileDownloadChunker{reader: reader, chunkSize: normalized}, nil
}

func (c *FileDownloadChunker) Next() ([]byte, error) {
	buffer := make([]byte, c.chunkSize)
	n, err := io.ReadFull(c.reader, buffer)
	if err == nil {
		return buffer, nil
	}
	if errors.Is(err, io.EOF) && n == 0 {
		return nil, io.EOF
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || n > 0 {
		return buffer[:n], nil
	}
	return nil, err
}

type FileDownloadSendWindow struct {
	window       uint32
	maxChunkSize int
	inFlight     map[uint64]struct{}
}

func NewFileDownloadSendWindow(window uint32) (*FileDownloadSendWindow, error) {
	return NewFileDownloadSendWindowWithChunkSize(window, FileDownloadDefaultChunkSize)
}

func NewFileDownloadSendWindowWithChunkSize(window uint32, maxChunkSize int) (*FileDownloadSendWindow, error) {
	if window == 0 {
		return nil, errors.New(FileDownloadErrorWindowExceeded)
	}
	normalized, err := normalizeFileDownloadChunkSize(maxChunkSize)
	if err != nil {
		return nil, err
	}
	return &FileDownloadSendWindow{
		window: window, maxChunkSize: normalized, inFlight: map[uint64]struct{}{},
	}, nil
}

func (w *FileDownloadSendWindow) ObserveSend(frame TunnelFrame) error {
	if err := ValidateFileDownloadFrame(frame, w.maxChunkSize); err != nil {
		return err
	}
	if frame.Type != TunnelFrameStreamData {
		return nil
	}
	if uint32(len(w.inFlight)) >= w.window {
		return errors.New(FileDownloadErrorWindowExceeded)
	}
	w.inFlight[frame.Seq] = struct{}{}
	return nil
}

func (w *FileDownloadSendWindow) ObserveAck(frame TunnelFrame) error {
	if err := ValidateFileDownloadFrame(frame, FileDownloadDefaultChunkSize); err != nil {
		return err
	}
	if frame.Type != TunnelFrameStreamAck {
		return nil
	}
	w.window = frame.Window
	for seq := range w.inFlight {
		if seq <= frame.Ack {
			delete(w.inFlight, seq)
		}
	}
	return nil
}

func (m FileDownloadMetadata) toMap() map[string]string {
	out := map[string]string{"path": m.Path}
	if strings.TrimSpace(m.FileName) != "" {
		out["fileName"] = m.FileName
	}
	if strings.TrimSpace(m.ContentType) != "" {
		out["contentType"] = m.ContentType
	}
	if m.Size != nil && *m.Size >= 0 {
		out["size"] = strconv.FormatInt(*m.Size, 10)
	}
	return out
}

func validateFileDownloadShape(frame TunnelFrame) error {
	if frame.Type == TunnelFrameStreamOpen && frame.StreamType != TunnelStreamFileDownload {
		return errors.New("file download stream.open must use file.download")
	}
	if frame.Type == TunnelFrameStreamOpen && strings.TrimSpace(frame.Metadata["path"]) == "" {
		return errors.New("file download path is required")
	}
	if frame.Type == TunnelFrameStreamError && !isFileDownloadErrorCode(frame.ErrorCode) {
		return fmt.Errorf("unknown file download error code: %s", frame.ErrorCode)
	}
	return nil
}

func validateFileDownloadChunk(chunk []byte, maxChunkSize int) error {
	normalized, err := normalizeFileDownloadChunkSize(maxChunkSize)
	if err != nil {
		return err
	}
	if len(chunk) == 0 {
		return errors.New("file download chunk is empty")
	}
	if len(chunk) > normalized {
		return fmt.Errorf("file download chunk exceeds %d bytes", normalized)
	}
	return nil
}

func normalizeFileDownloadChunkSize(size int) (int, error) {
	if size == 0 {
		return FileDownloadDefaultChunkSize, nil
	}
	if size < 0 || size > FileDownloadMaxChunkSize {
		return 0, fmt.Errorf("invalid file download chunk size: %d", size)
	}
	return size, nil
}

func isFileDownloadErrorCode(code string) bool {
	switch code {
	case FileDownloadErrorCancelled, FileDownloadErrorWindowExceeded, FileDownloadErrorDenied, FileDownloadErrorFailed:
		return true
	default:
		return false
	}
}
