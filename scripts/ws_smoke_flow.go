package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"mobilevc/internal/protocol"
)

const defaultSmokeTimeout = 8 * time.Minute

type smokeScenario string

const (
	scenarioFull                 smokeScenario = "full"
	scenarioPermissionDiffReview smokeScenario = "permission-diff-review"
	scenarioTerminalLogs         smokeScenario = "terminal-logs"
	scenarioCodexBasic           smokeScenario = "codex-basic"
	scenarioCodexReadmeWrite     smokeScenario = "codex-readme-write"
)

func main() {
	var (
		baseURL   = flag.String("url", "", "websocket url")
		timeout   = flag.Duration("timeout", defaultSmokeTimeout, "overall smoke timeout")
		scenario  = flag.String("scenario", string(scenarioFull), "smoke scenario: full, permission-diff-review, terminal-logs, codex-basic, or codex-readme-write")
		aiCommand = flag.String("ai-command", strings.TrimSpace(os.Getenv("SMOKE_AI_COMMAND")), "AI command for codex-basic scenario")
		engine    = flag.String("engine", strings.TrimSpace(os.Getenv("SMOKE_ENGINE")), "engine for codex-basic scenario")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	wsURL := *baseURL
	if strings.TrimSpace(wsURL) == "" {
		wsURL = buildWSURL()
	}

	if err := runSmoke(
		ctx,
		wsURL,
		smokeScenario(strings.TrimSpace(*scenario)),
		strings.TrimSpace(*aiCommand),
		strings.TrimSpace(*engine),
		os.Stdout,
	); err != nil {
		fmt.Fprintf(os.Stderr, "smoke failed: %v\n", err)
		os.Exit(1)
	}
}

func buildWSURL() string {
	host := strings.TrimSpace(os.Getenv("HOST"))
	if host == "" {
		host = "127.0.0.1"
	}
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8001"
	}
	token := strings.TrimSpace(os.Getenv("AUTH_TOKEN"))
	if token == "" {
		token = "test"
	}
	return fmt.Sprintf("ws://%s:%s/ws?token=%s", host, port, url.QueryEscape(token))
}

type smokeRunner struct {
	conn       *websocket.Conn
	transcript *transcript
	ctx        context.Context
	aiCommand  string
	engine     string
}

func runSmoke(ctx context.Context, wsURL string, scenario smokeScenario, aiCommand string, engine string, out *os.File) error {
	tr := newTranscript(out)
	tr.section("connect")

	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, resp, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("dial websocket: %w (status=%s)", err, resp.Status)
		}
		return fmt.Errorf("dial websocket: %w", err)
	}
	defer conn.Close()

	runner := &smokeRunner{
		conn:       conn,
		transcript: tr,
		ctx:        ctx,
		aiCommand:  aiCommand,
		engine:     engine,
	}
	conn.SetReadLimit(8 << 20)

	if err := runner.bootstrap(); err != nil {
		return err
	}
	if err := runner.selectSession(); err != nil {
		return err
	}
	if scenario == scenarioCodexBasic {
		if err := runner.codexBasicFlow(); err != nil {
			return err
		}
		tr.section("done")
		tr.line("done scenario=%s", scenarioCodexBasic)
		return nil
	}
	if scenario == scenarioCodexReadmeWrite {
		if err := runner.codexReadmeWriteFlow(); err != nil {
			return err
		}
		tr.section("done")
		tr.line("done scenario=%s", scenarioCodexReadmeWrite)
		return nil
	}
	if err := runner.chatFlow(); err != nil {
		return err
	}
	fileDiff, err := runner.planFlow()
	if err != nil {
		return err
	}
	if err := runner.reviewFlow(fileDiff); err != nil {
		return err
	}
	if err := runner.assertHistoryReviewState(); err != nil {
		return err
	}

	switch scenario {
	case "", scenarioFull:
		if err := runner.catalogFlow(); err != nil {
			return err
		}
		if err := runner.finalize(); err != nil {
			return err
		}
	case scenarioTerminalLogs:
		if err := runner.terminalLogFlow(); err != nil {
			return err
		}
		tr.section("done")
		tr.line("done scenario=%s", scenarioTerminalLogs)
		return nil
	case scenarioPermissionDiffReview:
		tr.section("done")
		tr.line("done scenario=%s", scenarioPermissionDiffReview)
		return nil
	default:
		return fmt.Errorf("unknown smoke scenario %q", scenario)
	}

	tr.section("done")
	tr.line("done scenario=%s", scenarioFull)
	return nil
}

func (r *smokeRunner) bootstrap() error {
	r.transcript.section("bootstrap")
	if _, err := r.waitForType(protocol.EventTypeSessionState, 15*time.Second, nil); err != nil {
		return err
	}
	if _, err := r.waitForType(protocol.EventTypeAgentState, 15*time.Second, nil); err != nil {
		return err
	}
	if _, err := r.waitForType(protocol.EventTypeSkillCatalogResult, 15*time.Second, nil); err != nil {
		return err
	}
	if _, err := r.waitForType(protocol.EventTypeMemoryListResult, 15*time.Second, nil); err != nil {
		return err
	}
	if _, err := r.waitForType(protocol.EventTypeSessionListResult, 15*time.Second, nil); err != nil {
		return err
	}

	sessionTitle := fmt.Sprintf("ws-smoke-%d", time.Now().Unix())
	if err := r.send(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: sessionTitle}); err != nil {
		return err
	}
	created, err := r.waitForType(protocol.EventTypeSessionCreated, 15*time.Second, nil)
	if err != nil {
		return err
	}
	r.transcript.sessionID = created.nestedString("summary", "id")
	if r.transcript.sessionID == "" {
		return errors.New("session_create did not return a session id")
	}
	if _, err := r.waitForType(protocol.EventTypeSessionState, 15*time.Second, nil); err != nil {
		return err
	}
	if _, err := r.waitForType(protocol.EventTypeSessionListResult, 15*time.Second, nil); err != nil {
		return err
	}
	return nil
}

func (r *smokeRunner) selectSession() error {
	if strings.TrimSpace(r.transcript.sessionID) == "" {
		return errors.New("session id unavailable before exec")
	}
	if err := r.send(protocol.SessionLoadRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_load"}, SessionID: r.transcript.sessionID}); err != nil {
		return err
	}
	if _, err := r.waitForType(protocol.EventTypeSessionHistory, 30*time.Second, nil); err != nil {
		return err
	}
	if _, err := r.waitForType(protocol.EventTypeReviewState, 15*time.Second, nil); err != nil {
		return err
	}
	if _, err := r.waitForType(protocol.EventTypeSessionState, 15*time.Second, nil); err != nil {
		return err
	}
	if err := r.send(protocol.SessionContextUpdateRequestEvent{
		ClientEvent:       protocol.ClientEvent{Action: "session_context_update"},
		EnabledSkillNames: []string{"review"},
	}); err != nil {
		return err
	}
	if _, err := r.waitForType(protocol.EventTypeSessionContextResult, 15*time.Second, nil); err != nil {
		return err
	}
	return nil
}

