package adapter

import (
	"path/filepath"
	"regexp"
	"strings"

	"mobilevc/internal/protocol"
)

var (
	pythonTracebackStartPattern = regexp.MustCompile(`^Traceback \(most recent call last\):$`)
	javaExceptionStartPattern   = regexp.MustCompile(`^Exception in thread `)
	javaStackLinePattern        = regexp.MustCompile(`^\s+at\s+`)
	pythonExceptionLinePattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_\.]*Error: .+|^[A-Za-z_][A-Za-z0-9_\.]*Exception: .+`)
	javaCauseLinePattern        = regexp.MustCompile(`^Caused by: `)
	stepLinePattern             = regexp.MustCompile(`^(?:[•*\-]\s*)?(Installing|Reading|Updating|Creating|Running|Thinking|Analyzing|Writing|Editing|Checking|Loading|Searching|Inspecting|Planning|Applying|Resolving|Building|Compiling|Testing|Reviewing|Opening|Exploring)\b.*$`)
	stepDonePattern             = regexp.MustCompile(`^(?:[•*\-]\s*)?(Done|Completed|Finished|Resolved)\b.*$`)
	pathTailPattern             = regexp.MustCompile(`([A-Za-z0-9_./\\-]+\.[A-Za-z0-9_+-]+)$`)
	diffGitStartPattern         = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)
	diffHunkPattern             = regexp.MustCompile(`^@@ .+ @@`)
	patchFilePattern            = regexp.MustCompile(`^\*\*\* (Update|Add|Delete) File:\s+(.+)$`)
)

type GenericParser struct {
	buffer     []string
	buffering  bool
	errorKind  string
	diffBuffer []string
	diffing    bool
	diffPath   string
	diffTitle  string
	diffLang   string
	patchStyle bool
}

func NewGenericParser() *GenericParser {
	return &GenericParser{}
}

func (p *GenericParser) Detect(dir string) bool {
	return true
}

func (p *GenericParser) ParseLine(line string, sessionID string, stream string) []any {
	trimmed := strings.TrimRight(line, "\r\n")
	trimmed = strings.TrimLeft(trimmed, "\r")
	return p.parseLine(trimmed, sessionID, stream)
}

func (p *GenericParser) parseLine(line string, sessionID string, stream string) []any {
	if p.buffering {
		if p.shouldContinueErrorBuffer(line) {
			p.buffer = append(p.buffer, line)
			return nil
		}

		events := p.flushBufferedError(sessionID)
		if line == "" {
			return events
		}
		return append(events, p.parseLine(line, sessionID, stream)...)
	}

	if p.diffing {
		if p.shouldContinueDiffBuffer(line) {
			p.appendDiffLine(line)
			if p.patchStyle && line == "*** End Patch" {
				return p.flushBufferedDiff(sessionID)
			}
			return nil
		}

		events := p.flushBufferedDiff(sessionID)
		if line == "" {
			return events
		}
		return append(events, p.parseLine(line, sessionID, stream)...)
	}

	if p.isPythonStart(line) {
		p.startErrorBuffer("python", line)
		return nil
	}
	if p.isJavaStart(line) {
		p.startErrorBuffer("java", line)
		return nil
	}
	if p.isDiffStart(line) {
		p.startDiffBuffer(line)
		if p.patchStyle && line == "*** End Patch" {
			return p.flushBufferedDiff(sessionID)
		}
		return nil
	}
	if message, status, target, ok := p.detectStepUpdate(line); ok {
		tool, command := detectStepToolAndCommand(message)
		return []any{protocol.NewStepUpdateEvent(sessionID, message, status, target, tool, command)}
	}
	return []any{protocol.NewLogEvent(sessionID, line, stream)}
}

func (p *GenericParser) Flush(sessionID string, stream string) []any {
	var events []any
	if p.buffering {
		events = append(events, p.flushBufferedError(sessionID)...)
	}
	if p.diffing {
		events = append(events, p.flushBufferedDiff(sessionID)...)
	}
	return events
}

func (p *GenericParser) HasPendingDiff() bool {
	return p.diffing && len(p.diffBuffer) > 0
}

func (p *GenericParser) startErrorBuffer(kind, firstLine string) {
	p.buffering = true
	p.errorKind = kind
	p.buffer = []string{firstLine}
}

func (p *GenericParser) shouldContinueErrorBuffer(line string) bool {
	if line == "" {
		return true
	}

	switch p.errorKind {
	case "python":
		return strings.HasPrefix(line, "  File ") || strings.HasPrefix(line, "    ") || pythonExceptionLinePattern.MatchString(line)
	case "java":
		return javaStackLinePattern.MatchString(line) || javaCauseLinePattern.MatchString(line) || strings.HasPrefix(line, "\t...") || strings.HasPrefix(line, "\tSuppressed: ")
	default:
		return false
	}
}

func (p *GenericParser) flushBufferedError(sessionID string) []any {
	stack := strings.Join(p.buffer, "\n")
	message := p.lastNonEmptyLine(p.buffer)
	if message == "" && len(p.buffer) > 0 {
		message = p.buffer[0]
	}

	event := protocol.NewErrorEvent(sessionID, message, stack)
	p.buffer = nil
	p.buffering = false
	p.errorKind = ""
	return []any{event}
}

func (p *GenericParser) startDiffBuffer(firstLine string) {
	p.diffing = true
	p.diffBuffer = nil
	p.diffPath = ""
	p.diffTitle = ""
	p.diffLang = ""
	p.patchStyle = strings.HasPrefix(firstLine, "*** ")
	p.appendDiffLine(firstLine)
}

func (p *GenericParser) appendDiffLine(line string) {
	p.diffBuffer = append(p.diffBuffer, line)
	p.captureDiffMetadata(line)
}

func (p *GenericParser) captureDiffMetadata(line string) {
	if matches := diffGitStartPattern.FindStringSubmatch(line); len(matches) == 3 {
		p.setDiffPath(matches[2])
		if p.diffTitle == "" {
			p.diffTitle = "Updating " + matches[2]
		}
		return
	}

	if matches := patchFilePattern.FindStringSubmatch(line); len(matches) == 3 {
		action := matches[1]
		path := strings.TrimSpace(matches[2])
		p.setDiffPath(path)
		p.diffTitle = action + " " + path
		return
	}

	if strings.HasPrefix(line, "+++ b/") {
		p.setDiffPath(strings.TrimPrefix(line, "+++ b/"))
		return
	}
	if strings.HasPrefix(line, "--- a/") && p.diffPath == "" {
		p.setDiffPath(strings.TrimPrefix(line, "--- a/"))
		return
	}
}

func (p *GenericParser) setDiffPath(path string) {
	path = strings.TrimSpace(path)
	if path == "" || path == "/dev/null" {
		return
	}
	p.diffPath = path
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	if ext != "" {
		p.diffLang = ext
	}
	if p.diffTitle == "" {
		p.diffTitle = "Updating " + path
	}
}

func (p *GenericParser) shouldContinueDiffBuffer(line string) bool {
	if line == "" {
		return true
	}
	if p.patchStyle {
		return strings.HasPrefix(line, "*** ") || strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, " ")
	}
	if p.isDiffStart(line) {
		return true
	}
	return strings.HasPrefix(line, "index ") ||
		strings.HasPrefix(line, "new file mode ") ||
		strings.HasPrefix(line, "deleted file mode ") ||
		strings.HasPrefix(line, "similarity index ") ||
		strings.HasPrefix(line, "rename from ") ||
		strings.HasPrefix(line, "rename to ") ||
		strings.HasPrefix(line, "--- ") ||
		strings.HasPrefix(line, "+++ ") ||
		diffHunkPattern.MatchString(line) ||
		strings.HasPrefix(line, "+") ||
		strings.HasPrefix(line, "-") ||
		strings.HasPrefix(line, " ") ||
		strings.HasPrefix(line, `\\ No newline at end of file`)
}

func (p *GenericParser) flushBufferedDiff(sessionID string) []any {
	diff := strings.Join(p.diffBuffer, "\n")
	path := p.diffPath
	title := p.diffTitle
	lang := p.diffLang
	if title == "" {
		if path != "" {
			title = "Updating " + path
		} else {
			title = "File diff"
		}
	}
	p.diffBuffer = nil
	p.diffing = false
	p.diffPath = ""
	p.diffTitle = ""
	p.diffLang = ""
	p.patchStyle = false
	return []any{protocol.NewFileDiffEvent(sessionID, path, title, diff, lang)}
}

func (p *GenericParser) detectStepUpdate(line string) (string, string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", "", "", false
	}
	if len(trimmed) > 140 {
		return "", "", "", false
	}
	if strings.Contains(trimmed, "$") || strings.Contains(trimmed, "|") || strings.Contains(trimmed, "	") {
		return "", "", "", false
	}
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "(") || strings.HasPrefix(trimmed, "{") {
		return "", "", "", false
	}
	if strings.Contains(trimmed, "://") {
		return "", "", "", false
	}
	if !(stepLinePattern.MatchString(trimmed) || stepDonePattern.MatchString(trimmed)) {
		return "", "", "", false
	}

	status := "running"
	if stepDonePattern.MatchString(trimmed) {
		status = "done"
	} else if strings.HasSuffix(trimmed, "...") {
		status = "info"
	}

	target := ""
	if matches := pathTailPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
		target = matches[1]
	}
	return trimmed, status, target, true
}

func detectStepToolAndCommand(message string) (string, string) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return "", ""
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return "", ""
	}
	tool := strings.ToLower(parts[0])
	return tool, trimmed
}

func (p *GenericParser) lastNonEmptyLine(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return strings.TrimSpace(lines[i])
		}
	}
	return ""
}

func (p *GenericParser) isPythonStart(line string) bool {
	return pythonTracebackStartPattern.MatchString(line)
}

func (p *GenericParser) isJavaStart(line string) bool {
	return javaExceptionStartPattern.MatchString(line)
}

func (p *GenericParser) isDiffStart(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	return diffGitStartPattern.MatchString(trimmed) ||
		strings.HasPrefix(trimmed, "*** Begin Patch") ||
		patchFilePattern.MatchString(trimmed) ||
		strings.HasPrefix(trimmed, "--- ") ||
		strings.HasPrefix(trimmed, "+++ ") ||
		diffHunkPattern.MatchString(trimmed)
}
