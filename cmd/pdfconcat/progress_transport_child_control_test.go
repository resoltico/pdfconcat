// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux || windows

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	progressChildControl         = "PDFCONCAT_PROGRESS_CHILD_CONTROL"
	progressChildState           = "PDFCONCAT_PROGRESS_CHILD_STATE"
	progressChildHolder          = "PDFCONCAT_PROGRESS_CHILD_HOLDER"
	progressChildControlSelector = "-test.run=^TestProgressTransportChildControlFixture$"
)

// The holder's release follows the capture refusal itself. Waiting for the
// controller to exit also charges process and coverage teardown to the holder's
// separate finite lifetime, even though the missing EOF is already established.
// os/exec serializes writes when stdout and stderr share this same writer.
type progressChildControlCapture struct {
	output          bytes.Buffer
	releasePath     string
	refusalObserved bool
	releaseErr      error
}

func (c *progressChildControlCapture) Write(data []byte) (int, error) {
	n, err := c.output.Write(data)
	if c.releasePath != "" && !c.refusalObserved && bytes.Contains(c.output.Bytes(), []byte("native child capture did not reach EOF")) {
		c.refusalObserved = true
		c.releaseErr = os.WriteFile(c.releasePath, nil, 0o600)
	}
	return n, errors.Join(err, c.releaseErr)
}

func progressControllerSettlementError(ctx context.Context, observedAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, known := ctx.Deadline(); known && !observedAt.Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

func TestProgressControllerSettlementChecksAbsoluteDeadline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), progressTransportChildBudget+4*time.Second)
	defer cancel()
	deadline, known := ctx.Deadline()
	if !known || ctx.Err() != nil {
		t.Fatal("live deadline fixture not established")
	}
	for _, item := range []struct {
		observed time.Time
		want     error
	}{
		{deadline.Add(-time.Nanosecond), nil},
		{deadline, context.DeadlineExceeded},
		{deadline.Add(time.Nanosecond), context.DeadlineExceeded},
	} {
		if err := progressControllerSettlementError(ctx, item.observed); !errors.Is(err, item.want) {
			t.Fatalf("process observed at %v: %v, want %v", item.observed, err, item.want)
		}
	}
	cancel()
	for _, observed := range []time.Time{deadline.Add(-time.Nanosecond), deadline.Add(time.Nanosecond)} {
		if err := progressControllerSettlementError(ctx, observed); err != context.Canceled {
			t.Fatalf("parent cancellation changed: %v", err)
		}
	}
	if err := progressControllerSettlementError(context.Background(), deadline); err != nil {
		t.Fatalf("no-deadline context acquired a deadline: %v", err)
	}
}

