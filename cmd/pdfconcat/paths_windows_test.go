// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsCLIPathRefusals(t *testing.T) {
	for _, path := range []string{`C:x`, `\x`, `/x`} {
		for _, option := range []string{"plan", "base-dir", "output", "report", "source"} {
			for _, existing := range []bool{false, true} {
				t.Run(option+"/"+path+"/"+map[bool]string{false: "absent report", true: "existing report"}[existing], func(t *testing.T) {
					dir := t.TempDir()
					report := filepath.Join(dir, "report.json")
					if existing {
						if err := os.WriteFile(report, []byte("keep existing"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					args := []string{"check", "--report", report, "--overwrite"}
					switch option {
					case "plan":
						args = append(args, "--plan", path)
					case "base-dir":
						args = append(args, "--plan", "-", "--base-dir", path)
					case "output":
						args = append(args, "-o", path, "--blank")
					case "report":
						args = []string{"check", "--report", path, "--blank"}
					case "source":
						args = append(args, path)
					}
					r, exit, _ := invoke(t, args, `{"version":4,"items":[{"page":{}}]}`)
					if exit != 2 || r.Status != "invalid" || !hasDiagnostic(r, "path_invalid") || hasDiagnostic(r, "plan_unreadable") {
						t.Fatalf("path refusal: exit=%d result=%+v", exit, r)
					}
					if existing {
						data, err := os.ReadFile(report)
						if err != nil || string(data) != "keep existing" {
							t.Fatalf("unsafe report replacement: %q %v", data, err)
						}
					} else if option != "report" && (r.Report == nil || !r.Report.Written) {
						t.Fatalf("safe report missing: %+v", r)
					}
				})
			}
		}
	}
}

func TestWindowsUnreadablePlanRemainsFailed(t *testing.T) {
	for _, directory := range []bool{false, true} {
		planPath := filepath.Join(t.TempDir(), "plan.json")
		if directory {
			if err := os.Mkdir(planPath, 0700); err != nil {
				t.Fatal(err)
			}
		}
		report := filepath.Join(t.TempDir(), "report.json")
		r, exit, _ := invoke(t, []string{"check", "--plan", planPath, "--report", report}, "")
		if exit != 1 || r.Status != "failed" || !hasDiagnostic(r, "plan_unreadable") || hasDiagnostic(r, "path_invalid") || r.Report == nil || !r.Report.Written {
			t.Fatalf("read failure: exit=%d result=%+v", exit, r)
		}
	}
}
