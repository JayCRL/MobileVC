package session

import (
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"mobilevc/internal/protocol"
)

var (
	markdownImagePathPattern = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)
	localPathPattern         = regexp.MustCompile("(?m)(?:^|[\\s('\\\"\\[])(/[^\\s)'\\\"<>]+)")
)

func TimelineAttachmentsFromText(text, source string) []protocol.TimelineAttachment {
	paths := timelineAttachmentPaths(text)
	if len(paths) == 0 {
		return nil
	}
	attachments := make([]protocol.TimelineAttachment, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		cleanPath := strings.TrimSpace(path)
		if cleanPath == "" {
			continue
		}
		absPath, err := filepath.Abs(filepath.Clean(cleanPath))
		if err != nil {
			continue
		}
		if _, ok := seen[absPath]; ok {
			continue
		}
		seen[absPath] = struct{}{}
		attachments = append(attachments, timelineAttachmentForPath(absPath, source))
	}
	return attachments
}

func timelineAttachmentPaths(text string) []string {
	matches := markdownImagePathPattern.FindAllStringSubmatch(text, -1)
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) <= 1 {
			continue
		}
		if path, ok := normalizeLocalAttachmentPath(match[1]); ok {
			paths = append(paths, path)
		}
	}
	for _, match := range localPathPattern.FindAllStringSubmatch(text, -1) {
		if len(match) <= 1 {
			continue
		}
		if path, ok := normalizeLocalAttachmentPath(match[1]); ok {
			paths = append(paths, path)
		}
	}
	return paths
}

func normalizeLocalAttachmentPath(path string) (string, bool) {
	trimmed := strings.Trim(strings.TrimSpace(path), ".,;:!?")
	if !strings.HasPrefix(trimmed, "/") {
		return "", false
	}
	ext := strings.ToLower(filepath.Ext(trimmed))
	if ext == "" {
		return "", false
	}
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".heic", ".heif",
		".pdf", ".txt", ".md", ".json", ".yaml", ".yml", ".csv", ".tsv",
		".zip", ".log", ".dart", ".go", ".js", ".ts", ".tsx", ".jsx":
		return trimmed, true
	default:
		return "", false
	}
}

func timelineAttachmentForPath(path string, source string) protocol.TimelineAttachment {
	trimmed := strings.TrimSpace(path)
	name := filepath.Base(trimmed)
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(trimmed)))
	kind := "file"
	if strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		kind = "image"
	}
	var size int64
	previewStatus := "pending"
	if info, err := os.Stat(trimmed); err == nil {
		size = info.Size()
		if info.IsDir() {
			previewStatus = "unsupported"
		}
	}
	return protocol.TimelineAttachment{
		ID:            attachmentIDForPath(trimmed),
		Kind:          kind,
		Name:          name,
		MIMEType:      mimeType,
		Size:          size,
		Path:          trimmed,
		PreviewStatus: previewStatus,
		Source:        source,
	}
}

func attachmentIDForPath(path string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	id := strings.Trim(replacer.Replace(path), "_")
	if id == "" {
		return fmt.Sprintf("path-%x", path)
	}
	return id
}
