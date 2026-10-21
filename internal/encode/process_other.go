//go:build !darwin && !linux

package encode

import (
	"os"
	"os/exec"
)

func isolateInterrupt(*exec.Cmd) {}

func interruptFFmpeg(cmd *exec.Cmd) error {
	return cmd.Process.Signal(os.Interrupt)
}
