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
	runtimepkg "mobilevc/internal/runtime"
	"mobilevc/internal/session"
	"mobilevc/internal/skills"
)

type Handler struct {
	AuthToken     string
	NewExecRunner func() runner.Runner
	NewPtyRunner  func() runner.Runner
	Upgrader      websocket.Upgrader
	SkillLauncher *skills.Launcher
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
		SkillLauncher: skills.NewLauncher(),
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
	runtimeSvc := runtimepkg.NewService(sessionID, runtimepkg.Dependencies{
		NewExecRunner: h.NewExecRunner,
		NewPtyRunner:  h.NewPtyRunner,
	})
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

	emit := func(event any) {
		runtimepkg.Enqueue(ctx, writeCh, event)
	}

	defer func() {
		cancel()
		runtimeSvc.Cleanup()
		writerWG.Wait()
	}()

	emit(protocol.NewSessionStateEvent(sessionID, string(session.StateActive), "connected"))
	emit(runtimeSvc.InitialEvent())

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
			emit(protocol.NewErrorEvent(sessionID, "only text messages are supported", ""))
			continue
		}

		var clientEvent protocol.ClientEvent
		if err := json.Unmarshal(payload, &clientEvent); err != nil {
			emit(protocol.NewErrorEvent(sessionID, fmt.Sprintf("invalid json: %v", err), ""))
			continue
		}

		switch clientEvent.Action {
		case "exec":
			var reqEvent protocol.ExecRequestEvent
			if err := json.Unmarshal(payload, &reqEvent); err != nil {
				emit(protocol.NewErrorEvent(sessionID, fmt.Sprintf("invalid exec request: %v", err), ""))
				continue
			}
			if strings.TrimSpace(reqEvent.Command) == "" {
				emit(protocol.NewErrorEvent(sessionID, "cmd is required", ""))
				continue
			}
			mode, err := runtimepkg.ParseMode(reqEvent.Mode)
			if err != nil {
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
				continue
			}
			err = runtimeSvc.Execute(ctx, sessionID, runtimepkg.ExecuteRequest{
				Command: reqEvent.Command,
				CWD:     reqEvent.CWD,
				Mode:    mode,
				RuntimeMeta: protocol.RuntimeMeta{
					Source:       fallback(reqEvent.Source, "command"),
					SkillName:    reqEvent.SkillName,
					Target:       reqEvent.Target,
					TargetType:   reqEvent.TargetType,
					TargetPath:   reqEvent.TargetPath,
					ResultView:   reqEvent.ResultView,
					ContextID:    reqEvent.ContextID,
					ContextTitle: reqEvent.ContextTitle,
					TargetText:   reqEvent.TargetText,
				},
			}, emit)
			if err != nil {
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
			}
		case "input":
			var inputEvent protocol.InputRequestEvent
			if err := json.Unmarshal(payload, &inputEvent); err != nil {
				emit(protocol.NewErrorEvent(sessionID, fmt.Sprintf("invalid input request: %v", err), ""))
				continue
			}
			if inputEvent.Data == "" {
				emit(protocol.NewErrorEvent(sessionID, "input data is required", ""))
				continue
			}
			if err := runtimeSvc.SendInput(ctx, sessionID, runtimepkg.InputRequest{Data: inputEvent.Data}, emit); err != nil {
				message := err.Error()
				if errors.Is(err, runner.ErrInputNotSupported) {
					message = "input is only supported for pty sessions"
				}
				emit(protocol.NewErrorEvent(sessionID, message, ""))
			}
		case "skill_exec":
			var skillEvent protocol.SkillRequestEvent
			if err := json.Unmarshal(payload, &skillEvent); err != nil {
				emit(protocol.NewErrorEvent(sessionID, fmt.Sprintf("invalid skill request: %v", err), ""))
				continue
			}
			if h.SkillLauncher == nil {
				emit(protocol.NewErrorEvent(sessionID, "skill launcher is unavailable", ""))
				continue
			}
			execReq, err := h.SkillLauncher.BuildRequest(
				skillEvent.Name,
				fallback(skillEvent.CWD, "."),
				skillEvent.TargetType,
				skillEvent.TargetPath,
				skillEvent.TargetTitle,
				skillEvent.TargetDiff,
				skillEvent.ContextID,
				skillEvent.ContextTitle,
				skillEvent.TargetText,
				skillEvent.TargetStack,
			)
			if err != nil {
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
				continue
			}
			if err := runtimeSvc.Execute(ctx, sessionID, execReq, emit); err != nil {
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
			}
		case "fs_list":
			var fsListReq protocol.FSListRequestEvent
			if err := json.Unmarshal(payload, &fsListReq); err != nil {
				emit(protocol.NewErrorEvent(sessionID, fmt.Sprintf("invalid fs_list request: %v", err), ""))
				continue
			}
			result, err := listDirectory(sessionID, fsListReq.Path)
			if err != nil {
				emit(protocol.NewErrorEvent(sessionID, fmt.Sprintf("list directory: %v", err), ""))
				continue
			}
			emit(result)
		default:
			emit(protocol.NewErrorEvent(sessionID, fmt.Sprintf("unknown action: %s", clientEvent.Action), ""))
		}
	}
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func parseMode(raw string) (runner.Mode, error) {
	return runtimepkg.ParseMode(raw)
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
