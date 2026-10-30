//go:build !windows

package main

import (
	"errors"
	"os"
	"syscall"
)

func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

func terminateProcess(process *os.Process) error {
	if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// notifyStopRequests is a no-op on Unix; `commentloop stop` sends SIGTERM.
func notifyStopRequests(chan<- os.Signal) {}
