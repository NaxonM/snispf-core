//go:build !linux

package utils

func hasAFPacketSupport() bool {
	return false
}
