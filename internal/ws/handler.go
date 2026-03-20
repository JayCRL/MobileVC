package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
	"mobilevc/internal/session"
)

type Handler struct {
	AuthToken     string
	NewExecRunner func() runner.Runner
	NewPtyRunner  func() runner.Runner
	Upgrader      websocket.Upgrader
}

func NewHandler(authToken string) *Handler {
	return &Handler{
		AuthToken: authToken,
		NewExecRunner: func() runner.Runner {
			return runner.NewExecRunner()
		},
		NewPtyRunner: func() runner.Runner {
			return runner.NewPtyRunner()
		},
		Upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" || token != h.AuthToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := h.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	sessionID := fmt.Sprintf("session-%d", time.Now().UTC().UnixNano())
	controller := session.NewController(sessionID)
	writeCh := make(chan any, 128)
	writeErrCh := make(chan error, 1)
	var writerWG sync.WaitGroup
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-writeCh:
				if !ok {
					return
				}
				if err := conn.WriteJSON(event); err != nil {
					select {
					case writeErrCh <- err:
					default:
					}
					return
				}
			}
		}
	}()

	var runnerMu sync.Mutex
	var activeRunner runner.Runner
	var activeSessionID string

	cleanup := func() {
		runnerMu.Lock()
		current := activeRunner
		activeRunner = nil
		activeSessionID = ""
		runnerMu.Unlock()
		if current != nil {
			_ = current.Close()
		}
	}
	defer func() {
		cancel()
		cleanup()
		writerWG.Wait()
	}()

	enqueueEvent(ctx, writeCh, protocol.NewSessionStateEvent(sessionID, string(session.StateActive), "connected"))
	enqueueEvent(ctx, writeCh, controller.InitialEvent())

	for {
		select {
		case err := <-writeErrCh:
			if err != nil {
				log.Printf("write websocket event failed: %v", err)
			}
			return
		default:
		}

		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("unexpected websocket close: %v", err)
			}
			return
		}

		if messageType != websocket.TextMessage {
			enqueueEvent(ctx, writeCh, protocol.NewErrorEvent(sessionID, "only text messages are supported", ""))
			continue
		}

		var clientEvent protocol.ClientEvent
		if err := json.Unmarshal(payload, &clientEvent); err != nil {
			enqueueEvent(ctx, writeCh, protocol.NewErrorEvent(sessionID, fmt.Sprintf("invalid json: %v", err), ""))
			continue
		}

		switch clientEvent.Action {
		case "exec":
			var reqEvent protocol.ExecRequestEvent
			if err := json.Unmarshal(payload, &reqEvent); err != nil {
				enqueueEvent(ctx, writeCh, protocol.NewErrorEvent(sessionID, fmt.Sprintf("invalid exec request: %v", err), ""))
				continue
			}
			if strings.TrimSpace(reqEvent.Command) == "" {
				enqueueEvent(ctx, writeCh, protocol.NewErrorEvent(sessionID, "cmd is required", ""))
				continue
			}

			mode, err := parseMode(reqEvent.Mode)
			if err != nil {
				enqueueEvent(ctx, writeCh, protocol.NewErrorEvent(sessionID, err.Error(), ""))
				continue
			}

			runnerMu.Lock()
			if activeRunner != nil {
				runnerMu.Unlock()
				enqueueEvent(ctx, writeCh, protocol.NewErrorEvent(sessionID, "another command is already running", ""))
				continue
			}

			currentRunner := h.newRunner(mode)
			activeRunner = currentRunner
			activeSessionID = sessionID
			runnerMu.Unlock()

			for _, event := range controller.OnExecStart(reqEvent.Command) {
				enqueueEvent(ctx, writeCh, event)
			}

			go func(req protocol.ExecRequestEvent, selected runner.Runner, selectedMode runner.Mode) {
				err := selected.Run(ctx, runner.ExecRequest{
					SessionID: sessionID,
					Command:   req.Command,
					CWD:       req.CWD,
					Mode:      selectedMode,
				}, func(event any) {
					enqueueEvent(ctx, writeCh, event)
					for _, mapped := range controller.OnRunnerEvent(event) {
						enqueueEvent(ctx, writeCh, mapped)
					}
				})
				if err != nil {
					log.Printf("runner finished with error: %v", err)
				}

				runnerMu.Lock()
				if activeRunner == selected {
					activeRunner = nil
					activeSessionID = ""
				}
				runnerMu.Unlock()

				for _, event := range controller.OnCommandFinished() {
					enqueueEvent(ctx, writeCh, event)
				}
			}(reqEvent, currentRunner, mode)
		case "input":
			var inputEvent protocol.InputRequestEvent
			if err := json.Unmarshal(payload, &inputEvent); err != nil {
				enqueueEvent(ctx, writeCh, protocol.NewErrorEvent(sessionID, fmt.Sprintf("invalid input request: %v", err), ""))
				continue
			}
			if inputEvent.Data == "" {
				enqueueEvent(ctx, writeCh, protocol.NewErrorEvent(sessionID, "input data is required", ""))
				continue
			}

			runnerMu.Lock()
			currentRunner := activeRunner
			currentSessionID := activeSessionID
			runnerMu.Unlock()

			if currentRunner == nil || currentSessionID == "" {
				enqueueEvent(ctx, writeCh, protocol.NewErrorEvent(sessionID, "no active runner", ""))
				continue
			}

			if err := currentRunner.Write(ctx, []byte(inputEvent.Data)); err != nil {
				message := err.Error()
				if errors.Is(err, runner.ErrInputNotSupported) {
					message = "input is only supported for pty sessions"
				}
				enqueueEvent(ctx, writeCh, protocol.NewErrorEvent(sessionID, message, ""))
			} else {
				for _, event := range controller.OnInputSent() {
					enqueueEvent(ctx, writeCh, event)
				}
			}
		case "fs_list":
			var fsListReq protocol.FSListRequestEvent
			if err := json.Unmarshal(payload, &fsListReq); err != nil {
				enqueueEvent(ctx, writeCh, protocol.NewErrorEvent(sessionID, fmt.Sprintf("invalid fs_list request: %v", err), ""))
				continue
			}

			result, err := listDirectory(sessionID, fsListReq.Path)
			if err != nil {
				enqueueEvent(ctx, writeCh, protocol.NewErrorEvent(sessionID, fmt.Sprintf("list directory: %v", err), ""))
				continue
			}
			enqueueEvent(ctx, writeCh, result)
		default:
			enqueueEvent(ctx, writeCh, protocol.NewErrorEvent(sessionID, fmt.Sprintf("unknown action: %s", clientEvent.Action), ""))
		}
	}
}

