//go:build !windows

package bypass

import (
	"net"
	"syscall"
)

func setConnTTL(conn *net.TCPConn, ttl int) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var setErr error
	err = raw.Control(func(fd uintptr) {
		setErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_TTL, ttl)
	})
	if err != nil {
		return err
	}
	return setErr
}

func getConnTTL(conn *net.TCPConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var value int
	var getErr error
	err = raw.Control(func(fd uintptr) {
		value, getErr = syscall.GetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_TTL)
	})
	if err != nil {
		return 0, err
	}
	if getErr != nil {
		return 0, getErr
	}
	return value, nil
}
