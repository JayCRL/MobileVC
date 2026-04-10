package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	adbpkg "mobilevc/internal/adb"
	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
)

type Snapshot struct {
	Running                   bool
	CanAcceptInteractiveInput bool
	ActiveMeta                protocol.RuntimeMeta
	ActiveSession             string
	ResumeSessionID           string
	TemporaryElevated         bool
	SafePermissionMode        string
	ClaudeLifecycle           string
}

var runtimeInfoQueries = map[string]string{
	"help":          "命令帮助",
	"model":         "模型信息",
	"claude_models": "Claude 模型目录",
	"codex_models":  "Codex 模型目录",
	"cost":          "成本信息",
	"context":       "运行上下文",
	"doctor":        "环境诊断",
}

var fetchCodexModelCatalog = runner.FetchCodexModelCatalog
var fetchClaudeModelsFromAPI = fetchModelsFromAPI
var fetchClaudeModelsFromNativeCLI = fetchModelsFromNativeCLI

func BuildRuntimeInfoResult(sessionID, query, cwd string, svc *Service) (protocol.RuntimeInfoResultEvent, error) {
	key := strings.TrimSpace(strings.ToLower(query))
	if key == "" {
		key = "context"
	}
	title, ok := runtimeInfoQueries[key]
	if !ok {
		return protocol.RuntimeInfoResultEvent{}, fmt.Errorf("unsupported runtime_info query: %s", query)
	}

	snapshot := Snapshot{}
	if svc != nil {
		snapshot = svc.RuntimeSnapshot()
	}

	switch key {
	case "help":
		return protocol.NewRuntimeInfoResultEvent(sessionID, key, title, "当前支持的 runtime info 查询与 slash command 概览。", false, []protocol.RuntimeInfoItem{
			{Label: "help", Value: "列出 runtime_info 查询能力", Available: true, Status: "ready"},
			{Label: "model", Value: "查看当前模型识别状态", Available: true, Status: "ready"},
			{Label: "claude_models", Value: "查看 Claude 可用模型目录（从 settings.json 与 API）", Available: true, Status: "ready"},
			{Label: "codex_models", Value: "查看 Codex 原生模型与推理强度目录", Available: true, Status: "ready"},
			{Label: "cost", Value: "查看成本遥测接入状态", Available: true, Status: "ready"},
			{Label: "context", Value: "查看当前 cwd / 会话 / 运行状态", Available: true, Status: "ready"},
			{Label: "doctor", Value: "查看环境与连接诊断", Available: true, Status: "ready"},
			{Label: "slash_commands", Value: "/help /clear /exit /quit /model /cost /context /compact /init /memory /add-dir /review /run /build /test /analyze /git status /git diff /git commit /git push /git pull /pr create /plan /execute /diff /doctor /fast", Available: true, Status: "ready", Detail: "slash_command action 已支持后端解析与分发。"},
		}), nil
	case "claude_models":
		items, err := fetchClaudeModelCatalog()
		if err != nil {
			return protocol.NewRuntimeInfoResultEvent(sessionID, key, title, fmt.Sprintf("Claude 模型目录拉取失败：%v", err), true, []protocol.RuntimeInfoItem{{
				Label:     "claude_model_catalog",
				Value:     "unavailable",
				Available: false,
				Status:    "missing",
				Detail:    err.Error(),
			}}), nil
		}
		return protocol.NewRuntimeInfoResultEvent(
			sessionID,
			key,
			title,
			fmt.Sprintf("已同步 %d 个 Claude 模型，可用于 Flutter 侧动态选择。", len(items)),
			false,
			items,
		), nil
	case "model":
		items := []protocol.RuntimeInfoItem{{
			Label:     "active_ai",
			Value:     detectModelValue(snapshot.ActiveMeta),
			Available: true,
			Status:    "limited",
			Detail:    "当前项目尚未从 Claude / Gemini 流中稳定提取精确模型名，此处仅展示已知 AI CLI 上下文。",
		}}
		return protocol.NewRuntimeInfoResultEvent(sessionID, key, title, "模型信息为有限可见状态。", false, items), nil
	case "codex_models":
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()

		entries, err := fetchCodexModelCatalog(ctx, codexCatalogCommand(snapshot), cwd)
		if err != nil {
			return protocol.NewRuntimeInfoResultEvent(sessionID, key, title, fmt.Sprintf("Codex 原生模型目录拉取失败：%v", err), true, []protocol.RuntimeInfoItem{{
				Label:     "codex_model_catalog",
				Value:     "unavailable",
				Available: false,
				Status:    "missing",
				Detail:    err.Error(),
			}}), nil
		}

		items := make([]protocol.RuntimeInfoItem, 0, len(entries))
		for _, entry := range entries {
			if strings.TrimSpace(entry.Model) == "" {
				continue
			}
			items = append(items, protocol.RuntimeInfoItem{
				Label:     entry.Model,
				Value:     fallbackValue(strings.TrimSpace(entry.DisplayName), entry.Model),
				Available: true,
				Status:    ternary(entry.IsDefault, "default", "ready"),
				Detail:    strings.TrimSpace(entry.Description),
				Meta:      entry,
			})
		}
		return protocol.NewRuntimeInfoResultEvent(
			sessionID,
			key,
			title,
			fmt.Sprintf("已同步 %d 个 Codex 原生模型，可用于 Flutter 侧动态选择。", len(items)),
			false,
			items,
		), nil
	case "cost":
		items := []protocol.RuntimeInfoItem{{
			Label:     "telemetry",
			Value:     "unavailable",
			Available: true,
			Status:    "limited",
			Detail:    "前端已有 cost 展示占位，但后端暂未接入真实 cost telemetry。",
		}}
		return protocol.NewRuntimeInfoResultEvent(sessionID, key, title, "成本统计暂未接入真实数据源。", false, items), nil
	case "context":
		resolvedCWD := strings.TrimSpace(cwd)
		if resolvedCWD == "" {
			resolvedCWD = "."
		}
		items := []protocol.RuntimeInfoItem{
			{Label: "cwd", Value: resolvedCWD, Available: true, Status: availabilityStatus(pathExists(resolvedCWD)), Detail: cwdDetail(resolvedCWD)},
			{Label: "runner", Value: ternary(snapshot.Running, "running", "idle"), Available: true, Status: ternary(snapshot.Running, "active", "ready")},
			{Label: "active_session", Value: fallbackValue(snapshot.ActiveSession, "(none)"), Available: true, Status: availabilityStatus(snapshot.ActiveSession != "")},
			{Label: "source", Value: fallbackValue(snapshot.ActiveMeta.Source, "command"), Available: true, Status: "ready"},
			{Label: "skill", Value: fallbackValue(snapshot.ActiveMeta.SkillName, "none"), Available: true, Status: availabilityStatus(snapshot.ActiveMeta.SkillName != "")},
			{Label: "target_path", Value: fallbackValue(snapshot.ActiveMeta.TargetPath, "(none)"), Available: true, Status: availabilityStatus(snapshot.ActiveMeta.TargetPath != "")},
			{Label: "resume_session", Value: fallbackValue(firstNonEmpty(snapshot.ActiveMeta.ResumeSessionID, snapshot.ResumeSessionID), "(none)"), Available: true, Status: availabilityStatus(firstNonEmpty(snapshot.ActiveMeta.ResumeSessionID, snapshot.ResumeSessionID) != "")},
			{Label: "temporary_elevated", Value: ternary(snapshot.TemporaryElevated, "true", "false"), Available: true, Status: ternary(snapshot.TemporaryElevated, "active", "ready")},
			{Label: "safe_permission_mode", Value: fallbackValue(snapshot.SafePermissionMode, "(none)"), Available: true, Status: availabilityStatus(snapshot.SafePermissionMode != "")},
			{Label: "context", Value: fallbackValue(snapshot.ActiveMeta.ContextTitle, "(none)"), Available: true, Status: availabilityStatus(snapshot.ActiveMeta.ContextTitle != "")},
		}
		return protocol.NewRuntimeInfoResultEvent(sessionID, key, title, "当前运行上下文快照。", false, items), nil
	case "doctor":
		resolvedCWD := strings.TrimSpace(cwd)
		if resolvedCWD == "" {
			resolvedCWD = "."
		}
		claudePath, claudeErr := exec.LookPath("claude")
		codexPath, codexErr := exec.LookPath("codex")
		ghPath, ghErr := exec.LookPath("gh")
		adbStatus := adbpkg.DetectStatus(context.Background())
		adbPath := adbStatus.ADBPath
		if adbPath == "" {
			adbPath = "not found"
		}
		emulatorPath := adbStatus.EmulatorPath
		if emulatorPath == "" {
			emulatorPath = "not found"
		}
		items := []protocol.RuntimeInfoItem{
			{Label: "cwd_exists", Value: resolvedCWD, Available: pathExists(resolvedCWD), Status: availabilityStatus(pathExists(resolvedCWD)), Detail: cwdDetail(resolvedCWD)},
			{Label: "claude_cli", Value: fallbackValue(claudePath, "not found"), Available: claudeErr == nil, Status: availabilityStatus(claudeErr == nil), Detail: doctorDetail(claudeErr)},
			{Label: "codex_cli", Value: fallbackValue(codexPath, "not found"), Available: codexErr == nil, Status: availabilityStatus(codexErr == nil), Detail: doctorDetail(codexErr)},
			{Label: "adb_cli", Value: adbPath, Available: adbStatus.ADBAvailable, Status: availabilityStatus(adbStatus.ADBAvailable), Detail: adbStatus.Message},
			{Label: "emulator_cli", Value: emulatorPath, Available: adbStatus.EmulatorAvailable, Status: availabilityStatus(adbStatus.EmulatorAvailable), Detail: adbStatus.Message},
			{Label: "gh_cli", Value: fallbackValue(ghPath, "not found"), Available: ghErr == nil, Status: availabilityStatus(ghErr == nil), Detail: doctorDetail(ghErr)},
			{Label: "ws_session", Value: fallbackValue(sessionID, "(none)"), Available: sessionID != "", Status: availabilityStatus(sessionID != "")},
			{Label: "active_runner", Value: ternary(snapshot.Running, "running", "idle"), Available: true, Status: ternary(snapshot.Running, "active", "ready")},
		}
		return protocol.NewRuntimeInfoResultEvent(sessionID, key, title, "环境诊断仅做只读检查，不会启动 runner。", false, items), nil
	default:
		return protocol.RuntimeInfoResultEvent{}, fmt.Errorf("unsupported runtime_info query: %s", query)
	}
}

