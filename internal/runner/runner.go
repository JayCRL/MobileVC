package runner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"mobilevc/internal/protocol"
)

type Mode string

const (
	ModeExec Mode = "exec"
	ModePTY  Mode = "pty"
)

var ErrInputNotSupported = errors.New("input not supported")
var ErrNoPendingControlRequest = errors.New("no pending control request")

type ExecRequest struct {
	Command        string
	CWD            string
	SessionID      string
	Mode           Mode
	PermissionMode string
	protocol.RuntimeMeta
}

type EventSink func(event any)

type Runner interface {
	Run(ctx context.Context, req ExecRequest, sink EventSink) error
	Write(ctx context.Context, data []byte) error
	Close() error
}

type InteractiveStateProvider interface {
	CanAcceptInteractiveInput() bool
}

type PermissionResponseWriter interface {
	WritePermissionResponse(ctx context.Context, decision string) error
	HasPendingPermissionRequest() bool
}

type ClaudeSessionProvider interface {
	ClaudeSessionID() string
}

type shellSpec struct {
	path              string
	args              []string
	gitBash           string
	winpty            string
	windowsPowerShell string
	windowsCmd        string
	claudeNode        string
	claudeCLI         string
}

func newShellCommand(ctx context.Context, command string, mode Mode) *exec.Cmd {
	spec := getShellSpec()
	if runtime.GOOS == "windows" && shouldUseWindowsClaudeEntry(command, spec) && mode != ModePTY {
		nodeEntry := spec.claudeNode
		cliEntry := spec.claudeCLI
		if shortNode, err := windowsShortPath(nodeEntry); err == nil && shortNode != "" {
			nodeEntry = shortNode
		}
		if shortCLI, err := windowsShortPath(cliEntry); err == nil && shortCLI != "" {
			cliEntry = shortCLI
		}
		if spec.winpty != "" && spec.gitBash != "" {
			wrappedParts := []string{
				"winpty",
				"-Xallow-non-tty",
				shellEscapeForBash(windowsPathForMSYS(nodeEntry)),
				shellEscapeForBash(windowsPathForMSYS(cliEntry)),
			}
			for _, arg := range claudeCommandArgs(command) {
				wrappedParts = append(wrappedParts, shellEscapeForBash(arg))
			}
			cmd := exec.CommandContext(ctx, spec.gitBash, "-lc", strings.Join(wrappedParts, " "))
			cmd.Env = shellEnvironment(spec, command)
			return cmd
		}
		args := append([]string{cliEntry}, claudeCommandArgs(command)...)
		cmd := exec.CommandContext(ctx, nodeEntry, args...)
		cmd.Env = shellEnvironment(spec, command)
		return cmd
	}
	preparedCommand := prepareShellCommand(command, spec, mode)
	cmd := exec.CommandContext(ctx, spec.path, append(spec.args, preparedCommand)...)
	cmd.Env = shellEnvironment(spec, command)
	return cmd
}

