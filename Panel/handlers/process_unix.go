//go:build !windows

package handlers

import (
	"errors"
	"syscall"
	"time"
)

func timeNowUnix() int64 { return time.Now().Unix() }

func sleepShort() { time.Sleep(100 * time.Millisecond) }

// processAlive reports whether pid refers to a live process. Signal 0 performs
// the permission and existence checks without delivering anything.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// killProcess terminates the process, escalating to SIGKILL after a grace
// period so xmrig-proxy can flush its state.
func killProcess(pid int) error {
	if pid <= 0 {
		return errors.New("invalid pid")
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}

	deadline := timeNowUnix() + int64(proxyStopTimeout.Seconds())
	for timeNowUnix() < deadline {
		if !processAlive(pid) {
			return nil
		}
		sleepShort()
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
