package ws

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
	runtimepkg "mobilevc/internal/runtime"
	"mobilevc/internal/store"
)

type slashCommandSpec struct {
	key          string
	category     string
	runtimeQuery string
	skillName    string
	execTemplate string
	requiresArgs bool
	confirmOnly  bool
	localOnly    bool
}

var slashCommandCatalog = []slashCommandSpec{
	{key: "/help", category: "runtime-info", runtimeQuery: "help"},
	{key: "/clear", category: "local", localOnly: true},
	{key: "/exit", category: "local", localOnly: true},
	{key: "/quit", category: "local", localOnly: true},
	{key: "/context", category: "runtime-info", runtimeQuery: "context"},
	{key: "/fast", category: "local", localOnly: true},
	{key: "/model", category: "runtime-info", runtimeQuery: "model"},
	{key: "/cost", category: "runtime-info", runtimeQuery: "cost"},
	{key: "/doctor", category: "runtime-info", runtimeQuery: "doctor"},
	{key: "/review", category: "skill", skillName: "review"},
	{key: "/analyze", category: "skill", skillName: "analyze"},
	{key: "/flutter-context", category: "skill", skillName: "flutter-context"},
	{key: "/diff", category: "local", localOnly: true},
	{key: "/init", category: "exec", execTemplate: "claude /init"},
	{key: "/memory", category: "runtime-info", runtimeQuery: "memory-panel"},
	{key: "/add-dir", category: "exec", execTemplate: "claude /add-dir %s", requiresArgs: true},
	{key: "/plan", category: "exec", execTemplate: "claude /plan"},
	{key: "/execute", category: "exec", execTemplate: "claude /execute"},
	{key: "/compact", category: "exec", execTemplate: "claude /compact"},
	{key: "/run", category: "exec", execTemplate: "%s", requiresArgs: true},
	{key: "/build", category: "exec", execTemplate: "go build ./..."},
	{key: "/test", category: "exec", execTemplate: "go test ./..."},
	{key: "/git status", category: "exec", execTemplate: "git status"},
	{key: "/git diff", category: "exec", execTemplate: "git diff"},
	{key: "/git commit", category: "exec", execTemplate: "git commit -m %s", requiresArgs: true, confirmOnly: true},
	{key: "/git push", category: "exec", execTemplate: "git push", confirmOnly: true},
	{key: "/git pull", category: "exec", execTemplate: "git pull"},
	{key: "/pr create", category: "exec", execTemplate: "gh pr create", confirmOnly: true},
}

type parsedSlashCommand struct {
	spec slashCommandSpec
	raw  string
	args string
}

func parseSlashCommand(raw string) (*parsedSlashCommand, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("slash command is required")
	}
	if !strings.HasPrefix(trimmed, "/") {
		return nil, fmt.Errorf("slash command must start with /")
	}
	for _, spec := range sortedSlashCatalog() {
		if trimmed == spec.key || strings.HasPrefix(trimmed, spec.key+" ") {
			return &parsedSlashCommand{spec: spec, raw: trimmed, args: strings.TrimSpace(strings.TrimPrefix(trimmed, spec.key))}, nil
		}
	}
	return nil, fmt.Errorf("unsupported slash command: %s", trimmed)
}

func sortedSlashCatalog() []slashCommandSpec {
	items := make([]slashCommandSpec, len(slashCommandCatalog))
	copy(items, slashCommandCatalog)
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if len(items[j].key) > len(items[i].key) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	return items
}