func newClaudeStreamCommand(ctx context.Context, command string, resumeSessionID string, permissionMode string) *exec.Cmd {
	spec := getShellSpec()
	if spec.claudeNode != "" && spec.claudeCLI != "" {
		nodeEntry := spec.claudeNode
		cliEntry := spec.claudeCLI
		if shortNode, err := windowsShortPath(nodeEntry); err == nil && shortNode != "" {
			nodeEntry = shortNode
		}
		if shortCLI, err := windowsShortPath(cliEntry); err == nil && shortCLI != "" {
			cliEntry = shortCLI
		}
		args := []string{cliEntry}
		base := strings.TrimSpace(command)
		if base != "" {
			for _, arg := range claudeCommandArgs(base) {
				args = append(args, arg)
			}
		}
		if resumeSessionID != "" && !containsArg(args, "--resume") {
			args = append(args, "--resume", resumeSessionID)
		}
		if !containsArg(args, "--print") && !containsArg(args, "-p") {
			args = append(args, "--print")
		}
		if !containsArg(args, "--verbose") {
			args = append(args, "--verbose")
		}
		if !containsArg(args, "--output-format") {
			args = append(args, "--output-format", "stream-json")
		}
		if !containsArg(args, "--input-format") {
			args = append(args, "--input-format", "stream-json")
		}
		args = appendPermissionPromptTool(args)
		args = appendPermissionMode(args, permissionMode)
		cmd := exec.CommandContext(ctx, nodeEntry, args...)
		cmd.Env = shellEnvironment(spec, command)
		return cmd
	}
	preparedCommand := buildClaudeStreamJSONCommand(command)
	if resumeSessionID != "" && !strings.Contains(strings.ToLower(preparedCommand), " --resume") {
		preparedCommand += " --resume " + shellEscapeForBash(resumeSessionID)
	}
	preparedCommand = appendPermissionModeToCommand(preparedCommand, permissionMode)
	cmd := exec.CommandContext(ctx, spec.path, append(spec.args, preparedCommand)...)
	cmd.Env = shellEnvironment(spec, command)
	return cmd
}

func newClaudePromptCommand(ctx context.Context, command string, prompt string, resumeSessionID string, permissionMode string) *exec.Cmd {
	spec := getShellSpec()
	if spec.claudeNode != "" && spec.claudeCLI != "" {
		nodeEntry := spec.claudeNode
		cliEntry := spec.claudeCLI
		if shortNode, err := windowsShortPath(nodeEntry); err == nil && shortNode != "" {
			nodeEntry = shortNode
		}
		if shortCLI, err := windowsShortPath(cliEntry); err == nil && shortCLI != "" {
			cliEntry = shortCLI
		}
		args := []string{cliEntry}
		base := strings.TrimSpace(command)
		if base != "" {
			for _, arg := range claudeCommandArgs(base) {
				args = append(args, arg)
			}
		}
		if resumeSessionID != "" {
			args = append(args, "--resume", resumeSessionID)
		}
		args = append(args, "--print", "--verbose", "--output-format", "stream-json")
		args = appendPermissionMode(args, permissionMode)
		args = append(args, prompt)
		cmd := exec.CommandContext(ctx, nodeEntry, args...)
		cmd.Env = shellEnvironment(spec, command)
		return cmd
	}
	preparedCommand := buildClaudePromptCommand(command, prompt, resumeSessionID)
	// permission-mode must be before prompt (positional arg)
	idx := strings.LastIndex(preparedCommand, shellEscapeForBash(prompt))
	if idx > 0 && permissionMode != "" {
		before := preparedCommand[:idx]
		after := preparedCommand[idx:]
		if !strings.Contains(strings.ToLower(before), "--permission-mode") {
			preparedCommand = before + "--permission-mode " + permissionMode + " " + after
		}
	} else {
		preparedCommand = appendPermissionModeToCommand(preparedCommand, permissionMode)
	}
	cmd := exec.CommandContext(ctx, spec.path, append(spec.args, preparedCommand)...)
	cmd.Env = shellEnvironment(spec, command)
	return cmd
}

func shouldUseWindowsClaudeEntry(command string, spec shellSpec) bool {
	return spec.claudeNode != "" && spec.claudeCLI != "" && isClaudeCommandName(command)
}

func prepareShellCommand(command string, spec shellSpec, mode Mode) string {
	if runtime.GOOS == "windows" && mode == ModePTY && spec.gitBash != "" && spec.winpty != "" && shouldWrapWithWinPTY(command) {
		return shellEscapeForBash(spec.winpty) + " -Xallow-non-tty " + shellEscapeForBash(spec.gitBash) + " -lc " + shellEscapeForBash(command)
	}
	return command
}

func shouldWrapWithWinPTY(command string) bool {
	return isAICommandName(command)
}

