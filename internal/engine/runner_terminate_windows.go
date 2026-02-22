//go:build windows

package engine

import "os/exec"

func configureCommandForTermination(cmd *exec.Cmd) {}

func terminateCommand(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
