//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
	winEpochOffset100ns            = 116444736000000000
)

type filetime struct {
	LowDateTime  uint32
	HighDateTime uint32
}

var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess     = kernel32.NewProc("OpenProcess")
	procCloseHandle     = kernel32.NewProc("CloseHandle")
	procGetExitCodeProc = kernel32.NewProc("GetExitCodeProcess")
	procGetProcessTimes = kernel32.NewProc("GetProcessTimes")
)

func parentProcessAlive(pid int, expectedStartUnixMS int64) bool {
	if pid <= 0 {
		return true
	}
	h, _, _ := procOpenProcess.Call(uintptr(processQueryLimitedInformation), 0, uintptr(uint32(pid)))
	if h == 0 {
		return false
	}
	defer procCloseHandle.Call(h)

	var code uint32
	r1, _, _ := procGetExitCodeProc.Call(h, uintptr(unsafe.Pointer(&code)))
	if r1 == 0 {
		return false
	}
	if code != stillActive {
		return false
	}

	if expectedStartUnixMS <= 0 {
		return true
	}

	actualStartUnixMS, ok := processStartUnixMS(h)
	if !ok {
		return false
	}

	delta := actualStartUnixMS - expectedStartUnixMS
	if delta < 0 {
		delta = -delta
	}
	return delta <= 2000
}

func processStartUnixMS(handle uintptr) (int64, bool) {
	var creation filetime
	var exit filetime
	var kernel filetime
	var user filetime

	r1, _, _ := procGetProcessTimes.Call(
		handle,
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r1 == 0 {
		return 0, false
	}

	ticks := (uint64(creation.HighDateTime) << 32) | uint64(creation.LowDateTime)
	if ticks <= winEpochOffset100ns {
		return 0, false
	}
	unix100ns := int64(ticks - winEpochOffset100ns)
	return unix100ns / 10000, true
}