func codexCatalogCommand(snapshot Snapshot) string {
	command := strings.TrimSpace(snapshot.ActiveMeta.Command)
	if command == "" {
		return "codex"
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "codex"
	}
	head := strings.ToLower(strings.TrimSpace(fields[0]))
	if head == "codex" || strings.HasSuffix(head, "/codex") || strings.HasSuffix(head, `\\codex`) || head == "codex.exe" || head == "codex.cmd" || head == "codex.ps1" {
		return command
	}
	return "codex"
}

func detectModelValue(meta protocol.RuntimeMeta) string {
	if strings.TrimSpace(meta.Model) != "" {
		if strings.TrimSpace(meta.ReasoningEffort) != "" {
			return strings.TrimSpace(meta.Model) + " · " + strings.TrimSpace(strings.ToUpper(meta.ReasoningEffort))
		}
		return strings.TrimSpace(meta.Model)
	}
	commandHead := ""
	if fields := strings.Fields(strings.TrimSpace(meta.Command)); len(fields) > 0 {
		commandHead = strings.ToLower(fields[0])
	}
	if commandHead == "codex" || strings.HasSuffix(commandHead, "/codex") || strings.HasSuffix(commandHead, `\\codex`) || commandHead == "codex.exe" {
		return "codex"
	}
	if strings.TrimSpace(meta.Engine) == "codex" {
		return "codex"
	}
	if strings.TrimSpace(meta.Source) == "skill-center" {
		return "claude (via skill-center)"
	}
	if strings.TrimSpace(meta.ResumeSessionID) != "" {
		return "claude (resumed session)"
	}
	return "unknown"
}

