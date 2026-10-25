//go:build !windows

package session

import "os/exec"

func hideCommandWindow(cmd *exec.Cmd) {}
