// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type terminalDrain struct {
	data []byte
	err  error
}

func TestProgressTerminalBacklogPreservesCompleteRecord(t *testing.T) {
	if runProgressTransportChild(t, progressTransportChildBudget) {
		return
	}
	master, source := progressPhasePTY(t)
	transport := progressNativeTransport(t, source)
	masterFD, err := nativeProgressDescriptor(master)
	requireProgressNoError(t, err)
	requireProgressNoError(t, unix.SetNonblock(int(masterFD), true))
	padding := fillProgressNativeSink(t, transport.fd)
	record := bytes.Repeat([]byte("bounded native record\n"), 4096)
	stop := make(chan struct{})
	result := make(chan terminalDrain, 1)
	go func() {
		time.Sleep(20 * time.Millisecond)
		var got []byte
		buffer := make([]byte, 65536)
		deadline := time.Now().Add(time.Second)
		for {
			n, err := unix.Read(int(masterFD), buffer)
			if n > 0 {
				got = append(got, buffer[:n]...)
			}
			if err != nil && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EINTR) {
				result <- terminalDrain{got, err}
				return
			}
			if errors.Is(err, unix.EAGAIN) {
				select {
				case <-stop:
					result <- terminalDrain{got, nil}
					return
				default:
				}
			}
			if time.Now().After(deadline) {
				result <- terminalDrain{got, fmt.Errorf("native terminal drain exceeded one second")}
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	attempted, writeErr := transport.writeNonblocking(ctx, record)
	close(stop)
	drained := <-result
	requireProgressNoError(t, drained.err)
	if !attempted || writeErr != nil {
		t.Fatalf("full terminal retry failed: attempted=%t err=%v", attempted, writeErr)
	}
	expected := append(make([]byte, padding), record...)
	if !bytes.Equal(drained.data, expected) {
		t.Fatalf("terminal acknowledged incomplete record: got=%d want=%d", len(drained.data), len(expected))
	}
}
