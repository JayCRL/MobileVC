//go:build !windows

package engine

import (
	"os/exec"
	"syscall"
)

func hideCommandWindow(cmd *exec.Cmd) {
}

func isolateCommandProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killCommandProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if pid > 0 {
		pgid, err := syscall.Getpgid(pid)
		if err == nil && pgid == pid && syscall.Kill(-pid, syscall.SIGKILL) == nil {
			return
		}
	}
	_ = cmd.Process.Kill()
}
