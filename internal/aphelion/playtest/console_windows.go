package playtest

import (
	"os/exec"
	"syscall"
)

// hideConsole keeps dm.exe from flashing a console window. DreamDaemon and
// DreamSeeker are GUI programs and unaffected.
func hideConsole(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
