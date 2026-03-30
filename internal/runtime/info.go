package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mobilevc/internal/protocol"
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
	"help":    "命令帮助",
	"model":   "模型信息",
	"cost":    "成本信息",
	"context": "运行上下文",
	"doctor":  "环境诊断",
}

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
			{Label: "cost", Value: "查看成本遥测接入状态", Available: true, Status: "ready"},
			{Label: "context", Value: "查看当前 cwd / 会话 / 运行状态", Available: true, Status: "ready"},
			{Label: "doctor", Value: "查看环境与连接诊断", Available: true, Status: "ready"},
			{Label: "slash_commands", Value: "/help /clear /exit /quit /model /cost /context /compact /init /memory /add-dir /review /run /build /test /analyze /git status /git diff /git commit /git push /git pull /pr create /plan /execute /diff /doctor /fast", Available: true, Status: "ready", Detail: "slash_command action 已支持后端解析与分发。"},
		}), nil
	case "model":
		items := []protocol.RuntimeInfoItem{{
			Label:     "active_ai",
			Value:     detectModelValue(snapshot.ActiveMeta),
			Available: true,
			Status:    "limited",
			Detail:    "当前项目尚未从 Claude / Gemini 流中稳定提取精确模型名，此处仅展示已知 AI CLI 上下文。",
		}}
		return protocol.NewRuntimeInfoResultEvent(sessionID, key, title, "模型信息为有限可见状态。", false, items), nil
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
		items := []protocol.RuntimeInfoItem{
			{Label: "cwd_exists", Value: resolvedCWD, Available: pathExists(resolvedCWD), Status: availabilityStatus(pathExists(resolvedCWD)), Detail: cwdDetail(resolvedCWD)},
			{Label: "claude_cli", Value: fallbackValue(claudePath, "not found"), Available: claudeErr == nil, Status: availabilityStatus(claudeErr == nil), Detail: doctorDetail(claudeErr)},
			{Label: "codex_cli", Value: fallbackValue(codexPath, "not found"), Available: codexErr == nil, Status: availabilityStatus(codexErr == nil), Detail: doctorDetail(codexErr)},
			{Label: "gh_cli", Value: fallbackValue(ghPath, "not found"), Available: ghErr == nil, Status: availabilityStatus(ghErr == nil), Detail: doctorDetail(ghErr)},
			{Label: "ws_session", Value: fallbackValue(sessionID, "(none)"), Available: sessionID != "", Status: availabilityStatus(sessionID != "")},
			{Label: "active_runner", Value: ternary(snapshot.Running, "running", "idle"), Available: true, Status: ternary(snapshot.Running, "active", "ready")},
		}
		return protocol.NewRuntimeInfoResultEvent(sessionID, key, title, "环境诊断仅做只读检查，不会启动 runner。", false, items), nil
	default:
		return protocol.RuntimeInfoResultEvent{}, fmt.Errorf("unsupported runtime_info query: %s", query)
	}
}

func detectModelValue(meta protocol.RuntimeMeta) string {
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