func TestProgressTransportChildCompletionContract(t *testing.T) {
	executable, err := os.Executable()
	requireProgressNoError(t, err)
	for _, scenario := range []string{"success", "assertion", "false-completion", "false-failure", "cleanup-stall", "startup-stall", "held-stdio", "killed-held-stdio", "unknown-exit", "runtime-fatal"} {
		t.Run(scenario, func(t *testing.T) {
			state := filepath.Join(t.TempDir(), "state")
			if scenario == "held-stdio" || scenario == "killed-held-stdio" {
				t.Cleanup(func() {
					if err := os.WriteFile(state+".release", nil, 0o600); err != nil {
						t.Errorf("release stdio holder during cleanup: %v", err)
					}
				})
			}
			helperArguments, controlEnv := progressControlArgs(progressChildControlSelector, progressVerboseHelperArgument)
			ctx, cancel := context.WithTimeout(t.Context(), progressTransportChildBudget+4*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, helperArguments...)
			command.Env = append(os.Environ(), progressChildControl+"="+scenario, progressChildState+"="+state)
			if controlEnv != "" {
				command.Env = append(command.Env, controlEnv)
			}
			capture := &progressChildControlCapture{}
			if scenario == "held-stdio" || scenario == "killed-held-stdio" {
				capture.releasePath = state + ".release"
			}
			reader, writer, err := os.Pipe()
			requireProgressNoError(t, err)
			command.Stdout, command.Stderr = writer, writer
			copied := make(chan error, 1)
			go func() { _, err := io.Copy(capture, reader); copied <- err }()
			runErr := command.Run() // The direct file handles separate process join from capture.
			controllerErr := progressControllerSettlementError(ctx, time.Now())
			captureErr := writer.Close()
			select {
			case err := <-copied:
				captureErr = errors.Join(captureErr, err, reader.Close())
			case <-time.After(time.Second):
				captureErr = errors.Join(captureErr, errors.New("controller capture did not reach EOF"), reader.Close())
				captureErr = errors.Join(captureErr, <-copied)
			}
			output := capture.output.Bytes() // Immutable after the owned copier joins.
			t.Cleanup(func() {
				if t.Failed() {
					t.Logf("native child control %s: %s", scenario, output)
				}
			})
			if controllerErr != nil {
				t.Fatalf("controller itself did not settle: %v (process=%v)", controllerErr, runErr)
			}
			requireProgressNoError(t, captureErr)
			var exit *exec.ExitError
			code := 0
			if runErr != nil {
				if !errors.As(runErr, &exit) {
					t.Fatal(runErr)
				}
				code = exit.ExitCode()
			}
			switch scenario {
			case "success":
				data, err := os.ReadFile(state)
				if code != 0 || err != nil || string(data) != "cleanup completed" || !bytes.Contains(output, []byte(progressTransportCleaned)) {
					t.Fatalf("cleanup receipt not established: code=%d state=%q err=%v", code, data, err)
				}
			case "assertion":
				if code != 1 || !bytes.Contains(output, []byte("native transport assertion or cleanup failed")) {
					t.Fatalf("ordinary assertion misclassified: code=%d", code)
				}
			case "cleanup-stall":
				if code != 1 || !bytes.Contains(output, []byte("body or cleanup exceeded")) || bytes.Contains(output, []byte(progressTransportCleaned)) {
					t.Fatalf("stalled cleanup claimed completion: code=%d", code)
				}
				before, err := os.ReadFile(state)
				requireProgressNoError(t, err)
				time.Sleep(50 * time.Millisecond)
				after, err := os.ReadFile(state)
				requireProgressNoError(t, err)
				if !bytes.Equal(before, after) {
					t.Fatal("private cleanup writer remained active after controller return")
				}
			default:
				if scenario == "held-stdio" || scenario == "killed-held-stdio" {
					if !bytes.Contains(output, []byte("native child capture did not reach EOF")) {
						t.Fatal("inherited stdio reached no independently verified EOF")
					}
					deadline := time.Now().Add(3 * time.Second)
					for {
						data, err := os.ReadFile(state)
						if err == nil && string(data) == "stdio holder completed" {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("bounded stdio holder did not finish")
						}
						time.Sleep(time.Millisecond)
					}
				}
				if code != 2 || !bytes.Contains(output, []byte("unknown native transport child outcome")) {
					t.Fatalf("unknown child converted to assertion: code=%d", code)
				}
			}
		})
	}
}

func TestProgressChildCaptureReleasesOnlyAfterCompleteEOFRefusal(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "release")
	capture := &progressChildControlCapture{releasePath: path}
	_, err := capture.Write([]byte(progressTransportCleaned + "fixture\n--- PASS: fixture (0.00s)\n"))
	requireProgressNoError(t, err)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("body completion released inherited stdio: %v", err)
	}
	_, err = capture.Write([]byte("native child capture did not "))
	requireProgressNoError(t, err)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial diagnostic released inherited stdio: %v", err)
	}
	_, err = capture.Write([]byte("reach EOF"))
	requireProgressNoError(t, err)
	_, err = os.Stat(path)
	requireProgressNoError(t, err)
	requireProgressNoError(t, os.Remove(path))
	_, err = capture.Write([]byte("native child capture did not reach EOF"))
	requireProgressNoError(t, err)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("duplicate refusal repeated holder release: %v", err)
	}
}