func (h *Handler) newRunner(mode runner.Mode) runner.Runner {
	switch mode {
	case runner.ModePTY:
		if h.NewPtyRunner != nil {
			return h.NewPtyRunner()
		}
	default:
		if h.NewExecRunner != nil {
			return h.NewExecRunner()
		}
	}
	return runner.NewExecRunner()
}

func parseMode(raw string) (runner.Mode, error) {
	mode := runner.Mode(strings.TrimSpace(raw))
	if mode == "" {
		return runner.ModeExec, nil
	}
	switch mode {
	case runner.ModeExec, runner.ModePTY:
		return mode, nil
	default:
		return "", fmt.Errorf("unknown mode: %s", raw)
	}
}

func listDirectory(sessionID, rawPath string) (protocol.FSListResultEvent, error) {
	target := strings.TrimSpace(rawPath)
	if target == "" {
		target = "."
	}

	cleanTarget := filepath.Clean(target)
	absPath, err := filepath.Abs(cleanTarget)
	if err != nil {
		return protocol.FSListResultEvent{}, err
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return protocol.FSListResultEvent{}, err
	}

	items := make([]protocol.FSItem, 0, len(entries))
	for _, entry := range entries {
		item := protocol.FSItem{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
		}
		if info, err := entry.Info(); err == nil {
			item.Size = info.Size()
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	return protocol.NewFSListResultEvent(sessionID, absPath, items), nil
}

func enqueueEvent(ctx context.Context, writeCh chan<- any, event any) {
	select {
	case <-ctx.Done():
		return
	default:
	}

	select {
	case <-ctx.Done():
		return
	case writeCh <- event:
	}
}
