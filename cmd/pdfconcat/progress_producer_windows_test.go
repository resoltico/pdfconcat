// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

type progressProducerRead struct {
	err  error
	data []byte
}

const (
	progressProducerScenario    = "PDFCONCAT_NATIVE_PRODUCER"
	progressProducerSelector    = "-test.run=^TestProgressNativeProducerHelper$"
	progressProducerReceipt     = "native producer complete: "
	progressProducerSentinel    = "original-producer\n"
	progressInheritedProducer   = "inherited"
	progressMessageProducer     = "message"
	progressNamedServerProducer = "named-server"
)

func TestProgressNativeProducerBoundaries(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	requireProgressNoError(t, err)

	for _, scenario := range []string{
		"anonymous", progressInheritedProducer, progressNamedServerProducer, "named-client",
		progressMessageProducer, "socket", "go-poller", "foreign-port",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			runProgressProducerChild(t, executable, scenario)
		})
	}
}

func runProgressProducerChild(t *testing.T, executable, scenario string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, executable, progressHelperArgs(progressProducerSelector, "-test.v")...)

	command.Env = append(os.Environ(), progressProducerScenario+"="+scenario)

	var inherited <-chan progressProducerRead

	if scenario == progressInheritedProducer {
		pipe, err := command.StderrPipe()
		requireProgressNoError(t, err)

		inherited = collectProgressProducerPipe(pipe)
	}

	var captured bytes.Buffer

	command.Stdout = &captured

	startErr := command.Start()
	if startErr != nil {
		if inherited != nil {
			<-inherited
		}

		t.Fatal(startErr)
	}

	var observed progressProducerRead
	if inherited != nil {
		observed = <-inherited // Drain to EOF before Wait closes the process pipe.
	}

	err := command.Wait()
	output := captured.Bytes()
	t.Logf("private native producer %s: %s", scenario, output)

	if ctx.Err() != nil || err != nil || !strings.Contains(string(output), progressProducerReceipt+scenario) ||
		!strings.Contains(string(output), "--- PASS: TestProgressNativeProducerHelper") {
		t.Fatalf("native producer scenario not proved (watchdog=%v): %v", ctx.Err(), err)
	}

	if inherited != nil {
		requireProgressNoError(t, observed.err)

		if string(observed.data) != progressTestRecord+progressProducerSentinel {
			t.Fatal("inherited stderr did not preserve exact transport and caller records")
		}
	}
}

func collectProgressProducerPipe(reader io.ReadCloser) <-chan progressProducerRead {
	result := make(chan progressProducerRead, 1)

	go func() {
		data, err := io.ReadAll(reader)
		result <- progressProducerRead{data: data, err: errors.Join(err, reader.Close())}
	}()

	return result
}

func TestProgressNativeProducerHelper(t *testing.T) {
	t.Parallel()

	scenario := os.Getenv(progressProducerScenario)
	if scenario == "" {
		return
	}

	switch scenario {
	case "anonymous":
		reader, writer := progressPipe(t)
		assertProgressProducerDelivery(t, writer, reader)
	case progressInheritedProducer:
		source := os.Stderr
		assertProgressProducerMetadata(t, source)
		transport := progressNativeTransport(t, source)
		requireProgressNoError(t, transport.WriteRecord(t.Context(), []byte(progressTestRecord)))
		requireProgressNoError(t, transport.Close())

		_, err := source.WriteString(progressProducerSentinel)
		requireProgressNoError(t, err)
	case progressNamedServerProducer, "named-client", progressMessageProducer:
		assertProgressNamedProducer(t, scenario)
	case "socket":
		assertProgressSocketProducer(t)
	case "go-poller":
		assertProgressGoPollerProducer(t)
	case "foreign-port":
		assertProgressForeignPortProducer(t)
	default:
		t.Fatal("unknown native producer scenario")
	}

	t.Log(progressProducerReceipt + scenario)
}

func assertProgressProducerDelivery(t *testing.T, source, peer *os.File) {
	t.Helper()
	assertProgressProducerMetadata(t, source)
	transport := progressNativeTransport(t, source)
	requireProgressNoError(t, transport.WriteRecord(t.Context(), []byte(progressTestRecord)))
	requireProgressNoError(t, transport.Close())

	_, err := source.WriteString(progressProducerSentinel)
	requireProgressNoError(t, err)

	data := make([]byte, len(progressTestRecord)+len(progressProducerSentinel))
	_, err = io.ReadFull(peer, data)
	requireProgressNoError(t, err)

	if string(data) != progressTestRecord+progressProducerSentinel {
		t.Fatal("native admission changed transport/caller bytes")
	}
}

func assertProgressProducerMetadata(t *testing.T, source *os.File) {
	t.Helper()
	handle := progressProducerHandle(t, source)
	mode, err := progressPipeMode(handle)
	requireProgressNoError(t, err)

	query := windows.NewLazySystemDLL("ntdll.dll").NewProc("NtQueryVolumeInformationFile")
	requireProgressNoError(t, query.Find())
	device, information, err := progressPipeBackend(handle, query)
	requireProgressNoError(t, err)
	t.Logf(
		"native pipe mode=%#x device=%#x characteristics=%#x information=%d",
		mode,
		device.deviceType,
		device.characteristics,
		information,
	)

	if information != progressPipeDeviceBytes || device.deviceType != progressNamedPipeDevice ||
		device.characteristics&progressRemoteDevice != 0 {
		t.Fatal("native positive producer did not meet local synchronous byte-pipe contract")
	}
}

func progressProducerHandle(t *testing.T, source *os.File) windows.Handle {
	t.Helper()

	raw, err := source.SyscallConn()
	requireProgressNoError(t, err)

	var handle windows.Handle

	requireProgressNoError(t, raw.Control(func(value uintptr) { handle = windows.Handle(value) }))

	return handle
}

func assertProgressProducerRefused(t *testing.T, source *os.File) {
	t.Helper()

	transport, err := newProgressTransport(source)
	if transport != nil || !errors.Is(err, errProgressUnsupportedHandle) {
		if transport != nil {
			requireProgressNoError(t, transport.Close())
		}

		t.Fatalf("unsupported producer admitted: %v", err)
	}
}
