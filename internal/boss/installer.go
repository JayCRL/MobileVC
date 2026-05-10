package boss

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mobilevc/internal/logx"
	"mobilevc/internal/protocol"
)

// InstallResult is returned by InstallClaude.
type InstallResult struct {
	Installed    bool   `json:"installed"`
	SettingsDone bool   `json:"settingsDone"`
	Version      string `json:"version,omitempty"`
	Error        string `json:"error,omitempty"`
}

// InstallClaude installs Claude Code CLI via npm and writes settings.json.
// Progress events are emitted to the given emit function.
func InstallClaude(ctx context.Context, apiKey, baseURL, sessionID string, emit func(any)) InstallResult {
	var result InstallResult

	// Step 1: Check npm
	emit(protocol.NewPlanningStateEvent(sessionID, "installing", "", "", "检查 Node.js 环境...", nil))
	logx.Info("boss", "install: checking npm")

	if _, err := exec.LookPath("npm"); err != nil {
		result.Error = "npm 未安装。请先安装 Node.js: https://nodejs.org"
		emit(protocol.NewPlanningStateEvent(sessionID, "failed", "", "", result.Error, nil))
		return result
	}
	if _, err := exec.LookPath("node"); err != nil {
		result.Error = "node 未安装。请先安装 Node.js: https://nodejs.org"
		emit(protocol.NewPlanningStateEvent(sessionID, "failed", "", "", result.Error, nil))
		return result
	}

	// Step 2: Install Claude Code CLI
	emit(protocol.NewPlanningStateEvent(sessionID, "installing", "", "", "正在安装 Claude Code CLI...", nil))
	logx.Info("boss", "install: running npm install -g @anthropic-ai/claude-code")

	cmd := exec.CommandContext(ctx, "npm", "install", "-g", "@anthropic-ai/claude-code")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		result.Error = fmt.Sprintf("安装失败: %v", err)
		emit(protocol.NewPlanningStateEvent(sessionID, "failed", "", "", result.Error, nil))
		return result
	}
	result.Installed = true

	// Step 3: Verify installation
	emit(protocol.NewPlanningStateEvent(sessionID, "installing", "", "", "验证安装...", nil))
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		for _, p := range []string{
			os.ExpandEnv("$HOME/.npm-global/bin/claude"),
			"/usr/local/bin/claude",
			"/opt/homebrew/bin/claude",
		} {
			if fi, statErr := os.Stat(p); statErr == nil && !fi.IsDir() {
				claudePath = p
				break
			}
		}
	}
	if claudePath == "" {
		result.Error = "安装后未找到 claude 命令，请检查 PATH"
		emit(protocol.NewPlanningStateEvent(sessionID, "failed", "", "", result.Error, nil))
		return result
	}

	// Step 4: Write settings.json
	emit(protocol.NewPlanningStateEvent(sessionID, "installing", "", "", "配置 settings.json...", nil))
	logx.Info("boss", "install: writing settings.json")

	settingsDir := filepath.Join(os.ExpandEnv("$HOME"), ".claude")
	if err := os.MkdirAll(settingsDir, 0700); err != nil {
		result.Error = fmt.Sprintf("创建 .claude 目录失败: %v", err)
		emit(protocol.NewPlanningStateEvent(sessionID, "failed", "", "", result.Error, nil))
		return result
	}

	settingsPath := filepath.Join(settingsDir, "settings.json")
	settings := map[string]interface{}{}
	if apiKey != "" {
		settings["apiKeyHelper"] = apiKey
	}
	if baseURL != "" {
		settings["baseURL"] = strings.TrimSpace(baseURL)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		result.Error = fmt.Sprintf("序列化 settings.json 失败: %v", err)
		emit(protocol.NewPlanningStateEvent(sessionID, "failed", "", "", result.Error, nil))
		return result
	}

	if err := os.WriteFile(settingsPath, append(data, '\n'), 0600); err != nil {
		result.Error = fmt.Sprintf("写入 settings.json 失败: %v", err)
		emit(protocol.NewPlanningStateEvent(sessionID, "failed", "", "", result.Error, nil))
		return result
	}
	result.SettingsDone = true

	emit(protocol.NewPlanningStateEvent(sessionID, "completed", "", "",
		fmt.Sprintf("安装完成！Claude Code CLI 已就绪，配置文件: %s", settingsPath), nil))
	logx.Info("boss", "install: complete path=%s settings=%s", claudePath, settingsPath)

	return result
}
