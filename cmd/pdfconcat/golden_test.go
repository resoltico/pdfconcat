// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenResultsAndReport(t *testing.T) {
	plan := `{"version":4,"items":[{"page":{"size":"100x200pt"},"count":2}]}`
	for _, tc := range []struct {
		name  string
		args  []string
		stdin string
	}{
		{"check", []string{"check", "--plan", "-"}, plan},
		{"usage", []string{"unknown"}, ""},
	} {
		_, _, output := invoke(t, tc.args, tc.stdin)
		expected, err := os.ReadFile(filepath.Join("testdata", tc.name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		validateResult(t, expected)
		if output != string(expected) {
			t.Fatalf("%s changed:\ngot %s\nwant %s", tc.name, output, expected)
		}
	}
	report := filepath.Join(t.TempDir(), "report.json")
	invoke(t, []string{"check", "--plan", "-", "--report", report}, plan)
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	validateResult(t, data)
	encodedPath, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, encodedPath, []byte(`"/abs/report.json"`))
	expected, err := os.ReadFile(filepath.Join("testdata", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	validateResult(t, expected)
	if !bytes.Equal(data, expected) {
		t.Fatalf("report changed:\ngot %s\nwant %s", data, expected)
	}
}

func TestInterruptedCommandIsOneJSONOutcome(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout, stderr bytes.Buffer
	code := runContext(ctx, []string{"check", "--plan", "-"}, strings.NewReader(""), &stdout, &stderr)
	if code != 130 || stderr.Len() > 0 {
		t.Fatalf("interruption exit %d stderr %q", code, stderr.String())
	}
	validateResult(t, stdout.Bytes())
}