func (r *smokeRunner) chatFlow() error {
	r.transcript.section("chat")
	if err := r.send(protocol.ExecRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "exec"},
		Command:        "claude",
		Mode:           "pty",
		PermissionMode: "default",
	}); err != nil {
		return err
	}

	if _, err := r.waitForType(protocol.EventTypeAgentState, 30*time.Second, nil); err != nil {
		return err
	}
	if _, err := r.waitForType(protocol.EventTypeSessionState, 30*time.Second, nil); err != nil {
		return err
	}
	if err := r.send(protocol.InputRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "input"},
		Data:        "Reply with exactly READY and nothing else.\n",
	}); err != nil {
		return err
	}
	if _, err := r.waitForAnyType(120*time.Second, []string{protocol.EventTypeLog, protocol.EventTypeAgentState, protocol.EventTypeProgress, protocol.EventTypePromptRequest}, func(evt eventMap) bool {
		switch evt.stringField("type") {
		case protocol.EventTypeLog:
			return strings.Contains(strings.ToUpper(evt.stringField("msg")), "READY")
		case protocol.EventTypeAgentState:
			return evt.boolField("awaitInput") || strings.EqualFold(evt.stringField("state"), "IDLE")
		case protocol.EventTypeProgress:
			return true
		case protocol.EventTypePromptRequest:
			return true
		default:
			return false
		}
	}); err != nil {
		return err
	}
	return nil
}

func (r *smokeRunner) reviewFlow(fileDiff eventMap) error {
	r.transcript.section("review")
	if fileDiff == nil || strings.TrimSpace(fileDiff.stringField("path")+fileDiff.stringField("contextId")) == "" {
		r.transcript.line("review_skipped no_real_diff_available")
		return nil
	}

	targetGroupID := firstNonEmpty(
		fileDiff.stringField("groupId"),
		fileDiff.stringField("executionId"),
		fileDiff.stringField("contextId"),
		fileDiff.stringField("path"),
	)

	// Wait for Claude to be in an interactive state before sending review decision
	if err := r.waitForReviewReady(targetGroupID); err != nil {
		r.transcript.line("review_skipped wait_ready_failed err=%v", err)
		return nil
	}

	if err := r.send(protocol.ReviewDecisionRequestEvent{
		ClientEvent:  protocol.ClientEvent{Action: "review_decision"},
		Decision:     "accept",
		ExecutionID:  fileDiff.stringField("executionId"),
		GroupID:      targetGroupID,
		GroupTitle:   firstNonEmpty(fileDiff.stringField("groupTitle"), fileDiff.stringField("title")),
		ContextID:    firstNonEmpty(fileDiff.stringField("contextId"), fileDiff.stringField("path")),
		ContextTitle: firstNonEmpty(fileDiff.stringField("contextTitle"), fileDiff.stringField("title")),
		TargetPath:   fileDiff.stringField("path"),
	}); err != nil {
		return err
	}

	// Backend sends review_state after successful review_decision (handler.go:875-877)
	reviewState, err := r.waitForType(protocol.EventTypeReviewState, 30*time.Second, nil)
	if err != nil {
		return err
	}
	r.transcript.line("review_state groups=%d", reviewState.arrayLen("groups"))

	// Verify the group was accepted
	if !reviewStateHasAcceptedGroup(reviewState, targetGroupID) {
		r.transcript.line("review_warn group=%q not_yet_accepted (may be partial)", targetGroupID)
	} else {
		r.transcript.line("review_accepted group=%q", targetGroupID)
	}
	return nil
}

func (r *smokeRunner) planFlow() (eventMap, error) {
	r.transcript.section("plan")
	if _, err := r.waitForType(protocol.EventTypeAgentState, 180*time.Second, func(evt eventMap) bool {
		return strings.EqualFold(evt.stringField("state"), "IDLE") || strings.Contains(evt.stringField("msg"), "可继续对话")
	}); err != nil {
		return nil, err
	}

	prompt := "任务：在 README.md 末尾追加一行 'smoke test passed'。先不要执行，请用 EnterPlanMode 工具给出计划并等待我确认。\n"
	if err := r.send(protocol.InputRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "input"},
		Data:        prompt,
	}); err != nil {
		return nil, err
	}

	var planInteraction eventMap
	for attempts := 0; attempts < 3; attempts++ {
		var err error
		planInteraction, err = r.waitForPlanInteraction(180 * time.Second)
		if err != nil {
			if strings.Contains(err.Error(), "claude_went_idle_without_plan") || strings.Contains(err.Error(), "tool_truncation_or_error") {
				r.transcript.line("plan_retry attempt=%d reason=%q", attempts+1, err.Error())
				if err := r.send(protocol.InputRequestEvent{
					ClientEvent: protocol.ClientEvent{Action: "input"},
					Data:        "刚才你的 EnterPlanMode 工具调用似乎失败或被截断了，导致你并没有真正进入计划模式。请重新调用一次 EnterPlanMode 给出计划。\n",
				}); err != nil {
					return nil, err
				}
				continue
			}
			return nil, err
		}
		break // Success
	}

	if planInteraction == nil {
		return nil, fmt.Errorf("failed to enter plan mode after multiple retries")
	}

	decisionPayload, answerLabel, err := buildPlanDecisionPayload(r.transcript.sessionID, planInteraction)
	if err != nil {
		return nil, err
	}
	r.transcript.line("plan_prompt msg=%q kind=%q", planInteraction.stringField("msg"), planInteraction.stringField("kind"))
	r.transcript.line("plan_decision payload=%s", decisionPayload)

	if err := r.send(protocol.PlanDecisionRequestEvent{
		ClientEvent:     protocol.ClientEvent{Action: "plan_decision"},
		Decision:        decisionPayload,
		SessionID:       r.transcript.sessionID,
		ExecutionID:     planInteraction.stringField("executionId"),
		GroupID:         planInteraction.stringField("groupId"),
		GroupTitle:      planInteraction.stringField("groupTitle"),
		ContextID:       planInteraction.stringField("contextId"),
		ContextTitle:    planInteraction.stringField("contextTitle"),
		PromptMessage:   planInteraction.stringField("msg"),
		PermissionMode:  firstNonEmpty(planInteraction.stringField("permissionMode"), "default"),
		ResumeSessionID: planInteraction.stringField("resumeSessionId"),
		Command:         "claude",
		CWD:             ".",
		TargetPath:      planInteraction.stringField("targetPath"),
	}); err != nil {
		return nil, err
	}
	if _, err := r.waitForPlanContinuation(120 * time.Second); err != nil {
		return nil, err
	}
	r.transcript.line("plan_continue answer=%q", answerLabel)

	// After plan approval, Claude will attempt to write the file.
	// Wait for permission prompt and approve it, then capture the real file_diff.
	fileDiff, err := r.waitForFileDiffAfterApproval(180 * time.Second)
	if err != nil {
		return nil, err
	}
	r.transcript.line("file_diff path=%q title=%q contextId=%q groupId=%q executionId=%q",
		fileDiff.stringField("path"), fileDiff.stringField("title"),
		fileDiff.stringField("contextId"), fileDiff.stringField("groupId"),
		fileDiff.stringField("executionId"))
	return fileDiff, nil
}

