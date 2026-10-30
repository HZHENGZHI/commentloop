//go:build windows

package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
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

func stopEventName(pid int) string {
	return fmt.Sprintf(`Local\commentloop-stop-%d`, pid)
}

// Windows has no SIGTERM for arbitrary processes, and a CREATE_NO_WINDOW child
// has no console to receive Ctrl+Break, so the server listens on a named event
// instead (see notifyStopRequests). Kill is only the fallback for servers that
// do not expose the event.
func terminateProcess(process *os.Process) error {
	name, err := windows.UTF16PtrFromString(stopEventName(process.Pid))
	if err == nil {
		if h, openErr := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name); openErr == nil {
			defer windows.CloseHandle(h)
			if windows.SetEvent(h) == nil {
				return nil
			}
		}
	}
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

// notifyStopRequests delivers os.Interrupt to stop when `commentloop stop`
// signals this process's named stop event.
func notifyStopRequests(stop chan<- os.Signal) {
	name, err := windows.UTF16PtrFromString(stopEventName(os.Getpid()))
	if err != nil {
		return
	}
	h, err := windows.CreateEvent(nil, 1, 0, name)
	if err != nil {
		slog.Warn("stop event unavailable", "err", err)
		return
	}
	go func() {
		defer windows.CloseHandle(h)
		if ev, _ := windows.WaitForSingleObject(h, windows.INFINITE); ev == windows.WAIT_OBJECT_0 {
			select {
			case stop <- os.Interrupt:
			default:
			}
		}
	}()
}
