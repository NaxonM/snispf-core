package rawinjector

import "time"

type ConfirmationStatus string

const (
	ConfirmationStatusConfirmed     ConfirmationStatus = "confirmed"
	ConfirmationStatusFailed        ConfirmationStatus = "failed"
	ConfirmationStatusTimeout       ConfirmationStatus = "timeout"
	ConfirmationStatusNotRegistered ConfirmationStatus = "not_registered"
)

type DetailedWaiter interface {
	WaitForConfirmationDetailed(localPort int, timeout time.Duration) ConfirmationStatus
}

type PortStateDebugger interface {
	DebugPortState(localPort int) string
}

type Interface interface {
	Start() bool
	Stop()
	RegisterPort(localPort int, fakeHello []byte)
	WaitForConfirmation(localPort int, timeout time.Duration) bool
	CleanupPort(localPort int)
}