func isClaudeCommandName(command string) bool {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return false
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	head := strings.ToLower(fields[0])
	return head == "claude" || strings.HasSuffix(head, "/claude") || strings.HasSuffix(head, `\\claude`) || head == "claude.exe" || head == "claude.cmd" || head == "claude.ps1"
}

func isAICommandName(command string) bool {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return false
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	head := strings.ToLower(fields[0])
	isClaude := head == "claude" || strings.HasSuffix(head, "/claude") || strings.HasSuffix(head, `\\claude`) || head == "claude.exe" || head == "claude.cmd" || head == "claude.ps1"
	isGemini := head == "gemini" || strings.HasSuffix(head, "/gemini") || strings.HasSuffix(head, `\\gemini`) || head == "gemini.exe" || head == "gemini.cmd" || head == "gemini.ps1"
	isCodex := head == "codex" || strings.HasSuffix(head, "/codex") || strings.HasSuffix(head, `\\codex`) || head == "codex.exe" || head == "codex.cmd" || head == "codex.ps1"
	return isClaude || isGemini || isCodex
}

func claudeCommandArgs(command string) []string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) <= 1 {
		return nil
	}
	return append([]string(nil), fields[1:]...)
}

func buildClaudeStreamJSONCommand(command string) string {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		trimmed = "claude"
	}
	parts := []string{trimmed}
	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, " --print") && !strings.Contains(lower, " -p") {
		parts = append(parts, "--print")
	}
	if !strings.Contains(lower, " --verbose") {
		parts = append(parts, "--verbose")
	}
	if !strings.Contains(lower, "--output-format") {
		parts = append(parts, "--output-format", "stream-json")
	}
	if !strings.Contains(lower, "--input-format") {
		parts = append(parts, "--input-format", "stream-json")
	}
	if !strings.Contains(lower, "--permission-prompt-tool") {
		parts = append(parts, "--permission-prompt-tool", "stdio")
	}
	return strings.Join(parts, " ")
}

func buildClaudePromptCommand(command string, prompt string, resumeSessionID string) string {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		trimmed = "claude"
	}
	parts := []string{trimmed}
	lower := strings.ToLower(trimmed)
	if resumeSessionID != "" && !strings.Contains(lower, " --resume") {
		parts = append(parts, "--resume", resumeSessionID)
	}
	if !strings.Contains(lower, " --print") && !strings.Contains(lower, " -p") {
		parts = append(parts, "--print")
	}
	if !strings.Contains(lower, " --verbose") {
		parts = append(parts, "--verbose")
	}
	if !strings.Contains(lower, "--output-format") {
		parts = append(parts, "--output-format", "stream-json")
	}
	parts = append(parts, shellEscapeForBash(prompt))
	return strings.Join(parts, " ")
}

func appendPermissionPromptTool(args []string) []string {
	for _, arg := range args {
		if arg == "--permission-prompt-tool" {
			return args
		}
	}
	return append(args, "--permission-prompt-tool", "stdio")
}

func appendPermissionMode(args []string, permissionMode string) []string {
	if permissionMode == "" {
		return args
	}
	for _, a := range args {
		if a == "--permission-mode" {
			return args
		}
	}
	return append(args, "--permission-mode", permissionMode)
}

func appendPermissionModeToCommand(command string, permissionMode string) string {
	if permissionMode == "" {
		return command
	}
	if strings.Contains(strings.ToLower(command), "--permission-mode") {
		return command
	}
	return command + " --permission-mode " + permissionMode
}

func containsArg(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}

