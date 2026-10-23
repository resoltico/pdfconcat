// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestUsageStreamsBeforeIO(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, base := range [][]string{
		{"check", "--bad-option"},
		{"check", "--report="},
		{"check", "--report"},
		{"build", "--plan", "--report", "report.json"},
	} {
		for _, asJSON := range []bool{false, true} {
			args := append([]string(nil), base...)
			if asJSON {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			exit := run(args, strings.NewReader("not a plan"), &stdout, &stderr)
			if exit != 2 {
				t.Fatal(exit, stdout.String(), stderr.String())
			}
			if asJSON {
				var value core.Result
				if err := json.Unmarshal(stdout.Bytes(), &value); err != nil || stderr.Len() != 0 || len(value.Diagnostics) != 1 || !strings.HasPrefix(value.Diagnostics[0].Code.String(), "usage_") {
					t.Fatalf("JSON usage streams: %q / %q: %v", stdout.String(), stderr.String(), err)
				}
			} else if stdout.Len() != 0 || !strings.Contains(stderr.String(), "error usage_") || !strings.Contains(stderr.String(), "Run \"pdfconcat help") {
				t.Fatalf("text usage streams: %q / %q", stdout.String(), stderr.String())
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("argv failure created files: %v %v", entries, err)
			}
		}
	}
}

func TestReportOptionLikeFilenameRemainsUsable(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := pdffixture.Plain("source").WriteFile("source.pdf"); err != nil {
		t.Fatal(err)
	}
	for _, literal := range [][]string{{"--report=--json"}, {"--report", "./--json"}, {"--report", "--"}} {
		args := append([]string{"check", "source.pdf", "--json"}, literal...)
		r, exit, _ := invokeJSON(t, args, "")
		if exit != 0 || r.Report == nil || !r.Report.Written {
			t.Fatalf("explicit report literal %v: %+v exit%d", literal, r, exit)
		}
		if _, err := os.Stat(r.Report.Path); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(r.Report.Path); err != nil {
			t.Fatal(err)
		}
	}
}
