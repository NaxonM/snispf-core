package rawinjector

import "sync"

var (
	diagMu      sync.RWMutex
	rawDiagText string
)

func setRawDiagnostic(text string) {
	diagMu.Lock()
	rawDiagText = text
	diagMu.Unlock()
}

func RawDiagnostic() string {
	diagMu.RLock()
	defer diagMu.RUnlock()
	return rawDiagText
}
