package bypass

import (
	"context"
	"net"
	"strings"
	"time"

	"snispf/internal/logx"
	"snispf/internal/rawinjector"
	"snispf/internal/tlsclienthello"
)

type FakeSNI struct {
	method   string
	delay    time.Duration
	confirm  time.Duration
	injector rawinjector.Interface
}

func NewFakeSNI(method string, delaySec float64, confirmTimeout time.Duration, injector rawinjector.Interface) *FakeSNI {
	if confirmTimeout <= 0 {
		confirmTimeout = 2 * time.Second
	}
	return &FakeSNI{method: method, delay: time.Duration(delaySec * float64(time.Second)), confirm: confirmTimeout, injector: injector}
}

func (f *FakeSNI) Name() string { return "fake_sni" }

func (f *FakeSNI) Apply(_ context.Context, _ net.Conn, serverConn *net.TCPConn, fakeSNI string, firstData []byte) bool {
	if f.injector != nil {
		if !f.injector.WaitForConfirmation(serverConn.LocalAddr().(*net.TCPAddr).Port, f.confirm) {
			logx.Warnf("fake_sni: no raw confirmation before timeout, sending real data")
		}
		_, err := serverConn.Write(firstData)
		return err == nil
	}

	method := strings.ToLower(strings.TrimSpace(f.method))
	if method == "ttl_trick" {
		return f.applyTTLTrick(serverConn, fakeSNI, firstData)
	}

	// prefix_fake/disorder/fragment_fallback all fall back to fragmentation
	// on non-raw paths to avoid corrupting the TLS stream.
	_ = fakeSNI
	_ = serverConn.SetNoDelay(true)
	defer serverConn.SetNoDelay(false)
	frags := tlsclienthello.FragmentClientHello(firstData, "sni_split")
	for i, frag := range frags {
		if _, err := serverConn.Write(frag); err != nil {
			return false
		}
		if i < len(frags)-1 {
			time.Sleep(maxDuration(f.delay, 100*time.Millisecond))
		}
	}
	return true
}

func (f *FakeSNI) applyTTLTrick(serverConn *net.TCPConn, fakeSNI string, firstData []byte) bool {
	_ = serverConn.SetNoDelay(true)
	defer serverConn.SetNoDelay(false)

	fakeHello := tlsclienthello.BuildClientHello(fakeSNI)
	originalTTL, ttlErr := getConnTTL(serverConn)
	if ttlErr == nil {
		if err := setConnTTL(serverConn, 3); err != nil {
			return false
		}
	}

	_, _ = serverConn.Write(fakeHello)
	time.Sleep(50 * time.Millisecond)

	if ttlErr == nil {
		_ = setConnTTL(serverConn, originalTTL)
	}

	_, err := serverConn.Write(firstData)
	return err == nil
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
