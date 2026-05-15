//go:build windows

package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"mobilevc/internal/protocol"
)

// winProcessScript 用 PowerShell + Win32_Process 列出进程，每行输出一个紧凑 JSON 对象。
// 时间戳转成 Unix 秒避免 PS 版本间日期序列化差异；强制 UTF-8 stdout 防止 CommandLine 中文乱码。
const winProcessScript = `[Console]::OutputEncoding=[Text.Encoding]::UTF8;$ErrorActionPreference='Stop';$e=[DateTime]'1970-01-01';Get-CimInstance Win32_Process|ForEach-Object{$c=0;if($_.CreationDate){$c=[int64](($_.CreationDate.ToUniversalTime()-$e).TotalSeconds)};@{pid=[int]$_.ProcessId;ppid=[int]$_.ParentProcessId;created=$c;command=$_.CommandLine;name=$_.Name}|ConvertTo-Json -Compress}`

type winProcessRecord struct {
	PID     int    `json:"pid"`
	PPID    int    `json:"ppid"`
	Created int64  `json:"created"`
	Command string `json:"command"`
	Name    string `json:"name"`
}

func listAllProcesses(ctx context.Context) (map[int]protocol.RuntimeProcessItem, map[int][]int, error) {
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", winProcessScript)
	output, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("list processes: %w", err)
	}
	now := time.Now().Unix()
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	all := make(map[int]protocol.RuntimeProcessItem, len(lines))
	children := make(map[int][]int, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(strings.TrimSpace(line), "\r")
		if line == "" {
			continue
		}
		var rec winProcessRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.PID <= 0 {
			continue
		}
		command := strings.TrimSpace(rec.Command)
		if command == "" {
			command = rec.Name
		}
		elapsed := ""
		if rec.Created > 0 && now > rec.Created {
			elapsed = formatWindowsElapsed(now - rec.Created)
		}
		item := protocol.RuntimeProcessItem{
			PID:     rec.PID,
			PPID:    rec.PPID,
			Elapsed: elapsed,
			Command: command,
		}
		all[rec.PID] = item
		children[rec.PPID] = append(children[rec.PPID], rec.PID)
	}
	return all, children, nil
}

func formatWindowsElapsed(seconds int64) string {
	if seconds < 0 {
		return ""
	}
	days := seconds / 86400
	seconds -= days * 86400
	hours := seconds / 3600
	seconds -= hours * 3600
	minutes := seconds / 60
	seconds -= minutes * 60
	if days > 0 {
		return fmt.Sprintf("%d-%02d:%02d:%02d", days, hours, minutes, seconds)
	}
	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}
