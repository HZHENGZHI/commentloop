//go:build windows

package main

import (
	"errors"
	"os"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
)

// CREATE_NO_WINDOW gives the daemon a hidden console that child processes
// (git, agent CLIs) inherit, so they do not flash new console windows.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup | createNoWindow,
		HideWindow:    true,
	}
}

// Windows has no SIGTERM for arbitrary processes; terminate directly.
func terminateProcess(process *os.Process) error {
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
