// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestUsageErrorsHonorExplicitFormatBeforeIO(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, args := range [][]string{
		{"check", "--bad-option", "--format", "text"},
		{"check", "--format", "text", "--bad-option"},
		{"check", "--report=", "--format", "text"},
		{"check", "--format", "text", "--report="},
		{"check", "--report", "--format", "text", "missing.pdf"},
		{"build", "--plan", "--report", "report.json", "--format", "text"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := run(args, strings.NewReader("not a plan"), &stdout, &stderr)
			if exit != 2 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "error usage_") || strings.HasPrefix(stdout.String(), "{") || strings.Contains(stdout.String(), "source_unreadable") || strings.Contains(stdout.String(), "plan_syntax") {
				t.Fatalf("usage boundary: exit%d stdout%s stderr%s", exit, &stdout, &stderr)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("argv failure created files: %v %v", entries, err)
			}
		})
	}
}

func TestReportOptionLikeFilenameRemainsUsable(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := pdffixture.Plain("source").WriteFile("source.pdf"); err != nil {
		t.Fatal(err)
	}
	for _, literal := range [][]string{{"--report=--format"}, {"--report", "./--format"}, {"--report", "--"}} {
		args := append([]string{"check", "source.pdf"}, literal...)
		r, exit, _ := invoke(t, args, "")
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