func doctorDetail(err error) string {
	if err == nil {
		return "available"
	}
	return err.Error()
}

func cwdDetail(cwd string) string {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return err.Error()
	}
	return abs
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func availabilityStatus(ok bool) string {
	if ok {
		return "ready"
	}
	return "missing"
}

func fallbackValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func ternary[T any](cond bool, yes, no T) T {
	if cond {
		return yes
	}
	return no
}

type claudeSettings struct {
	Model                string            `json:"model"`
	ModelReasoningEffort string            `json:"model_reasoning_effort"`
	Env                  map[string]string `json:"env"`
}

type anthropicModelsResponse struct {
	Data []struct {
		ID     string `json:"id"`
		Object string `json:"object"`
	} `json:"data"`
}

func fetchClaudeModelCatalog() ([]protocol.RuntimeInfoItem, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}

	settingsPath := filepath.Join(homeDir, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("read settings.json: %w", err)
	}

	var settings claudeSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parse settings.json: %w", err)
	}

	return fetchClaudeModelCatalogWithSettings(homeDir, settings)
}

func fetchClaudeModelCatalogWithSettings(homeDir string, settings claudeSettings) ([]protocol.RuntimeInfoItem, error) {
	baseURL := settings.Env["ANTHROPIC_BASE_URL"]
	authToken := settings.Env["ANTHROPIC_AUTH_TOKEN"]

	// 优先尝试从 API 获取模型列表
	if baseURL != "" && authToken != "" {
		if items, err := fetchClaudeModelsFromAPI(baseURL, authToken, settings.Model); err == nil && len(items) > 0 {
			return items, nil
		}
	}

	// API 失败时回退到 Claude 原生 /model
	if items, err := fetchClaudeModelsFromNativeCLI(cwdOrHome(homeDir), settings.Model); err == nil && len(items) > 0 {
		return items, nil
	}

	// 最后回退到 settings.json 配置的模型
	return []protocol.RuntimeInfoItem{{
		Label:     settings.Model,
		Value:     settings.Model,
		Available: true,
		Status:    "default",
		Detail:    "当前配置模型（从 settings.json）",
	}}, nil
}

