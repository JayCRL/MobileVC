package runtime

import (
	"context"
	"fmt"
	"strings"

	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
)

func ParseMode(raw string) (runner.Mode, error) {
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

func Enqueue(ctx context.Context, writeCh chan<- any, event any) {
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

func EmitWithMeta(emit func(any), meta protocol.RuntimeMeta, event any) {
	if emit == nil {
		return
	}
	emit(protocol.ApplyRuntimeMeta(event, meta))
}
