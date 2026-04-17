package bypass

import (
	"context"
	"net"
	"time"

	"snispf/internal/logx"
	"snispf/internal/rawinjector"
)

// WrongSeqStrict reproduces the legacy wrong-seq flow:
// wait for raw sniffer/injector confirmation before allowing relay.
type WrongSeqStrict struct {
	injector rawinjector.Interface
	timeout  time.Duration
}

func NewWrongSeqStrict(injector rawinjector.Interface, timeout time.Duration) *WrongSeqStrict {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &WrongSeqStrict{injector: injector, timeout: timeout}
}

func (w *WrongSeqStrict) Name() string { return "wrong_seq" }

func (w *WrongSeqStrict) Apply(_ context.Context, _ net.Conn, serverConn *net.TCPConn, _ string, firstData []byte) bool {
	if w.injector == nil {
		logx.Warnf("wrong_seq: raw injector not available")
		return false
	}

	localPort := serverConn.LocalAddr().(*net.TCPAddr).Port
	if detailed, ok := w.injector.(rawinjector.DetailedWaiter); ok {
		status := detailed.WaitForConfirmationDetailed(localPort, w.timeout)
		if status != rawinjector.ConfirmationStatusConfirmed {
			if dbg, ok := w.injector.(rawinjector.PortStateDebugger); ok {
				logx.Warnf("wrong_seq: confirmation failed on local_port=%d status=%s timeout_ms=%d %s", localPort, status, w.timeout.Milliseconds(), dbg.DebugPortState(localPort))
			} else {
				logx.Warnf("wrong_seq: confirmation failed on local_port=%d status=%s timeout_ms=%d", localPort, status, w.timeout.Milliseconds())
			}
			return false
		}
		logx.Debugf("wrong_seq: confirmation succeeded on local_port=%d status=%s timeout_ms=%d", localPort, status, w.timeout.Milliseconds())
	} else {
		if !w.injector.WaitForConfirmation(localPort, w.timeout) {
			logx.Warnf("wrong_seq: confirmation timeout on local_port=%d timeout_ms=%d", localPort, w.timeout.Milliseconds())
			return false
		}
		logx.Debugf("wrong_seq: confirmation succeeded on local_port=%d timeout_ms=%d", localPort, w.timeout.Milliseconds())
	}

	_, err := serverConn.Write(firstData)
	if err != nil {
		logx.Errorf("wrong_seq: first payload write failed on local_port=%d err=%v", localPort, err)
		return false
	}
	return true
}