func (r *smokeRunner) assertHistoryReviewState() error {
	r.transcript.section("history")
	if strings.TrimSpace(r.transcript.sessionID) == "" {
		return errors.New("session id unavailable before history verification")
	}
	if err := r.send(protocol.SessionLoadRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_load"}, SessionID: r.transcript.sessionID}); err != nil {
		return err
	}
	history, err := r.waitForType(protocol.EventTypeSessionHistory, 30*time.Second, nil)
	if err != nil {
		return err
	}
	r.transcript.line("history review_groups=%d canResume=%v", history.arrayLen("reviewGroups"), history.boolField("canResume"))
	return nil
}

func (r *smokeRunner) catalogFlow() error {
	r.transcript.section("catalog")
	memoryID := fmt.Sprintf("ws-smoke-memory-%d", time.Now().UnixNano())
	if err := r.send(protocol.MemoryRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "memory_upsert"},
		Item: protocol.MemoryItem{
			ID:      memoryID,
			Title:   "WS smoke memory",
			Content: "Remember the websocket smoke flow succeeded.",
		},
	}); err != nil {
		return err
	}

	memoryList, err := r.waitForType(protocol.EventTypeMemoryListResult, 30*time.Second, func(evt eventMap) bool {
		return evt.catalogMetaString("syncState") == "draft"
	})
	if err != nil {
		return err
	}
	if !memoryList.hasItemWithID(memoryID) {
		return fmt.Errorf("memory_upsert did not persist item %q", memoryID)
	}
	r.transcript.line("memory_list count=%d syncState=%q", memoryList.arrayLen("items"), memoryList.catalogMetaString("syncState"))

	if err := r.send(protocol.ClientEvent{Action: "memory_sync_pull"}); err != nil {
		return err
	}
	if _, err := r.waitForType(protocol.EventTypeCatalogSyncStatus, 30*time.Second, func(evt eventMap) bool {
		return evt.stringField("domain") == "memory"
	}); err != nil {
		return err
	}
	if _, err := r.waitForType(protocol.EventTypeCatalogSyncResult, 30*time.Second, func(evt eventMap) bool {
		return evt.stringField("domain") == "memory" && evt.boolField("success")
	}); err != nil {
		return err
	}
	memorySynced, err := r.waitForType(protocol.EventTypeMemoryListResult, 30*time.Second, func(evt eventMap) bool {
		return evt.catalogMetaString("syncState") == "synced" && evt.catalogMetaString("sourceOfTruth") == "claude"
	})
	if err != nil {
		return err
	}
	if !memorySynced.hasItemWithID(memoryID) {
		return fmt.Errorf("synced memory list lost item %q", memoryID)
	}

	if err := r.send(protocol.SkillRequestEvent{
		ClientEvent:  protocol.ClientEvent{Action: "skill_exec"},
		Name:         "review",
		CWD:          ".",
		TargetType:   "diff",
		TargetPath:   "README.md",
		TargetTitle:  "README smoke diff",
		TargetDiff:   "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@\n-Hello\n+Hello, smoke test.\n",
		ResultView:   "review-card",
		ContextID:    "smoke-readme-diff",
		ContextTitle: "README smoke diff",
	}); err != nil {
		return err
	}
	r.transcript.line("skill_exec queued name=%q target=%q", "review", "README.md")

	if err := r.send(protocol.ClientEvent{Action: "skill_sync_pull"}); err != nil {
		return err
	}
	if _, err := r.waitForAnyType(30*time.Second, []string{protocol.EventTypeCatalogSyncStatus, protocol.EventTypeSkillSyncResult, protocol.EventTypeCatalogSyncResult, protocol.EventTypeSkillCatalogResult}, func(evt eventMap) bool {
		if evt.stringField("type") == protocol.EventTypeCatalogSyncStatus {
			return evt.stringField("domain") == "skill"
		}
		if evt.stringField("type") == protocol.EventTypeSkillSyncResult {
			return strings.Contains(evt.stringField("msg"), "同步完成")
		}
		if evt.stringField("type") == protocol.EventTypeCatalogSyncResult {
			return evt.stringField("domain") == "skill" && evt.boolField("success")
		}
		return evt.catalogMetaString("syncState") == "synced" && evt.catalogMetaString("sourceOfTruth") == "claude"
	}); err != nil {
		return err
	}
	skillCatalog, err := r.waitForType(protocol.EventTypeSkillCatalogResult, 30*time.Second, func(evt eventMap) bool {
		return evt.catalogMetaString("syncState") == "synced" && evt.catalogMetaString("sourceOfTruth") == "claude"
	})
	if err != nil {
		return err
	}
	if skillCatalog.arrayLen("items") == 0 {
		return errors.New("skill sync produced empty catalog")
	}

	if err := r.send(protocol.SessionContextUpdateRequestEvent{
		ClientEvent:       protocol.ClientEvent{Action: "session_context_update"},
		EnabledSkillNames: []string{"review"},
	}); err != nil {
		return err
	}
	ctxResult, err := r.waitForType(protocol.EventTypeSessionContextResult, 30*time.Second, func(evt eventMap) bool {
		return evt.nestedArrayLen("sessionContext", "enabledSkillNames") == 1
	})
	if err != nil {
		return err
	}
	if ctxResult.nestedArrayLen("sessionContext", "enabledSkillNames") != 1 {
		return errors.New("session context update did not enable review skill")
	}
	return nil
}

