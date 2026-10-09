//go:build !windows

package repoinfo

import "os/exec"

func configureProcess(*exec.Cmd) {}
