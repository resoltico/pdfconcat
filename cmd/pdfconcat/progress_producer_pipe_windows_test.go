// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

type progressNamedPair struct {
	server       *os.File
	client       *os.File
	serverHandle windows.Handle
	clientHandle windows.Handle
}

func newProgressNamedPair(t *testing.T, pipeType, clientFlags uint32) *progressNamedPair {
	t.Helper()

	name, err := windows.UTF16PtrFromString(fmt.Sprintf(`\\.\pipe\pdfconcat-progress-%d`, os.Getpid()))
	requireProgressNoError(t, err)

	pair := &progressNamedPair{}

	t.Cleanup(func() { pair.release(t) })

	mode := pipeType | windows.PIPE_WAIT | windows.PIPE_REJECT_REMOTE_CLIENTS

	pair.serverHandle, err = windows.CreateNamedPipe(name, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		mode, 1, 4096, 4096, 0, nil)
	requireProgressNoError(t, err)

	flags := uint32(windows.FILE_ATTRIBUTE_NORMAL) | clientFlags

	pair.clientHandle, err = windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, flags, 0)
	requireProgressNoError(t, err)

	if err = windows.ConnectNamedPipe(pair.serverHandle, nil); err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		t.Fatal(err)
	}

	pair.server = os.NewFile(uintptr(pair.serverHandle), "native-producer-server")
	if pair.server == nil {
		t.Fatal("native named server wrapper unavailable")
	}

	pair.serverHandle = 0

	return pair
}

func (pair *progressNamedPair) wrapClient(t *testing.T) *os.File {
	t.Helper()

	pair.client = os.NewFile(uintptr(pair.clientHandle), "native-producer-client")
	if pair.client == nil {
		t.Fatal("native named client wrapper unavailable")
	}

	pair.clientHandle = 0

	return pair.client
}

func (pair *progressNamedPair) release(t *testing.T) {
	t.Helper()

	for _, file := range []*os.File{pair.client, pair.server} {
		if file != nil {
			if err := file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				t.Error(err)
			}
		}
	}

	for _, handle := range []windows.Handle{pair.clientHandle, pair.serverHandle} {
		if handle != 0 && handle != windows.InvalidHandle {
			requireProgressNoError(t, windows.CloseHandle(handle))
		}
	}
}

func assertProgressNamedProducer(t *testing.T, scenario string) {
	t.Helper()

	pipeType := uint32(windows.PIPE_TYPE_BYTE)
	if scenario == progressMessageProducer {
		pipeType = windows.PIPE_TYPE_MESSAGE | windows.PIPE_READMODE_MESSAGE
	}

	pair := newProgressNamedPair(t, pipeType, 0)

	client := pair.wrapClient(t)

	if scenario == progressMessageProducer {
		var flags uint32
		requireProgressNoError(t, windows.GetNamedPipeInfo(progressProducerHandle(t, client), &flags, nil, nil, nil))

		if flags&windows.PIPE_TYPE_MESSAGE == 0 {
			t.Fatal("message-pipe rejection control did not have a message producer")
		}

		assertProgressProducerRefused(t, client)
		_, err := client.WriteString(progressProducerSentinel)
		requireProgressNoError(t, err)

		data := make([]byte, len(progressProducerSentinel))
		_, err = pair.server.Read(data)
		requireProgressNoError(t, err)

		if string(data) != progressProducerSentinel {
			t.Fatal("refused message pipe did not preserve caller delivery")
		}

		return
	}

	if scenario == progressNamedServerProducer {
		assertProgressProducerDelivery(t, pair.server, client)
	} else {
		assertProgressProducerDelivery(t, client, pair.server)
	}
}
