// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package cli_test

import (
	"bytes"
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
				exit := cli.Run(t.Context(), []string{"check", "source.pdf", "--report", report, "--format", format}, nil, &stdout, &stderr, cli.Host{WorkingDir: func() (string, error) { return "", errors.New(tc.message) }})
				if exit != 1 || stderr.Len() != 0 {
					t.Fatalf("host failure classification: %d %q %q", exit, stdout.String(), stderr.String())
				}
				if format == "json" {
					var r core.Result
					if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
						t.Fatal(err)
					}
					if r.Status != "failed" || r.Command != "check" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "plan_unreadable" || len(r.Diagnostics[0].Items) != 0 || r.Diagnostics[0].Message != "Cannot determine working directory: "+tc.want {
						t.Fatal(r)
					}
				} else if !strings.Contains(stdout.String(), "check failed") || !strings.Contains(stdout.String(), "plan_unreadable") || strings.Contains(stdout.String(), "[items") {
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
	command, failure := cli.Parse([]string{"check", "--bad", "--format=text"})
	if command.Format != "text" || failure == nil || failure.FormatVersion != 4 || failure.Command != "check" || failure.Status != "invalid" || failure.Parts == nil || failure.Sections == nil || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code != "usage_unknown_option" || failure.Diagnostics[0].Items[0] != "argv:1" || core.ExitCode(*failure) != 2 {
		t.Fatalf("complete parse outcome: %+v %+v", command, failure)
	}
}
