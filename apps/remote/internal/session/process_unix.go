//go:build !windows

package session

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"mobilevc/internal/protocol"
)

func listAllProcesses(ctx context.Context) (map[int]protocol.RuntimeProcessItem, map[int][]int, error) {
	cmd := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,stat=,etime=,command=")
	output, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("list processes: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	all := make(map[int]protocol.RuntimeProcessItem, len(lines))
	children := make(map[int][]int, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		item := protocol.RuntimeProcessItem{
			PID:     pid,
			PPID:    ppid,
			State:   fields[2],
			Elapsed: fields[3],
			Command: strings.Join(fields[4:], " "),
		}
		all[pid] = item
		children[ppid] = append(children[ppid], pid)
	}
	return all, children, nil
}
