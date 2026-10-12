// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux || windows

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

func TestProgressTransportChildCompletionContract(t *testing.T) {
	executable, err := os.Executable()
	requireProgressNoError(t, err)
	for _, scenario := range []string{"success", "assertion", "false-completion", "false-failure", "cleanup-stall", "startup-stall", "held-stdio", "killed-held-stdio", "unknown-exit", "runtime-fatal"} {
		t.Run(scenario, func(t *testing.T) {
			state := filepath.Join(t.TempDir(), "state")
			helperArguments := progressHelperArgs(progressChildControlSelector, progressVerboseHelperArgument)
			ctx, cancel := context.WithTimeout(t.Context(), progressTransportChildBudget+4*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, helperArguments...)
			command.Env = append(os.Environ(), progressChildControl+"="+scenario, progressChildState+"="+state)
			output, runErr := command.CombinedOutput()
			t.Cleanup(func() {
				if t.Failed() {
					t.Logf("native child control %s: %s", scenario, output)
				}
			})
			if ctx.Err() != nil {
				t.Fatalf("controller itself did not settle: %v", ctx.Err())
			}
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

func TestProgressTransportChildControlFixture(t *testing.T) {
	scenario := os.Getenv(progressChildControl)
	if os.Getenv(progressChildHolder) == "1" {
		// Finite foreign holder demonstrates drain settlement, not descendant ownership.
		duration := 3 * time.Second
		if scenario == "killed-held-stdio" {
			duration = progressTransportChildBudget + 2*time.Second
		}
		time.Sleep(duration)
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
