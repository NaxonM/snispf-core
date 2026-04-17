//go:build windows

package bypass

import (
	"errors"
	"net"
)

func setConnTTL(_ *net.TCPConn, _ int) error {
	return errors.New("ttl trick not available on this build")
}

func getConnTTL(_ *net.TCPConn) (int, error) {
	return 0, errors.New("ttl trick not available on this build")
}
