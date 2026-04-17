package bypass

import (
	"context"
	"net"
	"time"

	"snispf/internal/tlsclienthello"
)

type Fragment struct {
	strategy string
	delay    time.Duration
}

func NewFragment(strategy string, delaySec float64) *Fragment {
	return &Fragment{strategy: strategy, delay: time.Duration(delaySec * float64(time.Second))}
}

func (f *Fragment) Name() string { return "fragment" }

func (f *Fragment) Apply(_ context.Context, _ net.Conn, serverConn *net.TCPConn, _ string, firstData []byte) bool {
	_ = serverConn.SetNoDelay(true)
	defer serverConn.SetNoDelay(false)
	frags := tlsclienthello.FragmentClientHello(firstData, f.strategy)
	for i, frag := range frags {
		if _, err := serverConn.Write(frag); err != nil {
			return false
		}
		if i < len(frags)-1 && f.delay > 0 {
			time.Sleep(f.delay)
		}
	}
	return true
}
