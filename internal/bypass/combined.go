package bypass

import (
	"context"
	"net"
	"time"

	"snispf/internal/logx"
	"snispf/internal/rawinjector"
	"snispf/internal/tlsclienthello"
)

type Combined struct {
	strategy string
	delay    time.Duration
	useTTL   bool
	confirm  time.Duration
	injector rawinjector.Interface
}

func NewCombined(strategy string, delaySec float64, useTTL bool, confirmTimeout time.Duration, injector rawinjector.Interface) *Combined {
	if confirmTimeout <= 0 {
		confirmTimeout = 2 * time.Second
	}
	return &Combined{strategy: strategy, delay: time.Duration(delaySec * float64(time.Second)), useTTL: useTTL, confirm: confirmTimeout, injector: injector}
}

func (c *Combined) Name() string { return "combined" }

func (c *Combined) Apply(_ context.Context, _ net.Conn, serverConn *net.TCPConn, fakeSNI string, firstData []byte) bool {
	if c.injector != nil {
		if !c.injector.WaitForConfirmation(serverConn.LocalAddr().(*net.TCPAddr).Port, c.confirm) {
			logx.Warnf("combined: no raw confirmation before timeout, continuing")
		}
	} else if c.useTTL {
		fakeHello := tlsclienthello.BuildClientHello(fakeSNI)
		originalTTL, ttlErr := getConnTTL(serverConn)
		if ttlErr == nil {
			if err := setConnTTL(serverConn, 3); err == nil {
				_, _ = serverConn.Write(fakeHello)
				time.Sleep(50 * time.Millisecond)
				_ = setConnTTL(serverConn, originalTTL)
			} else {
				_, _ = serverConn.Write(fakeHello)
			}
		} else {
			_, _ = serverConn.Write(fakeHello)
		}
		time.Sleep(1 * time.Millisecond)
	}

	_ = serverConn.SetNoDelay(true)
	defer serverConn.SetNoDelay(false)
	frags := tlsclienthello.FragmentClientHello(firstData, c.strategy)
	for i, frag := range frags {
		if _, err := serverConn.Write(frag); err != nil {
			return false
		}
		if i < len(frags)-1 && c.delay > 0 {
			time.Sleep(c.delay)
		}
	}
	return true
}
