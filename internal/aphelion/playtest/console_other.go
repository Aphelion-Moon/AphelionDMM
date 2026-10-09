//go:build !windows

package playtest

import "os/exec"

func hideConsole(*exec.Cmd) {}