func (r *smokeRunner) finalize() error {
	r.transcript.section("finalize")
	if err := r.send(protocol.InputRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "input"},
		Data:        "最后确认：请继续保持同一个会话，并用一句话回复已完成。\n",
	}); err != nil {
		return err
	}
	if _, err := r.waitForAnyType(120*time.Second, []string{protocol.EventTypeLog, protocol.EventTypeProgress, protocol.EventTypeAgentState}, nil); err != nil {
		return err
	}

	if err := r.send(protocol.InputRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "input"},
		Data:        "再补一条：请继续只修改 README.md，在 `### Smoke test` 标题下面再追加一行不同的 smoke test 说明；如果找不到这个标题，就把这一行追加到 README 末尾。不要重写整段，不要依赖现有正文的精确匹配。\n",
	}); err != nil {
		return err
	}
	secondPermissionPrompt, err := r.waitForWritePermissionPrompt(120 * time.Second)
	if err != nil {
		return err
	}
	r.transcript.line("second_permission_prompt msg=%q options=%v", secondPermissionPrompt.stringField("msg"), secondPermissionPrompt.stringSlice("options"))

	if err := r.send(protocol.SessionLoadRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_load"}, SessionID: r.transcript.sessionID}); err != nil {
		return err
	}
	loaded, err := r.waitForType(protocol.EventTypeSessionHistory, 30*time.Second, nil)
	if err != nil {
		return err
	}
	if !loaded.boolField("canResume") {
		return errors.New("expected loaded session to remain resumable")
	}
	if loaded.nestedArrayLen("sessionContext", "enabledSkillNames") != 1 {
		return fmt.Errorf("unexpected session context in history: %q", loaded.compactString())
	}
	return nil
}

func (r *smokeRunner) terminalLogFlow() error {
	r.transcript.section("terminal")
	command := "python3 -c \"import sys,time; print('RT-OUT-1'); sys.stdout.flush(); time.sleep(0.2); print('RT-OUT-2'); sys.stdout.flush(); print('RT-ERR-1', file=sys.stderr); sys.stderr.flush()\""
	if err := r.send(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     command,
		Mode:        "exec",
		CWD:         ".",
	}); err != nil {
		return err
	}

	stdoutSeen := false
	stderrSeen := false
	finishedSeen := false
	executionID := ""

	for !(stdoutSeen && stderrSeen && finishedSeen) {
		evt, err := r.waitForType(protocol.EventTypeLog, 60*time.Second, nil)
		if err != nil {
			return err
		}
		stream := strings.TrimSpace(evt.stringField("stream"))
		phase := strings.TrimSpace(evt.stringField("phase"))
		msg := evt.stringField("msg")
		currentExecutionID := strings.TrimSpace(evt.stringField("executionId"))
		if executionID == "" && currentExecutionID != "" {
			executionID = currentExecutionID
		}
		if currentExecutionID != "" && executionID != "" && currentExecutionID != executionID {
			return fmt.Errorf("expected one execution id, got %q and %q", executionID, currentExecutionID)
		}
		switch {
		case stream == "stdout" && strings.Contains(msg, "RT-OUT-1"):
			stdoutSeen = true
		case stream == "stderr" && strings.Contains(msg, "RT-ERR-1"):
			stderrSeen = true
		case phase == "finished":
			finishedSeen = true
		}
	}
	if executionID == "" {
		return errors.New("expected execution id in terminal log flow")
	}
	if _, err := r.waitForAnyType(30*time.Second, []string{protocol.EventTypeAgentState, protocol.EventTypeSessionState}, func(evt eventMap) bool {
		state := strings.ToLower(strings.TrimSpace(evt.stringField("state")))
		return state == "idle" || evt.boolField("awaitInput")
	}); err != nil {
		return err
	}

	if err := r.send(protocol.SessionLoadRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_load"}, SessionID: r.transcript.sessionID}); err != nil {
		return err
	}
	history, err := r.waitForType(protocol.EventTypeSessionHistory, 30*time.Second, nil)
	if err != nil {
		return err
	}
	stdout := history.nestedString("rawTerminalByStream", "stdout")
	stderr := history.nestedString("rawTerminalByStream", "stderr")
	if !strings.Contains(stdout, "RT-OUT-1") || !strings.Contains(stdout, "RT-OUT-2") {
		return fmt.Errorf("expected stdout tokens in history, got %q", stdout)
	}
	if !strings.Contains(stderr, "RT-ERR-1") {
		return fmt.Errorf("expected stderr token in history, got %q", stderr)
	}
	for _, item := range history.objectSlice("terminalExecutions") {
		if item.stringField("executionId") != executionID {
			continue
		}
		if !strings.Contains(item.stringField("stdout"), "RT-OUT-1") || !strings.Contains(item.stringField("stdout"), "RT-OUT-2") {
			return fmt.Errorf("expected execution stdout tokens, got %#v", item)
		}
		if !strings.Contains(item.stringField("stderr"), "RT-ERR-1") {
			return fmt.Errorf("expected execution stderr token, got %#v", item)
		}
		if exitCode, ok := item["exitCode"].(float64); ok && int(exitCode) != 0 {
			return fmt.Errorf("expected exitCode 0, got %#v", item)
		}
		r.transcript.line("terminal_history execution=%q stdout_ok=yes stderr_ok=yes", executionID)
		return nil
	}
	return fmt.Errorf("did not find terminal execution %q in history: %s", executionID, history.compactString())
}

func (r *smokeRunner) codexBasicFlow() error {
	r.transcript.section("codex-basic")

	cmd := strings.TrimSpace(r.aiCommand)
	if cmd == "" {
		cmd = "codex --version"
	}
	engine := strings.TrimSpace(r.engine)
	if engine == "" {
		engine = "codex"
	}

	if err := r.send(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     cmd,
		Mode:        "pty",
		CWD:         ".",
		RuntimeMeta: protocol.RuntimeMeta{
			Engine: engine,
			Source: "smoke-codex",
		},
		PermissionMode: "default",
	}); err != nil {
		return err
	}

	firstEvt, err := r.waitForAnyType(45*time.Second, []string{
		protocol.EventTypePromptRequest,
		protocol.EventTypeLog,
		protocol.EventTypeError,
		protocol.EventTypeAgentState,
		protocol.EventTypeSessionState,
	}, nil)
	if err != nil {
		return err
	}
	r.transcript.line("codex_first_event type=%s", firstEvt.stringField("type"))

	if firstEvt.stringField("type") == protocol.EventTypePromptRequest {
		if err := r.send(protocol.InputRequestEvent{
			ClientEvent: protocol.ClientEvent{Action: "input"},
			Data:        "echo CODEX_SMOKE_OK\n",
		}); err != nil {
			return err
		}
	}

	if _, err := r.waitForAnyType(30*time.Second, []string{
		protocol.EventTypeAgentState,
		protocol.EventTypeSessionState,
		protocol.EventTypeLog,
		protocol.EventTypeError,
	}, nil); err != nil {
		return err
	}

	if err := r.send(protocol.SessionLoadRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "session_load"},
		SessionID:   r.transcript.sessionID,
	}); err != nil {
		return err
	}
	history, err := r.waitForType(protocol.EventTypeSessionHistory, 30*time.Second, nil)
	if err != nil {
		return err
	}
	if history.nestedInt("summary", "entryCount") <= 0 {
		return fmt.Errorf("expected codex-basic history entryCount > 0, got %d", history.nestedInt("summary", "entryCount"))
	}
	r.transcript.line("codex_history entry_count=%d", history.nestedInt("summary", "entryCount"))
	return nil
}

