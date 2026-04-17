package bypass

import (
	"context"
	"net"
)

type Strategy interface {
	Name() string
	Apply(ctx context.Context, clientConn net.Conn, serverConn *net.TCPConn, fakeSNI string, firstData []byte) bool
}
