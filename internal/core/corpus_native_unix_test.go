// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package core_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/core"
)

func corpusSignalBinary(t *testing.T, race bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pdfconcat")
	args := []string{"build", "-trimpath"}
	if race {
		args = append(args, "-race")
	}
	args = append(args, "-o", path, "../../cmd/pdfconcat")
	if out, err := exec.CommandContext(t.Context(), "go", args...).CombinedOutput(); err != nil {
		t.Fatalf("fresh signal binary: %s %v", out, err)
	}
	return path
}
func waitSignalOutcome(t *testing.T, cmd *exec.Cmd, stdout, stderr *bytes.Buffer) core.Result {
	t.Helper()
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 130 {
			t.Fatalf("SIGINT exit: %v stdout=%s stderr=%s", err, stdout, stderr)
		}
	case <-time.After(5 * time.Second):
		if err := cmd.Process.Kill(); err != nil {
			t.Error(err)
		}
		t.Fatal("SIGINT did not terminate within five seconds")
	}
	var result core.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("one complete JSON outcome: %s %v", stdout, err)
	}
	if result.Status != "interrupted" || core.ExitCode(result) != 130 || stderr.Len() != 0 {
		t.Fatalf("signal outcome: %+v stderr=%s", result, stderr)
	}
	if err := os.WriteFile(filepath.Join(t.ArtifactDir(), "stdout.json"), stdout.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(t.ArtifactDir(), "stderr.txt"), stderr.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestCorpusPendingStdinSIGINT(t *testing.T) {
	for _, race := range []bool{false, true} {
		t.Run(fmt.Sprintf("race=%t", race), func(t *testing.T) {
			binary := corpusSignalBinary(t, race)
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := reader.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
					t.Error(err)
				}
			})
			t.Cleanup(func() {
				if err := writer.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
					t.Error(err)
				}
			})
			fd := int(writer.Fd())
			if err = syscall.SetNonblock(fd, true); err != nil {
				t.Fatal(err)
			}
			chunk := bytes.Repeat([]byte(" "), 4096)
			filled := 0
			for {
				n, err := syscall.Write(fd, chunk)
				if n > 0 {
					filled += n
				}
				if errors.Is(err, syscall.EAGAIN) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			for {
				if _, err = syscall.Write(fd, []byte(" ")); errors.Is(err, syscall.EAGAIN) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				filled++
			}

			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "check", "--plan", "-")
			cmd.Stdin = reader
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			ready := false
			for time.Now().Before(deadline) {
				if _, err = syscall.Write(fd, []byte(" ")); err == nil {
					ready = true
					break
				} else if !errors.Is(err, syscall.EAGAIN) {
					t.Fatal(err)
				}
				time.Sleep(time.Millisecond)
			}
			if !ready {
				cancel()
				_ = cmd.Wait()
				t.Fatal("child did not consume a byte from the proven-full pipe")
			}
			t.Logf("pipe_full_bytes=%d observed_child_consumption=true explicit_race_build=%t", filled, race)
			r := waitSignalOutcome(t, cmd, &stdout, &stderr)
			if r.Output != nil {
				t.Fatal("pending stdin invented output")
			}
		})
	}
}
func TestCorpusStagedBuildSIGINT(t *testing.T) {
	if os.Getenv("PDFCONCAT_SCALE") != "1" {
		t.Skip("opt-in concrete native staging workload: PDFCONCAT_SCALE=1")
	}
	for _, race := range []bool{false, true} {
		t.Run(fmt.Sprintf("race=%t", race), func(t *testing.T) {
			binary := corpusSignalBinary(t, race)
			dir := t.ArtifactDir()
			plan := filepath.Join(dir, "plan.json")
			corpusJSON(t, plan, map[string]any{"version": 4, "output": "packet.pdf", "items": []any{map[string]any{"page": map[string]any{"size": "A4"}, "count": 50000}}})
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "build", "--plan", plan)
			cmd.Dir = dir
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			ready := false
			deadline := time.Now().Add(45 * time.Second)
			for time.Now().Before(deadline) {
				names, err := filepath.Glob(filepath.Join(dir, ".packet.pdf.*.tmp"))
				if err != nil {
					t.Fatal(err)
				}
				if len(names) > 0 {
					ready = true
					break
				}
				time.Sleep(time.Millisecond)
			}
			if !ready {
				cancel()
				_ = cmd.Wait()
				t.Fatal("owned staging file not observed; no signal-readiness fallback is accepted")
			}
			r := waitSignalOutcome(t, cmd, &stdout, &stderr)
			if r.Output == nil || r.Output.Published {
				t.Fatal(r)
			}
			if _, err := os.Stat(filepath.Join(dir, "packet.pdf")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("final output visible: %v", err)
			}
			temps, err := filepath.Glob(filepath.Join(dir, ".packet.pdf.*.tmp"))
			if err != nil || len(temps) != 0 {
				t.Fatalf("owned staging cleanup: %v %v", temps, err)
			}
		})
	}
}
