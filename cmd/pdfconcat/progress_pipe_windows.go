// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

type progressPipeDevice struct {
	deviceType      uint32
	characteristics uint32
}

const (
	progressPipeDeviceClass   = 4
	progressPipeDeviceBytes   = 8
	progressNamedPipeDevice   = 0x11
	progressRemoteDevice      = 0x10
	progressIOStatusBytes     = 16
	progressStatusCountOffset = 8
)

func qualifyProgressPipe(handle windows.Handle, volumeQuery *windows.LazyProc) error {
	var flags uint32
	if err := windows.GetNamedPipeInfo(handle, &flags, nil, nil, nil); err != nil {
		return errors.Join(errProgressUnsupportedHandle, fmt.Errorf("identify actual stderr pipe: %w", err))
	}

	if flags&windows.PIPE_TYPE_MESSAGE != 0 {
		return errProgressUnsupportedHandle
	}

	if _, err := progressPipeMode(handle); err != nil {
		return err
	}

	device, information, backendErr := progressPipeBackend(handle, volumeQuery)
	if backendErr != nil {
		return backendErr
	}

	return qualifyProgressPipeBackend(device, information)
}

func qualifyProgressPipeBackend(device progressPipeDevice, information uintptr) error {
	if information != progressPipeDeviceBytes || device.deviceType != progressNamedPipeDevice ||
		device.characteristics&progressRemoteDevice != 0 {
		return errProgressUnsupportedHandle
	}

	return nil
}

func progressPipeMode(handle windows.Handle) (uint32, error) {
	// This information query is always synchronous, even on asynchronous handles.
	// Go 1.27.2 IsNonblock uses the same four-byte FileModeInformation query.
	const (
		fileModeInformation = 16
		fileModeBytes       = 4
	)

	var (
		mode   uint32
		status windows.IO_STATUS_BLOCK
		pin    runtime.Pinner
	)
	pin.Pin(&mode)

	pin.Pin(&status)
	defer pin.Unpin()

	queryErr := windows.NtQueryInformationFile(handle, &status, (*byte)(unsafe.Pointer(&mode)), fileModeBytes, fileModeInformation)
	runtime.KeepAlive(&mode)
	runtime.KeepAlive(&status)

	if queryErr != nil {
		return 0, errors.Join(errProgressUnsupportedHandle, fmt.Errorf("identify stderr pipe I/O mode: %w", queryErr))
	}

	if status.Information != fileModeBytes || mode&(windows.FILE_SYNCHRONOUS_IO_ALERT|windows.FILE_SYNCHRONOUS_IO_NONALERT) == 0 {
		return 0, errProgressUnsupportedHandle
	}

	return mode, nil
}

func progressPipeBackend(handle windows.Handle, query *windows.LazyProc) (progressPipeDevice, uintptr, error) {
	var (
		device progressPipeDevice
		status windows.IO_STATUS_BLOCK
		pin    runtime.Pinner
	)

	// The supported Windows64 ABI has an eight-byte device result and a
	// sixteen-byte status block with its byte-count member at offset eight.
	if unsafe.Sizeof(device) != progressPipeDeviceBytes || unsafe.Sizeof(status) != progressIOStatusBytes ||
		unsafe.Offsetof(status.Information) != progressStatusCountOffset {
		return device, 0, errProgressUnsupportedHandle
	}

	pin.Pin(&device)

	pin.Pin(&status)
	defer pin.Unpin()

	// Qualification already established a conforming synchronous file object.
	// Its native I/O completes before return; no asynchronous buffer owner exists
	// in this supported path. An actual unfinished return invalidates that premise.
	returned, _, _ := query.Call(uintptr(handle), uintptr(unsafe.Pointer(&status)), uintptr(unsafe.Pointer(&device)),
		progressPipeDeviceBytes, progressPipeDeviceClass)
	runtime.KeepAlive(&device)
	runtime.KeepAlive(&status)

	code := windows.NTStatus(returned) // Native NTSTATUS is the low 32-bit return value.
	if code != windows.STATUS_SUCCESS {
		return device, status.Information, errors.Join(errProgressUnsupportedHandle, fmt.Errorf("identify stderr pipe backend: %w", code))
	}

	return device, status.Information, nil
}