func (r *smokeRunner) codexReadmeWriteFlow() error {
	r.transcript.section("codex-readme-write")

	const readmePath = "README.md"
	const expectedPhrase = "Claude、Codex 等 AI CLI"
	original, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", readmePath, err)
	}
	defer func() {
		_ = os.WriteFile(readmePath, original, 0644)
	}()

	engine := strings.TrimSpace(r.engine)
	if engine == "" {
		engine = "codex"
	}
	prompt := "请只修改 README.md：把文档里“让手机成为你操控电脑上 Claude Code 的主入口”这句话改写为“让手机成为你操控电脑上 Claude、Codex 等 AI CLI 的主入口”。只改这一处并保存文件，然后回复 DONE。"
	cmd := "codex exec --dangerously-bypass-approvals-and-sandbox " + shellSingleQuote(prompt)

	if err := r.send(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     cmd,
		Mode:        "exec",
		CWD:         ".",
		RuntimeMeta: protocol.RuntimeMeta{
			Engine: engine,
			Source: "smoke-codex-write",
		},
		PermissionMode: "default",
	}); err != nil {
		return err
	}

	if _, err := r.waitForAnyType(45*time.Second, []string{
		protocol.EventTypeAgentState,
		protocol.EventTypeSessionState,
		protocol.EventTypePromptRequest,
		protocol.EventTypeLog,
	}, nil); err != nil {
		return err
	}

	gotDiff := false
	sawDone := false
	deadline := time.Now().Add(4 * time.Minute)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New("timeout waiting codex README write completion")
		}
		evt, err := r.waitForAnyType(remaining, []string{
			protocol.EventTypeFileDiff,
			protocol.EventTypePromptRequest,
			protocol.EventTypeInteractionRequest,
			protocol.EventTypeAgentState,
			protocol.EventTypeError,
			protocol.EventTypeLog,
			protocol.EventTypeProgress,
		}, nil)
		if err != nil {
			return err
		}
		switch evt.stringField("type") {
		case protocol.EventTypeFileDiff:
			if strings.Contains(strings.ToLower(evt.stringField("path")), "readme") {
				gotDiff = true
				r.transcript.line("codex_readme_diff title=%q path=%q", evt.stringField("title"), evt.stringField("path"))
			}
		case protocol.EventTypeError:
			return fmt.Errorf("codex write flow error: %s", evt.stringField("msg"))
		case protocol.EventTypeAgentState:
			if strings.EqualFold(evt.stringField("state"), "IDLE") && (gotDiff || sawDone) {
				goto VERIFY
			}
		case protocol.EventTypeSessionState:
			if strings.EqualFold(evt.stringField("state"), "closed") && (gotDiff || sawDone) {
				goto VERIFY
			}
		case protocol.EventTypeLog:
			if strings.Contains(strings.ToUpper(evt.stringField("msg")), "DONE") {
				sawDone = true
			}
		}
	}

VERIFY:
	updated, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("read updated %s: %w", readmePath, err)
	}
	updatedText := string(updated)
	if !strings.Contains(updatedText, expectedPhrase) {
		return fmt.Errorf("README write assertion failed: expected phrase %q not found", expectedPhrase)
	}
	if string(original) == updatedText {
		return errors.New("README content unchanged after codex write flow")
	}
	r.transcript.line("codex_readme_write_verified phrase=%q", expectedPhrase)
	return nil
}

func shellSingleQuote(text string) string {
	if text == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(text, "'", "'\"'\"'") + "'"
}

