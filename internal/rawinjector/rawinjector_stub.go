//go:build !linux && !windows

package rawinjector

import "time"

type stub struct{}

func New(_ string, _ string, _ int, _ func(string) []byte) Interface { return &stub{} }

func IsRawAvailable() bool { return false }

func (s *stub) Start() bool { return false }
func (s *stub) Stop()       {}
func (s *stub) RegisterPort(_ int, _ []byte) {
}
func (s *stub) WaitForConfirmation(_ int, _ time.Duration) bool { return false }
func (s *stub) WaitForConfirmationDetailed(_ int, _ time.Duration) ConfirmationStatus {
	return ConfirmationStatusNotRegistered
}
func (s *stub) CleanupPort(_ int) {}
