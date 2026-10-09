// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"errors"
	"io"
	"net"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func assertProgressSocketProducer(t *testing.T) {
	t.Helper()

	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	requireProgressNoError(t, err)
	closeProgressResource(t, listener)

	address, valid := listener.Addr().(*net.TCPAddr)
	if !valid {
		t.Fatal("loopback listener has no TCP address")
	}

	client, err := net.DialTCP("tcp4", nil, address)
	requireProgressNoError(t, err)
	t.Cleanup(func() {
		if closeErr := client.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Error(closeErr)
		}
	})

	peer, err := listener.AcceptTCP()
	requireProgressNoError(t, err)
	closeProgressResource(t, peer)

	file, err := client.File()
	requireProgressNoError(t, err)
	t.Cleanup(func() {
		// This exact duplicated file is intentionally closed below before the final socket exchange.
		if closeErr := file.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) && !errors.Is(closeErr, net.ErrClosed) {
			t.Error(closeErr)
		}
	})
	// File() itself changes SDK IOCP setup; the working baseline starts afterward.
	assertProgressSocketExchange(t, client, peer)
	kind, err := windows.GetFileType(progressProducerHandle(t, file))
	requireProgressNoError(t, err)

	if kind != windows.FILE_TYPE_PIPE {
		t.Fatal("socket control did not exercise the misleading native pipe file type")
	}

	assertProgressProducerRefused(t, file)
	assertProgressSocketExchange(t, client, peer)
	requireProgressNoError(t, file.Close())
	assertProgressSocketExchange(t, client, peer)
}

func assertProgressSocketExchange(t *testing.T, sender, receiver *net.TCPConn) {
	t.Helper()

	_, err := sender.Write([]byte(progressProducerSentinel))
	requireProgressNoError(t, err)

	data := make([]byte, len(progressProducerSentinel))
	_, err = io.ReadFull(receiver, data)
	requireProgressNoError(t, err)

	if string(data) != progressProducerSentinel {
		t.Fatal("socket producer changed exact peer bytes")
	}
}