func reviewStateHasAcceptedGroup(evt eventMap, targetGroupID string) bool {
	for _, group := range evt.objectSlice("groups") {
		groupID := strings.TrimSpace(group.stringField("id"))
		if strings.TrimSpace(targetGroupID) != "" && groupID != strings.TrimSpace(targetGroupID) {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(group.stringField("reviewStatus")))
		pending := group.boolField("pendingReview")
		if status == "accepted" && !pending {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isWritePermissionEvent(evt eventMap) bool {
	switch evt.stringField("type") {
	case protocol.EventTypePromptRequest:
		msg := strings.ToLower(strings.TrimSpace(evt.stringField("msg")))
		if strings.Contains(msg, "已就绪") || strings.Contains(msg, "继续输入") {
			return false
		}
		if strings.Contains(msg, "授权") || strings.Contains(msg, "权限") {
			return true
		}
		options := evt.stringSlice("options")
		return len(options) > 0
	case protocol.EventTypeStepUpdate:
		msg := strings.ToLower(strings.TrimSpace(evt.stringField("msg")))
		return strings.Contains(msg, "permission") || strings.Contains(msg, "write") || strings.Contains(msg, "权限")
	default:
		return false
	}
}

func normalizePermissionPromptEvent(evt eventMap) eventMap {
	if evt.stringField("type") == protocol.EventTypeStepUpdate {
		return eventMap{"msg": evt.stringField("msg"), "options": []any{"approve", "deny"}}
	}
	return evt
}

func (r *smokeRunner) waitForFileDiffAfterApproval(timeout time.Duration) (eventMap, error) {
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("timeout waiting for file_diff after plan approval")
		}
		evt, err := r.waitForAnyType(remaining, []string{
			protocol.EventTypeFileDiff,
			protocol.EventTypePromptRequest,
			protocol.EventTypeStepUpdate,
			protocol.EventTypeInteractionRequest,
			protocol.EventTypeAgentState,
			protocol.EventTypeLog,
			protocol.EventTypeProgress,
			protocol.EventTypeReviewState,
		}, func(evt eventMap) bool {
			switch evt.stringField("type") {
			case protocol.EventTypeFileDiff:
				return true
			case protocol.EventTypePromptRequest:
				return isWritePermissionEvent(evt)
			case protocol.EventTypeStepUpdate:
				return isWritePermissionEvent(evt)
			case protocol.EventTypeInteractionRequest:
				return strings.EqualFold(strings.TrimSpace(evt.stringField("kind")), "permission")
			case protocol.EventTypeAgentState:
				return evt.boolField("awaitInput") || strings.EqualFold(evt.stringField("state"), "IDLE")
			case protocol.EventTypeLog, protocol.EventTypeProgress, protocol.EventTypeReviewState:
				return false
			default:
				return false
			}
		})
		if err != nil {
			return nil, err
		}
		switch evt.stringField("type") {
		case protocol.EventTypeFileDiff:
			return evt, nil
		case protocol.EventTypePromptRequest, protocol.EventTypeStepUpdate, protocol.EventTypeInteractionRequest:
			r.transcript.line("plan_permission_auto_approve msg=%q", evt.stringField("msg"))
			if err := r.send(protocol.PermissionDecisionRequestEvent{
				ClientEvent:    protocol.ClientEvent{Action: "permission_decision"},
				Decision:       "approve",
				PermissionMode: firstNonEmpty(evt.stringField("permissionMode"), "default"),
				ResumeSessionID: firstNonEmpty(
					evt.stringField("resumeSessionId"),
					evt.nestedString("resumeRuntimeMeta", "resumeSessionId"),
				),
				PromptMessage:      evt.stringField("msg"),
				FallbackCommand:    "claude",
				FallbackCWD:        ".",
				FallbackEngine:     evt.stringField("engine"),
				FallbackTarget:     evt.stringField("target"),
				FallbackTargetType: evt.stringField("targetType"),
			}); err != nil {
				return nil, err
			}
			continue
		case protocol.EventTypeAgentState:
			if strings.EqualFold(evt.stringField("state"), "IDLE") {
				return nil, fmt.Errorf("claude went IDLE without producing a file_diff")
			}
			continue
		}
	}
}

func (r *smokeRunner) waitForWritePermissionPrompt(timeout time.Duration) (eventMap, error) {
	evt, err := r.waitForAnyType(timeout, []string{protocol.EventTypePromptRequest, protocol.EventTypeStepUpdate, protocol.EventTypeAgentState, protocol.EventTypeLog, protocol.EventTypeProgress}, func(evt eventMap) bool {
		return isWritePermissionEvent(evt)
	})
	if err != nil {
		return nil, err
	}
	return normalizePermissionPromptEvent(evt), nil
}

func (r *smokeRunner) waitForReviewReady(targetGroupID string) error {
	_, err := r.waitForAnyType(90*time.Second, []string{protocol.EventTypeReviewState, protocol.EventTypeAgentState, protocol.EventTypeFileDiff, protocol.EventTypePromptRequest, protocol.EventTypeLog, protocol.EventTypeProgress}, func(evt eventMap) bool {
		switch evt.stringField("type") {
		case protocol.EventTypeReviewState:
			groups := evt.objectSlice("groups")
			if len(groups) == 0 {
				return false
			}
			for _, group := range groups {
				groupID := strings.TrimSpace(group.stringField("id"))
				if strings.TrimSpace(targetGroupID) != "" && groupID != strings.TrimSpace(targetGroupID) {
					continue
				}
				status := strings.ToLower(strings.TrimSpace(group.stringField("reviewStatus")))
				if group.boolField("pendingReview") || status == "pending" || status == "" {
					return true
				}
			}
			return false
		case protocol.EventTypeAgentState:
			return evt.boolField("awaitInput")
		case protocol.EventTypePromptRequest:
			msg := strings.ToLower(strings.TrimSpace(evt.stringField("msg")))
			return strings.Contains(msg, "accept") || strings.Contains(msg, "revert") || strings.Contains(msg, "审核") || strings.Contains(msg, "review")
		case protocol.EventTypeLog, protocol.EventTypeProgress:
			return true
		case protocol.EventTypeFileDiff:
			return false
		default:
			return false
		}
	})
	return err
}

func (r *smokeRunner) waitForPlanInteraction(timeout time.Duration) (eventMap, error) {
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("timeout waiting for event type(s) %v", []string{protocol.EventTypeInteractionRequest})
		}
		evt, err := r.waitForAnyType(remaining, []string{protocol.EventTypeInteractionRequest, protocol.EventTypeRuntimePhase, protocol.EventTypePromptRequest, protocol.EventTypeAgentState, protocol.EventTypeLog, protocol.EventTypeProgress, protocol.EventTypeStepUpdate, protocol.EventTypeError}, func(evt eventMap) bool {
			switch evt.stringField("type") {
			case protocol.EventTypeInteractionRequest:
				return strings.EqualFold(strings.TrimSpace(evt.stringField("kind")), "plan")
			case protocol.EventTypeRuntimePhase:
				phase := strings.ToLower(strings.TrimSpace(evt.stringField("phase")))
				kind := strings.ToLower(strings.TrimSpace(evt.stringField("kind")))
				if kind == "plan" && (phase == "plan_requested" || phase == "plan_active") {
					r.transcript.line("plan_phase phase=%q kind=%q", evt.stringField("phase"), evt.stringField("kind"))
				}
				return false
			case protocol.EventTypePromptRequest:
				msg := strings.TrimSpace(evt.stringField("msg"))
				return strings.Contains(msg, "AskUserQuestion") || strings.Contains(msg, "permissions to use")
			case protocol.EventTypeStepUpdate:
				msg := strings.TrimSpace(evt.stringField("msg"))
				status := strings.ToLower(strings.TrimSpace(evt.stringField("status")))
				return strings.EqualFold(msg, "EnterPlanMode") || (strings.EqualFold(evt.stringField("tool"), "EnterPlanMode") && status == "completed")
			case protocol.EventTypeError:
				return true
			default:
				return false
			}
		})
		if err != nil {
			return nil, err
		}
		if evt.stringField("type") == protocol.EventTypeError {
			msg := strings.ToLower(evt.stringField("msg"))
			if strings.Contains(msg, "plan") || strings.Contains(msg, "json") || strings.Contains(msg, "tool") {
				return nil, fmt.Errorf("tool_truncation_or_error: %s", evt.stringField("msg"))
			}
			continue
		}
		if evt.stringField("type") == protocol.EventTypePromptRequest {
			r.transcript.line("plan_permission_prompt msg=%q options=%v", evt.stringField("msg"), evt.stringSlice("options"))
			if err := r.send(protocol.PermissionDecisionRequestEvent{
				ClientEvent:        protocol.ClientEvent{Action: "permission_decision"},
				Decision:           "approve",
				PermissionMode:     firstNonEmpty(evt.stringField("permissionMode"), "default"),
				ResumeSessionID:    evt.stringField("resumeSessionId"),
				PromptMessage:      evt.stringField("msg"),
				FallbackCommand:    "claude",
				FallbackCWD:        ".",
				FallbackEngine:     evt.stringField("engine"),
				FallbackTarget:     evt.stringField("target"),
				FallbackTargetType: evt.stringField("targetType"),
			}); err != nil {
				return nil, err
			}
			continue
		}
		if evt.stringField("type") == protocol.EventTypeStepUpdate {
			r.transcript.line("plan_step msg=%q status=%q", evt.stringField("msg"), evt.stringField("status"))
			continue
		}
		if evt.stringField("type") == protocol.EventTypeAgentState {
			state := strings.ToLower(strings.TrimSpace(evt.stringField("state")))
			if state == "idle" {
				// claude responded without entering plan mode — retry the prompt
				r.transcript.line("plan_retry claude_idle_without_plan")
				if err := r.send(protocol.InputRequestEvent{
					ClientEvent: protocol.ClientEvent{Action: "input"},
					Data:        "任务：在 README.md 末尾追加一行 'smoke test passed'。请用 EnterPlanMode 工具给出计划并等待我确认。\n",
				}); err != nil {
					return nil, err
				}
			}
			continue
		}
		return evt, nil
	}
}

