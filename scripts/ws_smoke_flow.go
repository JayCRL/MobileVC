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
)

func main() {
	var (
		baseURL  = flag.String("url", "", "websocket url")
		timeout  = flag.Duration("timeout", defaultSmokeTimeout, "overall smoke timeout")
		scenario = flag.String("scenario", string(scenarioFull), "smoke scenario: full or permission-diff-review")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	wsURL := *baseURL
	if strings.TrimSpace(wsURL) == "" {
		wsURL = buildWSURL()
	}

	if err := runSmoke(ctx, wsURL, smokeScenario(strings.TrimSpace(*scenario)), os.Stdout); err != nil {
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
}

func runSmoke(ctx context.Context, wsURL string, scenario smokeScenario, out *os.File) error {
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

	runner := &smokeRunner{conn: conn, transcript: tr, ctx: ctx}
	conn.SetReadLimit(8 << 20)

	if err := runner.bootstrap(); err != nil {
		return err
	}
	if err := runner.selectSession(); err != nil {
		return err
	}
	fileDiff, err := runner.chatFlow()
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

func (r *smokeRunner) chatFlow() (eventMap, error) {
	r.transcript.section("chat")
	if err := r.send(protocol.ExecRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "exec"},
		Command:        "claude",
		Mode:           "pty",
		PermissionMode: "default",
	}); err != nil {
		return nil, err
	}

	if _, err := r.waitForType(protocol.EventTypeAgentState, 30*time.Second, nil); err != nil {
		return nil, err
	}
	if _, err := r.waitForType(protocol.EventTypeSessionState, 30*time.Second, nil); err != nil {
		return nil, err
	}
	if err := r.send(protocol.InputRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "input"},
		Data:        "请先简短确认你已就绪，然后立即帮我修改 README.md，添加一行 smoke test 说明。\n",
	}); err != nil {
		return nil, err
	}

	var permissionPrompt eventMap
	for {
		evt, err := r.waitForAnyType(180*time.Second, []string{protocol.EventTypePromptRequest, protocol.EventTypeStepUpdate, protocol.EventTypeAgentState, protocol.EventTypeLog, protocol.EventTypeProgress}, nil)
		if err != nil {
			return nil, err
		}
		switch evt.stringField("type") {
		case protocol.EventTypePromptRequest:
			msg := strings.ToLower(strings.TrimSpace(evt.stringField("msg")))
			if strings.Contains(msg, "已就绪") || strings.Contains(msg, "继续输入") {
				continue
			}
			if !strings.Contains(msg, "授权") && !strings.Contains(msg, "权限") && len(evt.stringSlice("options")) == 0 {
				continue
			}
			permissionPrompt = evt
		case protocol.EventTypeStepUpdate:
			msg := strings.ToLower(strings.TrimSpace(evt.stringField("msg")))
			if !strings.Contains(msg, "permission") && !strings.Contains(msg, "write") && !strings.Contains(msg, "权限") {
				continue
			}
			permissionPrompt = eventMap{"msg": evt.stringField("msg"), "options": []any{"approve", "deny"}}
		default:
			continue
		}
		break
	}
	r.transcript.line("permission_prompt msg=%q options=%v", permissionPrompt.stringField("msg"), permissionPrompt.stringSlice("options"))

	if err := r.send(protocol.PermissionDecisionRequestEvent{
		ClientEvent:     protocol.ClientEvent{Action: "permission_decision"},
		Decision:        "approve",
		PermissionMode:  "default",
		TargetPath:      "README.md",
		PromptMessage:   permissionPrompt.stringField("msg"),
		FallbackCommand: "claude",
		FallbackCWD:     ".",
	}); err != nil {
		return nil, err
	}

	for {
		evt, err := r.waitForAnyType(120*time.Second, []string{protocol.EventTypeAgentState, protocol.EventTypeError, protocol.EventTypeLog, protocol.EventTypeProgress, protocol.EventTypeStepUpdate}, nil)
		if err != nil {
			return nil, err
		}
		if evt.stringField("type") == protocol.EventTypeError {
			msg := strings.ToLower(evt.stringField("msg"))
			if strings.Contains(msg, "permission") || strings.Contains(msg, "授权") || strings.Contains(msg, "write") {
				continue
			}
		}
		if evt.stringField("type") == protocol.EventTypeAgentState && (strings.EqualFold(evt.stringField("source"), "permission-decision") || strings.EqualFold(evt.stringField("state"), "THINKING")) {
			break
		}
	}
	fileDiff, err := r.waitForType(protocol.EventTypeFileDiff, 180*time.Second, func(evt eventMap) bool {
		path := strings.ToLower(evt.stringField("path"))
		title := strings.ToLower(evt.stringField("title"))
		return strings.Contains(path, "readme") || strings.Contains(title, "readme")
	})
	if err != nil {
		return nil, err
	}
	r.transcript.line("file_diff path=%q title=%q", fileDiff.stringField("path"), fileDiff.stringField("title"))
	return fileDiff, nil
}

func (r *smokeRunner) reviewFlow(fileDiff eventMap) error {
	r.transcript.section("review")
	if err := r.waitForReviewReady(firstNonEmpty(fileDiff.stringField("groupId"), fileDiff.stringField("executionId"), fileDiff.stringField("path"))); err != nil {
		return err
	}
	r.transcript.line("review_ready")
	if err := r.send(protocol.ReviewDecisionRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "review_decision"},
		Decision:       "accept",
		ExecutionID:    fileDiff.stringField("executionId"),
		GroupID:        firstNonEmpty(fileDiff.stringField("groupId"), fileDiff.stringField("executionId")),
		GroupTitle:     firstNonEmpty(fileDiff.stringField("groupTitle"), fileDiff.stringField("title")),
		ContextID:      firstNonEmpty(fileDiff.stringField("contextId"), fileDiff.stringField("path")),
		ContextTitle:   firstNonEmpty(fileDiff.stringField("contextTitle"), fileDiff.stringField("title")),
		TargetPath:     fileDiff.stringField("path"),
		PermissionMode: "default",
	}); err != nil {
		return err
	}

	reviewState, err := r.waitForType(protocol.EventTypeReviewState, 60*time.Second, func(evt eventMap) bool {
		return evt.arrayLen("groups") > 0
	})
	if err != nil {
		return err
	}
	if !reviewStateHasAcceptedGroup(reviewState, firstNonEmpty(fileDiff.stringField("groupId"), fileDiff.stringField("executionId"), fileDiff.stringField("path"))) {
		return fmt.Errorf("review_state did not mark target group accepted: %s", reviewState.compactString())
	}
	r.transcript.line("review_state groups=%d accepted", reviewState.arrayLen("groups"))
	return nil
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
	if history.arrayLen("reviewGroups") == 0 {
		return fmt.Errorf("expected history review groups after review decision: %s", history.compactString())
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
		Data:        "再补一条：请再次修改 README.md，追加一行关于 smoke test 的说明。\n",
	}); err != nil {
		return err
	}
	secondPermissionPrompt, err := r.waitForAnyType(120*time.Second, []string{protocol.EventTypePromptRequest, protocol.EventTypeStepUpdate}, nil)
	if err != nil {
		return err
	}
	if secondPermissionPrompt.stringField("type") == protocol.EventTypePromptRequest {
		r.transcript.line("second_permission_prompt msg=%q options=%v", secondPermissionPrompt.stringField("msg"), secondPermissionPrompt.stringSlice("options"))
	}

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
		if err := r.conn.SetReadDeadline(time.Now().Add(minDuration(remaining, 15*time.Second))); err != nil {
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
