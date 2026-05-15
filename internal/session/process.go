package session

import (
	"context"
	"sort"
	"strings"

	"mobilevc/internal/engine"
	"mobilevc/internal/protocol"
)

func (s *Service) ActiveProcessTree(ctx context.Context) (int, []protocol.RuntimeProcessItem, error) {
	if s == nil || s.manager == nil {
		return 0, nil, nil
	}
	activeRunner, meta, _ := s.manager.current()
	if activeRunner == nil {
		return 0, []protocol.RuntimeProcessItem{}, nil
	}
	provider, ok := activeRunner.(engine.ProcessProvider)
	if !ok {
		return 0, []protocol.RuntimeProcessItem{}, nil
	}
	ref := provider.ProcessRef()
	if ref.RootPID <= 0 {
		return 0, []protocol.RuntimeProcessItem{}, nil
	}
	if strings.TrimSpace(ref.ExecutionID) == "" {
		ref.ExecutionID = strings.TrimSpace(meta.ExecutionID)
	}
	if strings.TrimSpace(ref.Command) == "" {
		ref.Command = strings.TrimSpace(meta.Command)
	}
	if strings.TrimSpace(ref.CWD) == "" {
		ref.CWD = strings.TrimSpace(meta.CWD)
	}
	if strings.TrimSpace(ref.Source) == "" {
		ref.Source = strings.TrimSpace(meta.Engine)
	}
	items, err := snapshotProcessTree(ctx, ref)
	if err != nil {
		return ref.RootPID, nil, err
	}
	return ref.RootPID, items, nil
}

func snapshotProcessTree(ctx context.Context, ref engine.ProcessRef) ([]protocol.RuntimeProcessItem, error) {
	all, children, err := listAllProcesses(ctx)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return []protocol.RuntimeProcessItem{}, nil
	}

	root, ok := all[ref.RootPID]
	if !ok {
		return []protocol.RuntimeProcessItem{}, nil
	}
	root.Root = true
	root.ExecutionID = strings.TrimSpace(ref.ExecutionID)
	root.CWD = strings.TrimSpace(ref.CWD)
	root.Source = strings.TrimSpace(ref.Source)
	root.LogAvailable = true
	all[ref.RootPID] = root

	visited := map[int]struct{}{}
	ordered := make([]protocol.RuntimeProcessItem, 0, len(all))
	var walk func(pid int)
	walk = func(pid int) {
		if _, ok := visited[pid]; ok {
			return
		}
		visited[pid] = struct{}{}
		item, ok := all[pid]
		if !ok {
			return
		}
		if executionID := strings.TrimSpace(ref.ExecutionID); executionID != "" {
			item.ExecutionID = executionID
			item.LogAvailable = true
		}
		if cwd := strings.TrimSpace(ref.CWD); cwd != "" {
			item.CWD = cwd
		}
		if source := strings.TrimSpace(ref.Source); source != "" {
			item.Source = source
		}
		if pid == ref.RootPID {
			item.Root = true
		}
		ordered = append(ordered, item)
		kids := append([]int(nil), children[pid]...)
		sort.Ints(kids)
		for _, childPID := range kids {
			walk(childPID)
		}
	}
	walk(ref.RootPID)
	return ordered, nil
}
