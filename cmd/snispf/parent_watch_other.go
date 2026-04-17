//go:build !windows

package main

func parentProcessAlive(pid int, expectedStartUnixMS int64) bool {
	_ = pid
	_ = expectedStartUnixMS
	return true
}
