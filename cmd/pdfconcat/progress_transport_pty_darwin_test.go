// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// TIOCPTYGNAME is _IOC(IOC_OUT, 't', 83, 128) in Darwin's ttycom.h.
const progressNativePTYNameBytes = 128

var errProgressPTYPath = errors.New("invalid bounded native PTY slave path")

func progressNativePTY(t *testing.T) *os.File {
	t.Helper()

	_, slave := progressNativePTYPair(t)

	return slave
}

// progressNativePTYPair retains the real master so tests can observe bytes or retire the peer.
func progressNativePTYPair(t *testing.T) (*os.File, *os.File) {
	t.Helper()

	master, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	requireProgressNoError(t, err)

	peer := os.NewFile(uintptr(master), "private progress PTY master")
	if peer == nil {
		requireProgressNoError(t, unix.Close(master))
		t.Fatal("native PTY master wrapper unavailable")
	}

	closeProgressResource(t, peer)
	requireProgressNoError(t, unix.IoctlSetInt(master, unix.TIOCPTYGRANT, 0))
	requireProgressNoError(t, unix.IoctlSetInt(master, unix.TIOCPTYUNLK, 0))
	path, err := readProgressPTYName(master)
	requireProgressNoError(t, err)
	devices, err := os.OpenRoot("/dev")

	requireProgressNoError(t, err)
	defer func() { requireProgressNoError(t, devices.Close()) }()

	slave, err := devices.OpenFile(strings.TrimPrefix(path, "/dev/"), os.O_RDWR, 0)
	requireProgressNoError(t, err)
	closeProgressResource(t, slave)

	return peer, slave
}

func readProgressPTYName(master int) (string, error) {
	var name [progressNativePTYNameBytes]byte

	var pin runtime.Pinner

	pin.Pin(&name[0])
	defer pin.Unpin()
	// On supported Darwin64, this integer carrier preserves the pinned address.
	// IoctlSetInt uses maintained libSystem ioctl; TIOCPTYGNAME defines the output
	// pointer/128-byte direction, independently of the wrapper's nominal setter name.
	err := unix.IoctlSetInt(master, unix.TIOCPTYGNAME, int(uintptr(unsafe.Pointer(&name[0]))))
	runtime.KeepAlive(&name)

	if err != nil {
		return "", fmt.Errorf("read native PTY slave name: %w", err)
	}

	path, _, terminated := bytes.Cut(name[:], []byte{0})
	if !terminated || len(path) == 0 || !bytes.HasPrefix(path, []byte("/dev/")) {
		return "", errProgressPTYPath
	}

	return string(path), nil
}

func TestProgressPTYNameRejectsNonterminalBeforeParsing(t *testing.T) {
	t.Parallel()

	file, err := os.Open(os.DevNull)
	requireProgressNoError(t, err)
	closeProgressResource(t, file)
	descriptor, err := nativeProgressDescriptor(file)
	requireProgressNoError(t, err)

	path, err := readProgressPTYName(int(descriptor))
	if path != "" || !errors.Is(err, unix.ENODEV) {
		t.Fatalf("native ioctl failure did not precede parsing/open: path=%q error=%v", path, err)
	}
}