func (r *smokeRunner) waitForPlanContinuation(timeout time.Duration) (eventMap, error) {
	return r.waitForAnyType(timeout, []string{protocol.EventTypeAgentState, protocol.EventTypeLog, protocol.EventTypeProgress, protocol.EventTypeStepUpdate, protocol.EventTypeError}, func(evt eventMap) bool {
		switch evt.stringField("type") {
		case protocol.EventTypeAgentState:
			return strings.EqualFold(evt.stringField("source"), "plan-decision") || strings.EqualFold(evt.stringField("state"), "THINKING")
		case protocol.EventTypeLog, protocol.EventTypeProgress, protocol.EventTypeStepUpdate:
			return true
		case protocol.EventTypeError:
			return true
		default:
			return false
		}
	})
}

func buildPlanDecisionPayload(sessionID string, interaction eventMap) (string, string, error) {
	questions := extractPlanQuestions(interaction)
	questionID := "question-1"
	answerValue := "继续"
	answerLabel := answerValue
	if len(questions) > 0 {
		question := questions[0]
		questionID = firstNonEmpty(question.stringField("id"), question.stringField("questionId"), question.stringField("key"), questionID)
		answerValue, answerLabel = selectPlanAnswer(question)
	}
	payload := map[string]any{
		"kind":            "plan",
		"sessionId":       sessionID,
		"resumeSessionId": interaction.stringField("resumeSessionId"),
		"executionId":     interaction.stringField("executionId"),
		"groupId":         interaction.stringField("groupId"),
		"groupTitle":      interaction.stringField("groupTitle"),
		"contextId":       interaction.stringField("contextId"),
		"contextTitle":    interaction.stringField("contextTitle"),
		"targetPath":      interaction.stringField("targetPath"),
		"answers": map[string]string{
			questionID: answerLabel,
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}
	_ = answerValue
	return string(encoded), answerLabel, nil
}

func extractPlanQuestions(interaction eventMap) []eventMap {
	for _, key := range []string{"questions", "planQuestions", "steps"} {
		if questions := interaction.objectSlice(key); len(questions) > 0 {
			return questions
		}
	}
	for _, parent := range []string{"details", "detail", "data"} {
		raw, ok := interaction[parent]
		if !ok {
			continue
		}
		obj, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		nested := eventMap(obj)
		for _, key := range []string{"questions", "planQuestions", "steps"} {
			if questions := nested.objectSlice(key); len(questions) > 0 {
				return questions
			}
		}
	}
	return nil
}

func selectPlanAnswer(question eventMap) (string, string) {
	for _, key := range []string{"options", "choices", "buttons", "selections"} {
		options := question.objectSlice(key)
		if len(options) == 0 {
			continue
		}
		option := options[0]
		value := firstNonEmpty(option.stringField("value"), option.stringField("id"), option.stringField("key"), option.stringField("label"), option.stringField("title"), option.stringField("text"), option.stringField("msg"))
		label := firstNonEmpty(option.stringField("label"), option.stringField("title"), option.stringField("text"), option.stringField("displayText"), option.stringField("value"), value)
		if value == "" {
			continue
		}
		return value, label
	}
	for _, key := range []string{"options", "choices"} {
		values := question.stringSlice(key)
		if len(values) > 0 {
			return values[0], values[0]
		}
	}
	return "继续", "继续"
}

func (r *smokeRunner) send(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	r.transcript.send(payload)
	return r.conn.WriteMessage(websocket.TextMessage, payload)
}

func (r *smokeRunner) waitForType(want string, timeout time.Duration, predicate func(eventMap) bool) (eventMap, error) {
	return r.waitForAnyType(timeout, []string{want}, predicate)
}

func (r *smokeRunner) waitForAnyType(timeout time.Duration, wantTypes []string, predicate func(eventMap) bool) (eventMap, error) {
	want := make(map[string]struct{}, len(wantTypes))
	for _, t := range wantTypes {
		want[t] = struct{}{}
	}
	deadline := time.Now().Add(timeout)
	for {
		if r.ctx.Err() != nil {
			return nil, r.ctx.Err()
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("timeout waiting for event type(s) %v", wantTypes)
		}
		if err := r.conn.SetReadDeadline(time.Now().Add(minDuration(remaining, 3*time.Minute))); err != nil {
			return nil, err
		}
		_, data, err := r.conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		evt, err := decodeEvent(data)
		if err != nil {
			return nil, err
		}
		r.transcript.recv(data, evt)
		if err := r.transcript.checkNoise(evt); err != nil {
			return nil, err
		}
		if _, ok := want[evt.stringField("type")]; !ok {
			continue
		}
		if predicate != nil && !predicate(evt) {
			continue
		}
		if sessionID := evt.stringField("sessionId"); sessionID != "" && r.transcript.sessionID == "" {
			r.transcript.sessionID = sessionID
		}
		if evt.stringField("type") == protocol.EventTypeSessionCreated {
			if sessionID := evt.nestedString("summary", "id"); sessionID != "" {
				r.transcript.sessionID = sessionID
			}
		}
		return evt, nil
	}
}

func decodeEvent(data []byte) (eventMap, error) {
	var evt eventMap
	if err := json.Unmarshal(data, &evt); err != nil {
		return nil, err
	}
	return evt, nil
}

type transcript struct {
	out       *os.File
	sessionID string
}

func newTranscript(out *os.File) *transcript { return &transcript{out: out} }

func (t *transcript) section(name string) { t.line("== %s ==", name) }

func (t *transcript) line(format string, args ...any) {
	fmt.Fprintf(t.out, format+"\n", args...)
}

func (t *transcript) send(payload []byte) {
	t.line("send %s", payload)
}

func (t *transcript) recv(payload []byte, evt eventMap) {
	t.line("recv %s %s", evt.stringField("type"), summarizeEvent(evt, payload))
}

func (t *transcript) checkNoise(evt eventMap) error {
	joined := strings.ToLower(evt.compactString())
	if strings.Contains(joined, "no active runner") {
		return fmt.Errorf("unexpected noisy state: %s", evt.stringField("type"))
	}
	return nil
}

type eventMap map[string]any

func (e eventMap) stringField(key string) string {
	if e == nil {
		return ""
	}
	if v, ok := e[key]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

func (e eventMap) boolField(key string) bool {
	if e == nil {
		return false
	}
	v, ok := e[key]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func (e eventMap) stringSlice(key string) []string {
	if e == nil {
		return nil
	}
	raw, ok := e[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, fmt.Sprint(item))
	}
	return result
}

func (e eventMap) objectSlice(key string) []eventMap {
	if e == nil {
		return nil
	}
	raw, ok := e[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]eventMap, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, eventMap(obj))
	}
	return result
}

