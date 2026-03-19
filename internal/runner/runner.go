package runner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Mode string

const (
	ModeExec Mode = "exec"
	ModePTY  Mode = "pty"
)

var ErrInputNotSupported = errors.New("input not supported")

type ExecRequest struct {
	Command   string
	CWD       string
	SessionID string
	Mode      Mode
}

type EventSink func(event any)

type Runner interface {
	Run(ctx context.Context, req ExecRequest, sink EventSink) error
	Write(ctx context.Context, data []byte) error
	Close() error
}

type shellSpec struct {
	path    string
	args    []string
	gitBash string
	winpty  string
}

func newShellCommand(ctx context.Context, command string, mode Mode) *exec.Cmd {
	spec := getShellSpec()
	preparedCommand := prepareShellCommand(command, spec, mode)
	cmd := exec.CommandContext(ctx, spec.path, append(spec.args, preparedCommand)...)
	cmd.Env = shellEnvironment(spec)
	return cmd
}

func prepareShellCommand(command string, spec shellSpec, mode Mode) string {
	if runtime.GOOS == "windows" && mode == ModePTY && spec.gitBash != "" && spec.winpty != "" && shouldWrapWithWinPTY(command) {
		return shellEscapeForBash(spec.winpty) + " -Xallow-non-tty " + shellEscapeForBash(spec.gitBash) + " -lc " + shellEscapeForBash(command)
	}
	return command
}

func shouldWrapWithWinPTY(command string) bool {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return false
	}
	first := strings.Fields(trimmed)
	if len(first) == 0 {
		return false
	}
	head := strings.ToLower(first[0])
	return head == "claude" || strings.HasSuffix(head, "/claude") || strings.HasSuffix(head, `\claude`) || head == "claude.exe"
}

func shellEscapeForBash(path string) string {
	path = strings.ReplaceAll(path, `\`, `/`)
	path = strings.ReplaceAll(path, `'`, `'''"'"'''`)
	return `'` + path + `'`
}

func getShellSpec() shellSpec {
	if runtime.GOOS != "windows" {
		return shellSpec{path: "sh", args: []string{"-lc"}}
	}

	if bashPath := detectGitBashPath(); bashPath != "" {
		return shellSpec{
			path:    bashPath,
			args:    []string{"-lc"},
			gitBash: bashPath,
			winpty:  detectWinPTYPath(bashPath),
		}
	}

	if powershellPath := detectWindowsShellPath([]string{
		filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
		"powershell.exe",
	}); powershellPath != "" {
		return shellSpec{
			path: powershellPath,
			args: []string{"-NoLogo", "-NoProfile", "-Command"},
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

func shellEnvironment(spec shellSpec) []string {
	env := os.Environ()
	if runtime.GOOS == "windows" && spec.gitBash != "" {
		env = upsertEnv(env, "CLAUDE_CODE_GIT_BASH_PATH", spec.gitBash)
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