func handleSlashCommand(ctx context.Context, sessionID string, req protocol.SlashCommandRequestEvent, sessionContext store.SessionContext, runtimeSvc *runtimepkg.Service, launcher commandSkillLauncher, emit func(any)) error {
	parsed, err := parseSlashCommand(req.Command)
	if err != nil {
		return err
	}
	switch parsed.spec.category {
	case "local":
		return handleLocalSlashCommand(sessionID, parsed, req, emit)
	case "runtime-info":
		if parsed.spec.runtimeQuery == "memory-panel" {
			emit(protocol.NewRuntimeInfoResultEvent(sessionID, "memory-panel", "Memory 面板", "这是 MobileVC 内部记忆层，不是 Claude 隐式 /memory；请在前端 Memory 面板中管理。", false, []protocol.RuntimeInfoItem{{Label: "entry", Value: "Memory", Available: true, Status: "ui-panel", Detail: "在 Memory 面板中新增、编辑并勾选会话级记忆。"}}))
			return nil
		}
		result, err := runtimepkg.BuildRuntimeInfoResult(sessionID, parsed.spec.runtimeQuery, fallback(req.CWD, "."), runtimeSvc)
		if err != nil {
			return err
		}
		emit(result)
		return nil
	case "skill":
		if launcher == nil {
			return fmt.Errorf("skill launcher is unavailable")
		}
		skillReq, err := buildSkillRequestFromSlash(parsed, req)
		if err != nil {
			return err
		}
		return executeSkillRequest(ctx, sessionID, skillReq, sessionContext, runtimeSvc, launcher, emit)
	case "exec":
		execReq, err := buildExecRequestFromSlash(parsed, req)
		if err != nil {
			return err
		}
		return runtimeSvc.Execute(ctx, sessionID, execReq, emit)
	default:
		return fmt.Errorf("unsupported slash command category: %s", parsed.spec.category)
	}
}

func handleLocalSlashCommand(sessionID string, parsed *parsedSlashCommand, req protocol.SlashCommandRequestEvent, emit func(any)) error {
	switch parsed.spec.key {
	case "/clear", "/exit", "/quit", "/fast":
		result := protocol.NewRuntimeInfoResultEvent(sessionID, strings.TrimPrefix(parsed.spec.key, "/"), "前端本地命令", "该命令应由前端本地处理，后端不会代替执行破坏性或 UI 行为。", false, []protocol.RuntimeInfoItem{{
			Label:     "command",
			Value:     parsed.spec.key,
			Available: true,
			Status:    "local-only",
			Detail:    "请由前端自行清屏、断开 WebSocket 或切换 fast 标记。",
		}})
		emit(result)
		return nil
	case "/diff":
		if strings.TrimSpace(req.TargetDiff) == "" {
			return fmt.Errorf("/diff requires targetDiff context")
		}
		event := protocol.NewFileDiffEvent(sessionID, req.TargetPath, fallback(req.TargetTitle, "最近 Diff"), req.TargetDiff, guessLangFromPath(req.TargetPath))
		event.RuntimeMeta = protocol.RuntimeMeta{
			Source:       "slash-command",
			TargetType:   "diff",
			TargetPath:   req.TargetPath,
			ContextID:    req.ContextID,
			ContextTitle: fallback(req.ContextTitle, req.TargetTitle),
		}
		emit(event)
		return nil
	default:
		return fmt.Errorf("unsupported local slash command: %s", parsed.spec.key)
	}
}

func buildExecRequestFromSlash(parsed *parsedSlashCommand, req protocol.SlashCommandRequestEvent) (runtimepkg.ExecuteRequest, error) {
	args := strings.TrimSpace(parsed.args)
	if parsed.spec.requiresArgs && args == "" {
		return runtimepkg.ExecuteRequest{}, fmt.Errorf("%s requires arguments", parsed.spec.key)
	}
	command := parsed.spec.execTemplate
	switch parsed.spec.key {
	case "/run":
		command = args
	case "/test":
		command = "go test ./..."
	case "/git commit":
		quoted := args
		if !isQuotedArgument(quoted) {
			quoted = strconv.Quote(args)
		}
		command = fmt.Sprintf(parsed.spec.execTemplate, quoted)
	case "/add-dir":
		command = fmt.Sprintf(parsed.spec.execTemplate, args)
	default:
		if strings.Contains(parsed.spec.execTemplate, "%s") {
			command = fmt.Sprintf(parsed.spec.execTemplate, args)
		}
	}
	return runtimepkg.ExecuteRequest{
		Command:        command,
		CWD:            fallback(req.CWD, "."),
		Mode:           runner.ModePTY,
		PermissionMode: req.PermissionMode,
		RuntimeMeta: protocol.RuntimeMeta{
			Source:     "slash-command",
			Target:     parsed.raw,
			TargetType: "slash-command",
			TargetText: parsed.raw,
		},
	}, nil
}

