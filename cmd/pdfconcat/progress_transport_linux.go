// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build linux

package main

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type progressTransport struct {
	*progressTransportState
}

func openProgressDescriptor(file *os.File) (progressNativeSink, error) {
	descriptor, err := nativeProgressDescriptor(file)
	if err != nil {
		return progressNativeSink{}, err
	}

	var stat unix.Stat_t
	if err = unix.Fstat(int(descriptor), &stat); err != nil {
		return progressNativeSink{}, fmt.Errorf("inspect stderr type: %w", err)
	}

	if stat.Mode&unix.S_IFMT != unix.S_IFIFO && stat.Mode&unix.S_IFMT != unix.S_IFCHR {
		return progressNativeSink{}, errProgressSinkType
	}

	if stat.Mode&unix.S_IFMT == unix.S_IFCHR && !term.IsTerminal(int(descriptor)) {
		return progressNativeSink{}, errProgressSinkType
	}
	// Opening procfs makes a distinct open-file description even for a pipe.
	// Dup would instead share O_NONBLOCK with the parent and aliased stdout.
	owned, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", descriptor), unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return progressNativeSink{}, fmt.Errorf("open owned progress sink: %w", err)
	}

	protected, protectErr := protectProgressDescriptor(owned)
	if protectErr != nil {
		return progressNativeSink{}, protectErr
	}

	return progressNativeSink{fd: protected, guard: -1}, nil
}

func (*progressTransport) startNativeProgress() {}

func (*progressTransport) stopNativeProgress() {}

func (transport *progressTransport) closeNativeProgress() {
	transport.closeErr = unix.Close(transport.fd)
}

func (transport *progressTransport) writeNativeRecord(ctx context.Context, record []byte) (bool, error) {
	return transport.writeNonblocking(ctx, record)
}
