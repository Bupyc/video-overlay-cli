//go:build darwin || linux

package encode

import (
	"os"
	"os/exec"
	"syscall"
)

func isolateInterrupt(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func interruptFFmpeg(cmd *exec.Cmd) error {
	return cmd.Process.Signal(os.Interrupt)
}