func fetchModelsFromAPI(baseURL, authToken, currentModel string) ([]protocol.RuntimeInfoItem, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	modelsURL := strings.TrimRight(baseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, "GET", modelsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+authToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned %d", resp.StatusCode)
	}

	var modelsResp anthropicModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, err
	}

	items := make([]protocol.RuntimeInfoItem, 0, len(modelsResp.Data))
	for _, model := range modelsResp.Data {
		if !strings.Contains(strings.ToLower(model.ID), "claude") {
			continue
		}
		isDefault := model.ID == currentModel
		items = append(items, protocol.RuntimeInfoItem{
			Label:     model.ID,
			Value:     model.ID,
			Available: true,
			Status:    ternary(isDefault, "default", "ready"),
			Detail:    ternary(isDefault, "当前配置模型", "可用模型"),
		})
	}

	return items, nil
}

func fetchModelsFromNativeCLI(cwd, currentModel string) ([]protocol.RuntimeInfoItem, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "/model")
	if strings.TrimSpace(cwd) != "" {
		cmd.Dir = cwd
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("run claude /model: %w", err)
	}
	return parseClaudeModelCLIOutput(string(output), currentModel)
}

func parseClaudeModelCLIOutput(output, currentModel string) ([]protocol.RuntimeInfoItem, error) {
	lines := strings.Split(output, "\n")
	items := make([]protocol.RuntimeInfoItem, 0)
	seen := make(map[string]struct{})
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "*") {
			continue
		}
		line = strings.TrimSpace(strings.TrimLeft(line, "-*• "))
		if line == "" {
			continue
		}
		if idx := strings.Index(line, " ("); idx > 0 {
			line = strings.TrimSpace(line[:idx])
		}
		label := line
		key := strings.ToLower(label)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		isDefault := strings.EqualFold(label, currentModel)
		items = append(items, protocol.RuntimeInfoItem{
			Label:     label,
			Value:     label,
			Available: true,
			Status:    ternary(isDefault, "default", "ready"),
			Detail:    ternary(isDefault, "当前配置模型（来自 Claude /model）", "可用模型（来自 Claude /model）"),
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no models parsed from claude /model output")
	}
	return items, nil
}

func cwdOrHome(homeDir string) string {
	if strings.TrimSpace(homeDir) == "" {
		return "."
	}
	return homeDir
}
