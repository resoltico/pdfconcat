// SPDX-License-Identifier: AGPL-3.0-only
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
	plan := `{"version":8,"items":[{"page":{"size":"100x200pt"},"count":2}]}`
	for _, tc := range []struct {
		name  string
		args  []string
		stdin string
	}{
		{"check", []string{"check", "--plan", "-"}, plan},
	} {
		_, _, output := invokeJSON(t, tc.args, tc.stdin)
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
	invokeJSON(t, []string{"check", "--plan", "-", "--report", report}, plan)
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
	code := runContext(ctx, []string{"check", "--plan", "-", "--json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 130 || stderr.Len() > 0 {
		t.Fatalf("interruption exit %d stderr %q", code, stderr.String())
	}
	validateResult(t, stdout.Bytes())
}

func TestGoldenUsageText(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := run([]string{"unknown"}, strings.NewReader(""), &stdout, &stderr)
	expected, err := os.ReadFile(filepath.Join("testdata", "usage.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if exit != 2 || stdout.Len() != 0 || !bytes.Equal(stderr.Bytes(), expected) {
		t.Fatalf("usage streams: %d %q %q", exit, stdout.String(), stderr.String())
	}
}

func TestFittingOverrideAdmissionAndRoutes(t *testing.T) {
	dir := t.TempDir()
	input := `{"version":8,"fitting":{"target":"A4","tolerance":"1pt"},"items":[{"page":{}}]}`
	path := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, stdin string
		args        []string
		tolerance   float64
	}{
		{"named plan", "", []string{"--plan", path}, 1},
		{"stdin plan", input, []string{"--plan", "-"}, 1},
		{"direct", "", []string{"--blank", "--fit-tolerance=1pt", "--fit-to=A4"}, 1},
		{"replace target only", "", []string{"--plan", path, "--fit-to=A4"}, .01},
		{"override tolerance first", "", []string{"--fit-tolerance", "0pt", "--plan", path, "--fit-to", "A4"}, 0},
		{"override target first", "", []string{"--plan", path, "--fit-to", "A4", "--fit-tolerance", "-0pt"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, exit, data := invokeJSON(t, append([]string{"check"}, tc.args...), tc.stdin)
			if exit != 0 || r.FormatVersion != 10 || r.Fitting == nil || r.Fitting.Tolerance != tc.tolerance ||
				r.Fitting.Target.Width != 595.2755905511812 || r.Fitting.Target.Height != 841.8897637795276 {
				t.Fatalf("exit=%d %s", exit, data)
			}
			if strings.Contains(data, `"tolerance":-0`) {
				t.Fatal(data)
			}
		})
	}
	r, exit, _ := invokeJSON(t, []string{"check", "--plan", "-", "--fit-to=A4"}, `{"version":8,"fitting":{"target":"A4","tolerance":-1},"items":[{"page":{}}]}`)
	if exit != 2 || r.Fitting != nil || len(r.Diagnostics) != 1 || r.Diagnostics[0].Location.Ref != "/fitting/tolerance" {
		t.Fatalf("override bypassed authored plan admission: %+v", r)
	}
}
