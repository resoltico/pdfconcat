// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"errors"
	"fmt"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	consoleDeliveryGrace = 200 * time.Millisecond
)

var (
	errConsoleArguments = errors.New("interrupt-console needs one valid child PID")
	errConsoleIsolation = errors.New("console must contain exactly the target and this sender")
)

// controlCommand sends a native event only to the fixture's private console.
func controlCommand(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "interrupt-console" {
		return false, nil
	}

	if len(args) != 2 {
		return true, errConsoleArguments
	}

	pid, err := strconv.ParseUint(args[1], 10, 32)
	if err != nil || pid == 0 {
		return true, fmt.Errorf("%w: invalid PID %q", errConsoleArguments, args[1])
	}

	if err = callConsole("FreeConsole"); err != nil && !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
		return true, err
	}

	if attachErr := callConsole("AttachConsole", uintptr(pid)); attachErr != nil {
		return true, attachErr
	}

	if isolationErr := verifyPrivateConsole(uint32(pid)); isolationErr != nil {
		return true, isolationErr
	}
	// Attaching resets handlers. Ignore BREAK in this sender while the target's Go handler receives it.
	callback := syscall.NewCallback(func(uint32) uintptr { return 1 })
	if handlerErr := callConsole("SetConsoleCtrlHandler", callback, 1); handlerErr != nil {
		return true, handlerErr
	}

	if err = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, 0); err != nil {
		return true, fmt.Errorf("dispatch CTRL_BREAK: %w", err)
	}
	// Dispatch is asynchronous; keep the console attachment alive for delivery before the parent waits for target exit.
	time.Sleep(consoleDeliveryGrace)

	return true, nil
}

func callConsole(name string, args ...uintptr) error {
	procedure := windows.NewLazySystemDLL("kernel32.dll").NewProc(name)

	succeeded, _, err := procedure.Call(args...)
	if succeeded == 0 {
		return fmt.Errorf("%s: %w", name, err)
	}

	return nil
}

// verifyPrivateConsole refuses to broadcast into a parent or user console with any other participant.
func verifyPrivateConsole(target uint32) error {
	var processes [privateConsoleProcesses]uint32

	procedure := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")

	count, _, callErr := procedure.Call(uintptr(unsafe.Pointer(&processes[0])), uintptr(len(processes)))
	if count == 0 {
		return fmt.Errorf("GetConsoleProcessList: %w", callErr)
	}

	if !privateConsoleMembership(processes[:], count, target, windows.GetCurrentProcessId()) {
		return errConsoleIsolation
	}

	return nil
}