func shellEscapeForBash(path string) string {
	path = strings.ReplaceAll(path, `\`, `/`)
	path = strings.ReplaceAll(path, `'`, `'''"'"'''`)
	return `'` + path + `'`
}

func getShellSpec() shellSpec {
	if runtime.GOOS != "windows" {
		if zshPath, err := exec.LookPath("zsh"); err == nil && zshPath != "" {
			return shellSpec{path: zshPath, args: []string{"-lc"}}
		}
		if shPath, err := exec.LookPath("sh"); err == nil && shPath != "" {
			return shellSpec{path: shPath, args: []string{"-lc"}}
		}
		return shellSpec{path: "sh", args: []string{"-lc"}}
	}

	if powershellPath := detectWindowsShellPath([]string{
		filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
		"powershell.exe",
	}); powershellPath != "" {
		claudeNode, claudeCLI := detectClaudeNodeCLI()
		if bashPath := detectGitBashPath(); bashPath != "" {
			cmdPath := detectWindowsShellPath([]string{
				filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"),
				"cmd.exe",
			})
			return shellSpec{
				path:              bashPath,
				args:              []string{"-lc"},
				gitBash:           bashPath,
				winpty:            detectWinPTYPath(bashPath),
				windowsPowerShell: powershellPath,
				windowsCmd:        cmdPath,
				claudeNode:        claudeNode,
				claudeCLI:         claudeCLI,
			}
		}
		return shellSpec{
			path:              powershellPath,
			args:              []string{"-NoLogo", "-NoProfile", "-Command"},
			windowsPowerShell: powershellPath,
			windowsCmd: detectWindowsShellPath([]string{
				filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"),
				"cmd.exe",
			}),
			claudeNode: claudeNode,
			claudeCLI:  claudeCLI,
		}
	}

	if bashPath := detectGitBashPath(); bashPath != "" {
		return shellSpec{
			path:    bashPath,
			args:    []string{"-lc"},
			gitBash: bashPath,
			winpty:  detectWinPTYPath(bashPath),
		}
	}

	if cmdPath := detectWindowsShellPath([]string{
		filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"),
		"cmd.exe",
	}); cmdPath != "" {
		return shellSpec{
			path: cmdPath,
			args: []string{"/C"},
		}
	}

	return shellSpec{path: "sh", args: []string{"-lc"}}
}

func shellEnvironment(spec shellSpec, command string) []string {
	env := os.Environ()
	if isClaudeCommandName(command) {
		env = removeEnv(env, "CLAUDECODE")
	}
	if runtime.GOOS == "windows" && spec.gitBash != "" {
		env = upsertEnv(env, "CLAUDE_CODE_GIT_BASH_PATH", spec.gitBash)
	}
	// 仅对真正的 claude 命令注入 FORCE_TTY 相关的变量
	if runtime.GOOS == "windows" && spec.claudeCLI != "" && isClaudeCommandName(command) {
		env = upsertEnv(env, "MOBILEVC_FORCE_TTY", "1")
	}
	env = upsertEnv(env, "FORCE_COLOR", "1")
	env = upsertEnv(env, "CLICOLOR_FORCE", "1")
	env = upsertEnv(env, "TERM", "xterm-256color")
	return env
}

func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(strings.ToUpper(item), strings.ToUpper(prefix)) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func removeEnv(env []string, key string) []string {
	prefix := strings.ToUpper(key + "=")
	filtered := env[:0]
	for _, item := range env {
		if strings.HasPrefix(strings.ToUpper(item), prefix) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func detectGitBashPath() string {
	candidates := []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\Git\usr\bin\bash.exe`,
		`C:\Program Files (x86)\Git\bin\bash.exe`,
		`C:\Program Files (x86)\Git\usr\bin\bash.exe`,
	}

	if programFiles := strings.TrimSpace(os.Getenv("ProgramFiles")); programFiles != "" {
		candidates = append([]string{
			filepath.Join(programFiles, "Git", "bin", "bash.exe"),
			filepath.Join(programFiles, "Git", "usr", "bin", "bash.exe"),
		}, candidates...)
	}
	if programFilesX86 := strings.TrimSpace(os.Getenv("ProgramFiles(x86)")); programFilesX86 != "" {
		candidates = append([]string{
			filepath.Join(programFilesX86, "Git", "bin", "bash.exe"),
			filepath.Join(programFilesX86, "Git", "usr", "bin", "bash.exe"),
		}, candidates...)
	}
	if pathBash := detectWindowsShellPath([]string{"bash.exe", "bash"}); pathBash != "" {
		candidates = append([]string{pathBash}, candidates...)
	}
	if gitPath := detectWindowsShellPath([]string{"git.exe", "git"}); gitPath != "" {
		candidates = append([]string{inferGitBashFromGitPath(gitPath)}, candidates...)
	}

	return detectWindowsShellPath(candidates)
}

func detectWinPTYPath(gitBashPath string) string {
	gitBashPath = strings.TrimSpace(gitBashPath)
	if gitBashPath == "" {
		return ""
	}
	candidates := []string{
		filepath.Join(filepath.Dir(gitBashPath), "winpty.exe"),
		filepath.Join(filepath.Dir(filepath.Dir(gitBashPath)), "mingw64", "bin", "winpty.exe"),
		filepath.Join(filepath.Dir(filepath.Dir(gitBashPath)), "usr", "bin", "winpty.exe"),
		"winpty.exe",
		"winpty",
	}
	return detectWindowsShellPath(candidates)
}

func inferGitBashFromGitPath(gitPath string) string {
	gitPath = strings.TrimSpace(gitPath)
	if gitPath == "" {
		return ""
	}
	gitPath = filepath.Clean(gitPath)
	base := filepath.Dir(gitPath)
	if strings.EqualFold(filepath.Base(base), "cmd") || strings.EqualFold(filepath.Base(base), "bin") {
		root := filepath.Dir(base)
		candidate := filepath.Join(root, "usr", "bin", "bash.exe")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	candidate := filepath.Join(filepath.Dir(base), "usr", "bin", "bash.exe")
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate
	}
	return ""
}

func windowsPathForMSYS(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = strings.ReplaceAll(path, `\`, "/")
	if len(path) >= 2 && path[1] == ':' {
		drive := strings.ToLower(path[:1])
		rest := strings.TrimPrefix(path[2:], "/")
		if rest == "" {
			return "/" + drive
		}
		return "/" + drive + "/" + rest
	}
	return path
}

func detectClaudeNodeCLI() (string, string) {
	aliasBaseDir := `D:\claude-nodejs`
	if info, err := os.Stat(aliasBaseDir); err == nil && info.IsDir() {
		cliPath := filepath.Join(aliasBaseDir, "node_modules", "@anthropic-ai", "claude-code", "cli.js")
		nodePath := filepath.Join(aliasBaseDir, "node.exe")
		if cliInfo, cliErr := os.Stat(cliPath); cliErr == nil && !cliInfo.IsDir() {
			if nodeInfo, nodeErr := os.Stat(nodePath); nodeErr == nil && !nodeInfo.IsDir() {
				return nodePath, cliPath
			}
		}
	}

	claudeEntry := detectWindowsShellPath([]string{"claude.cmd", "claude.ps1", "claude"})
	if claudeEntry == "" {
		return "", ""
	}
	baseDir := filepath.Dir(claudeEntry)
	cliPath := filepath.Join(baseDir, "node_modules", "@anthropic-ai", "claude-code", "cli.js")
	if info, err := os.Stat(cliPath); err != nil || info.IsDir() {
		return "", ""
	}
	nodePath := filepath.Join(baseDir, "node.exe")
	if info, err := os.Stat(nodePath); err != nil || info.IsDir() {
		nodePath = detectWindowsShellPath([]string{"node.exe", "node"})
	}
	if nodePath == "" {
		return "", ""
	}
	return nodePath, cliPath
}

func detectWindowsShellPath(candidates []string) string {
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		if strings.Contains(candidate, `\`) || strings.Contains(candidate, `/`) || filepath.IsAbs(candidate) {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
			continue
		}
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved
		}
	}
	return ""
}