func TestProgressChildCapturePreservesReleaseFailureWithoutRetry(t *testing.T) {
	t.Parallel()
	directory := filepath.Join(t.TempDir(), "missing")
	path := filepath.Join(directory, "release")
	capture := &progressChildControlCapture{releasePath: path}
	_, err := capture.Write([]byte("native child capture did not reach EOF"))
	if !errors.Is(err, os.ErrNotExist) || !errors.Is(capture.releaseErr, os.ErrNotExist) {
		t.Fatalf("release failure disappeared: %v / %v", err, capture.releaseErr)
	}
	requireProgressNoError(t, os.Mkdir(directory, 0o700))
	_, err = capture.Write([]byte("later output"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("release was retried or its failure lost: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed release was retried: %v", err)
	}
}

func TestProgressTransportChildControlFixture(t *testing.T) {
	scenario := os.Getenv(progressChildControl)
	if os.Getenv(progressChildHolder) == "1" {
		// Finite foreign holder demonstrates drain settlement, not descendant ownership.
		// Outlive the child budget, capture refusal and one second of settlement.
		state := os.Getenv(progressChildState)
		requireProgressNoError(t, os.WriteFile(state+".ready", nil, 0o600))
		deadline := time.Now().Add(progressTransportChildBudget + 2*time.Second)
		for {
			if _, err := os.Stat(state + ".release"); err == nil {
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if time.Now().After(deadline) {
				t.Fatal("stdio holder release did not arrive within its existing bound")
			}
			time.Sleep(time.Millisecond)
		}
		requireProgressNoError(t, os.WriteFile(os.Getenv(progressChildState), []byte("stdio holder completed"), 0o600))
		return
	}
	if scenario == "" {
		return
	}
	if scenario == "startup-stall" && os.Getenv(progressTransportChild) == t.Name() {
		for {
			time.Sleep(time.Hour)
		}
	}
	if runProgressTransportChild(t, progressTransportChildBudget) {
		return
	}
	state := os.Getenv(progressChildState)
	switch scenario {
	case "success":
		t.Cleanup(func() { requireProgressNoError(t, os.WriteFile(state, []byte("cleanup completed"), 0o600)) })
	case "assertion":
		t.Error("ordinary native assertion")
	case "false-completion":
		if _, err := fmt.Println(progressTransportCleaned + t.Name()); err != nil {
			os.Exit(2)
		}
		if _, err := fmt.Printf("--- PASS: %s (0.00s)\nPASS\n", t.Name()); err != nil {
			os.Exit(2)
		}
		os.Exit(0) // stdout imitation cannot execute registered cleanup or create its receipt.
	case "false-failure":
		if _, err := fmt.Println(progressTransportFailed + t.Name()); err != nil {
			os.Exit(2)
		}
		if _, err := fmt.Printf("--- FAIL: %s (0.00s)\nFAIL\n", t.Name()); err != nil {
			os.Exit(2)
		}
		os.Exit(1) // A failed cleanup receipt is required, not an imitated FAIL line.
	case "cleanup-stall":
		t.Cleanup(func() {
			for {
				requireProgressNoError(t, os.WriteFile(state, []byte(time.Now().String()), 0o600))
				time.Sleep(10 * time.Millisecond)
			}
		})
	case "held-stdio", "killed-held-stdio":
		executable, err := os.Executable()
		requireProgressNoError(t, err)
		holder := exec.Command(executable, progressHelperArgs(progressChildControlSelector)...)
		holder.Env = append(os.Environ(), progressChildHolder+"=1")
		holder.Stdout, holder.Stderr = os.Stdout, os.Stderr
		requireProgressNoError(t, holder.Start())
		deadline := time.Now().Add(progressTransportChildBudget)
		for {
			if _, err := os.Stat(state + ".ready"); err == nil {
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if time.Now().After(deadline) {
				t.Fatal("stdio holder did not become ready")
			}
			time.Sleep(time.Millisecond)
		}
		if scenario == "killed-held-stdio" {
			for {
				time.Sleep(time.Hour)
			}
		}
	case "unknown-exit":
		os.Exit(2)
	case "runtime-fatal":
		values := map[int]int{}
		go func() {
			for {
				values[1]++
			}
		}()
		for {
			values[1]++
		}
	default:
		t.Fatalf("unknown native control scenario: %s", scenario)
	}
}
