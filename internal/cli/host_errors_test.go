// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package cli_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/core"
)

func TestAcquiredWorkingDirectoryErrorsKeepEnvironmentalOutcome(t *testing.T) {
	for _, tc := range []struct{ name, message, want string }{{"valid", "cwd backend failed", "cwd backend failed"}, {"invalid UTF8", "cwd \xff failed", "cwd � failed"}, {"NUL", "cwd \x00 failed", `cwd \x00 failed`}} {
		for _, format := range []string{"json", "text"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				report := filepath.Join(t.TempDir(), "must-not-exist.json")
				var stdout, stderr bytes.Buffer
				args := []string{"check", "source.pdf", "--report", report}
				if format == "json" {
					args = append(args, "--json")
				}
				exit := cli.Run(t.Context(), args, nil, &stdout, &stderr, cli.Host{WorkingDir: func() (string, error) { return "", errors.New(tc.message) }})
				if exit != 1 || format == "json" && stderr.Len() != 0 || format == "text" && stderr.Len() == 0 {
					t.Fatalf("host failure classification: %d %q %q", exit, stdout.String(), stderr.String())
				}
				if format == "json" {
					var r core.Result
					if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
						t.Fatal(err)
					}
					if r.Status != "failed" || r.Command != "check" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code.String() != "working_directory_unavailable" || len(r.Diagnostics[0].Items) != 0 || r.Diagnostics[0].Message != "Cannot determine working directory: "+tc.want {
						t.Fatal(r)
					}
				} else if !strings.Contains(stdout.String(), "check failed") || !strings.Contains(stderr.String(), "working_directory_unavailable") || strings.Contains(stderr.String(), "[items") {
					t.Fatalf("host text failure: %q", stdout.String())
				}
				if _, err := os.Stat(report); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("host invocation wrote report: %v", err)
				}
			})
		}
	}
}
func TestParserRetainsCompleteNormalizedFailure(t *testing.T) {
	command, failure := cli.Parse([]string{"check", "--bad", "--json"})
	if !command.JSON || failure == nil || failure.FormatVersion != 5 || failure.Command != "check" || failure.Status != "invalid" || failure.Parts == nil || failure.Sections == nil || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code.String() != "usage_unknown_option" || failure.Diagnostics[0].Items[0] != "argv:1" || core.ExitCode(*failure) != 2 {
		t.Fatalf("complete parse outcome: %+v %+v", command, failure)
	}
}

type failingPlanReader struct{}

func (failingPlanReader) Read([]byte) (int, error) { return 0, errors.New("stdin read failed") }

func TestStdinAcquisitionFailureDoesNotWriteReport(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		dir := t.TempDir()
		report := filepath.Join(dir, "must-not-exist.json")
		ctx, cancel := context.WithCancel(t.Context())
		if cancelled {
			cancel()
		}
		var stdout, stderr bytes.Buffer
		exit := cli.Run(ctx, []string{"check", "--plan", "-", "--report", report, "--json"}, failingPlanReader{}, &stdout, &stderr, cli.Host{WorkingDir: func() (string, error) { return dir, nil }})
		cancel()
		var outcome core.Result
		if err := json.Unmarshal(stdout.Bytes(), &outcome); err != nil {
			t.Fatal(err)
		}
		want, wantExit := "plan_unreadable", 1
		if cancelled {
			want, wantExit = "interrupted", 130
		}
		if exit != wantExit || stderr.Len() != 0 || outcome.Report != nil || len(outcome.Diagnostics) != 1 || outcome.Diagnostics[0].Code.String() != want {
			t.Fatalf("%d %q %+v", exit, stderr.String(), outcome)
		}
		if _, err := os.Stat(report); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("acquisition failure wrote report: %v", err)
		}
	}
}
