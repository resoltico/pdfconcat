// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/oracle"
	exchange "github.com/resoltico/pdfconcat/internal/wire"
)

func TestExecutableStdoutFailurePreservesPublication(t *testing.T) {
	tools := oracle.RequireTools(t)
	binary := buildBinary(t)
	for _, kind := range []string{"build", "version", "invalid-input"} {
		for _, closed := range []bool{false, true} {
			name := kind + "/normal"
			if closed {
				name = kind + "/closed-reader"
			}
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				pdf := filepath.Join(dir, "output.pdf")
				report := filepath.Join(dir, "report.json")
				args := []string{"version", "--json"}
				switch kind {
				case "build":
					args = []string{"build", "--json", "--blank", "--fit-to", "A4", "--output", pdf, "--report", report}
				case "invalid-input":
					args = []string{"build", "--json", "--output", pdf, filepath.Join(dir, "missing.pdf")}
				}
				cmd := exec.CommandContext(t.Context(), binary, args...)
				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				if closed {
					reader, writer, err := os.Pipe()
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						if err := writer.Close(); err != nil {
							t.Error(err)
						}
					})
					if err = reader.Close(); err != nil {
						t.Fatal(err)
					}
					// Passing the os.File directly makes this the child's actual FD1. The
					// reader is already closed before launch: no timing or helper-main path.
					cmd.Stdout = writer
				}
				err := cmd.Run()
				exit := 0
				if err != nil {
					var failure *exec.ExitError
					if !errors.As(err, &failure) {
						t.Fatal(err)
					}
					exit = failure.ExitCode()
				}
				want := 0
				if kind == "invalid-input" {
					want = 2
				}
				if closed {
					want = 1
				}
				if exit != want {
					t.Fatalf("exit=%d want=%d err=%v stderr=%q", exit, want, err, stderr.String())
				}
				if closed {
					if stderr.Len() == 0 {
						t.Fatal("stdout failure produced no diagnostic on working stderr")
					}
				} else {
					if stderr.Len() != 0 {
						t.Fatal(stderr.String())
					}
					validateResult(t, stdout.Bytes())
				}
				if kind == "build" {
					if err := tools.Check(pdf); err != nil {
						t.Fatal("published PDF lost or invalid:", err)
					}
					data, err := os.ReadFile(report)
					if err != nil {
						t.Fatal("published report lost:", err)
					}
					validateResult(t, data)
					var saved exchange.Result
					if err = json.Unmarshal(data, &saved); err != nil {
						t.Fatal(err)
					}
					if saved.Status != "ok" || !saved.Output.Published {
						t.Fatal("published report lost output facts", saved)
					}
				} else if _, err := os.Stat(pdf); !os.IsNotExist(err) {
					t.Fatal("non-build or invalid input published a PDF", err)
				}
			})
		}
	}
}
