//go:build linux

package utils

import "syscall"

func hasAFPacketSupport() bool {
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(htons16(0x0003)))
	if err != nil {
		return false
	}
	_ = syscall.Close(fd)
	return true
}

func htons16(v uint16) uint16 {
	return (v<<8)&0xff00 | (v>>8)&0x00ff
}
