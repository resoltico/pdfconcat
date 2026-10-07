//go:build !windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const mutationSignalFixture = `package main
import("context";"fmt";"os";"os/signal";"syscall")
func main(){
 ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM)
 defer stop()
 fmt.Println("ready")
 <-ctx.Done()
 fmt.Println("drained")
}
`

func TestMutationLauncherForwardsCancellationAndWaitsForDrain(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module example.org/mutationsignal\n", "main.go": mutationSignalFixture} {
		if writeErr := os.WriteFile(filepath.Join(dir, name), []byte(body), fileMode); writeErr != nil {
			t.Fatal(writeErr)
		}
	}

	binary := filepath.Join(dir, "signal-fixture")
	if buildErr := goCommand(dir, goBuildVerb, "-o", binary, ".").run(t.Context()); buildErr != nil {
		t.Fatal(buildErr)
	}

	stdout := mutationOutputFile(t, dir)
	stderr := mutationOutputFile(t, dir)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	result := make(chan error, 1)
	go func() { result <- (&command{name: binary, stdout: stdout, stderr: stderr}).runMutation(ctx, 0) }()

	waitMutationReady(t, stdout)
	cancel()

	select {
	case launchErr := <-result:
		if !errors.Is(launchErr, context.Canceled) {
			t.Fatalf("cancellation outcome lost: %v", launchErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("signal-aware child did not drain promptly")
	}

	var text [14]byte

	count, readErr := stdout.ReadAt(text[:], 0)
	if readErr != nil || string(text[:count]) != "ready\ndrained\n" {
		t.Fatalf("child was killed before graceful drain: %q %v", text[:count], readErr)
	}
}

func mutationOutputFile(t *testing.T, dir string) *os.File {
	t.Helper()

	file, createErr := os.CreateTemp(dir, "output-*")
	if createErr != nil {
		t.Fatal(createErr)
	}

	t.Cleanup(func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	return file
}

func waitMutationReady(t *testing.T, file *os.File) {
	t.Helper()

	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		var text [6]byte

		count, readErr := file.ReadAt(text[:], 0)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			t.Fatal(readErr)
		}

		if count == len(text) && string(text[:]) == "ready\n" {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatal("child did not establish signal readiness")
}