func (e eventMap) arrayLen(key string) int {
	if e == nil {
		return 0
	}
	raw, ok := e[key]
	if !ok {
		return 0
	}
	items, ok := raw.([]any)
	if !ok {
		return 0
	}
	return len(items)
}

func (e eventMap) nestedString(parent, child string) string {
	if e == nil {
		return ""
	}
	raw, ok := e[parent]
	if !ok {
		return ""
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	return fmt.Sprint(obj[child])
}

func (e eventMap) nestedArrayLen(parent, child string) int {
	if e == nil {
		return 0
	}
	raw, ok := e[parent]
	if !ok {
		return 0
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return 0
	}
	items, ok := obj[child].([]any)
	if !ok {
		return 0
	}
	return len(items)
}

func (e eventMap) nestedInt(parent, child string) int {
	rawParent, ok := e[parent]
	if !ok {
		return 0
	}
	parentMap, ok := rawParent.(map[string]any)
	if !ok {
		return 0
	}
	raw, ok := parentMap[child]
	if !ok {
		return 0
	}
	switch value := raw.(type) {
	case float64:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	case json.Number:
		n, err := value.Int64()
		if err == nil {
			return int(n)
		}
	}
	return 0
}

func (e eventMap) catalogMetaString(key string) string {
	if e == nil {
		return ""
	}
	raw, ok := e["meta"]
	if !ok {
		return ""
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	return fmt.Sprint(obj[key])
}

func (e eventMap) hasItemWithID(id string) bool {
	raw, ok := e["items"]
	if !ok {
		return false
	}
	items, ok := raw.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if fmt.Sprint(obj["id"]) == id {
			return true
		}
	}
	return false
}

func (e eventMap) compactString() string {
	payload, err := json.Marshal(e)
	if err != nil {
		return fmt.Sprint(e)
	}
	return string(payload)
}

func summarizeEvent(evt eventMap, raw []byte) string {
	switch evt.stringField("type") {
	case protocol.EventTypeSessionState:
		return fmt.Sprintf("state=%q msg=%q raw=%s", evt.stringField("state"), evt.stringField("msg"), raw)
	case protocol.EventTypeAgentState:
		return fmt.Sprintf("state=%q msg=%q awaitInput=%v source=%q command=%q raw=%s", evt.stringField("state"), evt.stringField("msg"), evt.boolField("awaitInput"), evt.stringField("source"), evt.stringField("command"), raw)
	case protocol.EventTypePromptRequest:
		return fmt.Sprintf("msg=%q options=%v raw=%s", evt.stringField("msg"), evt.stringSlice("options"), raw)
	case protocol.EventTypeInteractionRequest:
		return fmt.Sprintf("kind=%q title=%q msg=%q raw=%s", evt.stringField("kind"), evt.stringField("title"), evt.stringField("msg"), raw)
	case protocol.EventTypeSessionCreated:
		return fmt.Sprintf("session=%q title=%q raw=%s", evt.nestedString("summary", "id"), evt.nestedString("summary", "title"), raw)
	case protocol.EventTypeSessionListResult:
		return fmt.Sprintf("count=%d raw=%s", evt.arrayLen("items"), raw)
	case protocol.EventTypeSkillCatalogResult:
		return fmt.Sprintf("count=%d syncState=%q sourceOfTruth=%q raw=%s", evt.arrayLen("items"), evt.catalogMetaString("syncState"), evt.catalogMetaString("sourceOfTruth"), raw)
	case protocol.EventTypeMemoryListResult:
		return fmt.Sprintf("count=%d syncState=%q sourceOfTruth=%q raw=%s", evt.arrayLen("items"), evt.catalogMetaString("syncState"), evt.catalogMetaString("sourceOfTruth"), raw)
	case protocol.EventTypeCatalogSyncStatus:
		return fmt.Sprintf("domain=%q syncState=%q raw=%s", evt.stringField("domain"), evt.nestedString("meta", "syncState"), raw)
	case protocol.EventTypeCatalogSyncResult:
		return fmt.Sprintf("domain=%q success=%v msg=%q syncState=%q raw=%s", evt.stringField("domain"), evt.boolField("success"), evt.stringField("msg"), evt.nestedString("meta", "syncState"), raw)
	case protocol.EventTypeSkillSyncResult:
		return fmt.Sprintf("msg=%q raw=%s", evt.stringField("msg"), raw)
	case protocol.EventTypeFileDiff:
		return fmt.Sprintf("path=%q title=%q raw=%s", evt.stringField("path"), evt.stringField("title"), raw)
	case protocol.EventTypeReviewState:
		return fmt.Sprintf("groups=%d raw=%s", evt.arrayLen("groups"), raw)
	case protocol.EventTypeSessionHistory:
		return fmt.Sprintf("session=%q canResume=%v skillSync=%q memorySync=%q reviewGroups=%d raw=%s", evt.nestedString("summary", "id"), evt.boolField("canResume"), evt.nestedString("skillCatalogMeta", "syncState"), evt.nestedString("memoryCatalogMeta", "syncState"), evt.arrayLen("reviewGroups"), raw)
	case protocol.EventTypeLog:
		return fmt.Sprintf("msg=%q stream=%q phase=%q raw=%s", evt.stringField("msg"), evt.stringField("stream"), evt.stringField("phase"), raw)
	case protocol.EventTypeProgress:
		return fmt.Sprintf("msg=%q percent=%q raw=%s", evt.stringField("msg"), evt.stringField("percent"), raw)
	case protocol.EventTypeError:
		return fmt.Sprintf("msg=%q raw=%s", evt.stringField("msg"), raw)
	case protocol.EventTypeRuntimeInfoResult:
		return fmt.Sprintf("title=%q count=%d raw=%s", evt.stringField("title"), evt.arrayLen("items"), raw)
	default:
		return fmt.Sprintf("raw=%s", raw)
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
