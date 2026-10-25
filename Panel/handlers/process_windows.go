//go:build windows

package handlers

import (
	"errors"
	"syscall"
)

const stillActive = 259 // STILL_ACTIVE exit code

// processAlive reports whether pid refers to a live process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)

	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}

// killProcess terminates the process unconditionally (Windows has no SIGTERM).
func killProcess(pid int) error {
	if pid <= 0 {
		return errors.New("invalid pid")
	}
	handle, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
			return errors.New("access denied: the panel needs administrator rights to stop this process")
		}
		return err
	}
	defer syscall.CloseHandle(handle)

	if err := syscall.TerminateProcess(handle, 1); err != nil {
		return err
	}
	return nil
}
