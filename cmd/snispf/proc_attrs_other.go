//go:build !windows

package main

import "os/exec"

func setHiddenProcessAttrs(cmd *exec.Cmd) {
	_ = cmd
}