func buildSkillRequestFromSlash(parsed *parsedSlashCommand, req protocol.SlashCommandRequestEvent) (protocol.SkillRequestEvent, error) {
	if strings.TrimSpace(req.TargetType) == "" {
		return protocol.SkillRequestEvent{}, fmt.Errorf("%s requires targetType context", parsed.spec.key)
	}
	return protocol.SkillRequestEvent{
		ClientEvent:  protocol.ClientEvent{Action: "skill_exec"},
		Name:         parsed.spec.skillName,
		Engine:       req.Engine,
		CWD:          fallback(req.CWD, "."),
		TargetType:   req.TargetType,
		TargetPath:   req.TargetPath,
		TargetDiff:   req.TargetDiff,
		TargetTitle:  req.TargetTitle,
		ContextID:    req.ContextID,
		ContextTitle: req.ContextTitle,
		TargetText:   req.TargetText,
		TargetStack:  req.TargetStack,
	}, nil
}

type commandSkillLauncher interface {
	BuildRequest(name, engine, cwd, targetType, targetPath, targetTitle, targetDiff, contextID, contextTitle, targetText, targetStack string, sessionContext store.SessionContext) (runtimepkg.ExecuteRequest, error)
	ExtractPrompt(command string) string
}

func executeSkillRequest(ctx context.Context, sessionID string, skillEvent protocol.SkillRequestEvent, sessionContext store.SessionContext, runtimeSvc *runtimepkg.Service, launcher commandSkillLauncher, emit func(any)) error {
	execReq, err := launcher.BuildRequest(skillEvent.Name, skillEvent.Engine, fallback(skillEvent.CWD, "."), skillEvent.TargetType, skillEvent.TargetPath, skillEvent.TargetTitle, skillEvent.TargetDiff, skillEvent.ContextID, skillEvent.ContextTitle, skillEvent.TargetText, skillEvent.TargetStack, sessionContext)
	if err != nil {
		return err
	}
	prompt := launcher.ExtractPrompt(execReq.Command)
	if prompt == "" {
		return fmt.Errorf("无法执行 skill：prompt 为空")
	}
	inputReq := runtimepkg.InputRequest{
		Data: prompt + "\n",
		RuntimeMeta: protocol.MergeRuntimeMeta(execReq.RuntimeMeta, protocol.RuntimeMeta{
			Source: "skill-center",
		}),
	}
	resumeReq := execReq
	resumeReq.Mode = runner.ModePTY
	resumeReq.Command = "claude"
	resumeReq.RuntimeMeta = protocol.MergeRuntimeMeta(execReq.RuntimeMeta, protocol.RuntimeMeta{
		Command:        "claude",
		PermissionMode: execReq.PermissionMode,
	})
	if err := runtimeSvc.SendInputOrResume(ctx, sessionID, resumeReq, inputReq, emit); err == nil {
		return nil
	} else if !runtimeSvc.IsRunning() {
		return runtimeSvc.Execute(ctx, sessionID, execReq, emit)
	} else {
		return err
	}
}

func guessLangFromPath(path string) string {
	if strings.HasSuffix(path, ".go") {
		return "go"
	}
	if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".ts") {
		return "javascript"
	}
	if strings.HasSuffix(path, ".py") {
		return "python"
	}
	return "text"
}

func isQuotedArgument(value string) bool {
	if len(value) < 2 {
		return false
	}
	return (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) || (strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'"))
}
