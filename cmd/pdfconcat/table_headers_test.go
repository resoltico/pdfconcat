// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestTableHeaderSelectionRefusedBeforePublication(t *testing.T) {
	for _, fit := range []bool{false, true} {
		t.Run(map[bool]string{false: "original", true: "fit"}[fit], func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "table.pdf")
			plan := filepath.Join(dir, "plan.json")
			output := filepath.Join(dir, "output.pdf")
			if e := pdffixture.MultipageTable().WriteFile(source); e != nil {
				t.Fatal(e)
			}
			extra := ""
			if fit {
				extra = `,"fit_to":"A4"`
			}
			data := `{"version":5` + extra + `,"items":[{"source":"table.pdf","pages":[[3,3]]}]}`
			if e := os.WriteFile(plan, []byte(data), 0600); e != nil {
				t.Fatal(e)
			}
			for _, args := range [][]string{{"check", "--plan", plan, "-o", output}, {"build", "--plan", plan, "-o", output}} {
				r, exit, _ := invokeJSON(t, args, "")
				if exit != 2 || r.Status != "invalid" || (r.Output != nil && r.Output.Published) {
					t.Fatalf("accepted: exit%d %+v", exit, r)
				}
				if len(r.Diagnostics) != 1 || r.Diagnostics[0].Code.String() != "source_unsupported" || !strings.Contains(r.Diagnostics[0].Message, "Include the referenced header pages") {
					t.Fatalf("diagnostics: %+v", r.Diagnostics)
				}
				if _, e := os.Stat(output); !os.IsNotExist(e) {
					t.Fatalf("output published: %v", e)
				}
				entries, e := os.ReadDir(dir)
				if e != nil {
					t.Fatal(e)
				}
				if len(entries) != 2 {
					t.Fatalf("unexpected staging files: %v", entries)
				}
			}
			// Explicit discard is a supported decision, rather than silent loss.
			data = strings.Replace(data, `"version":5`, `"version":5,"structure":"discard"`, 1)
			if e := os.WriteFile(plan, []byte(data), 0600); e != nil {
				t.Fatal(e)
			}
			r, exit, _ := invokeJSON(t, []string{"build", "--plan", plan, "-o", output}, "")
			if exit != 0 || r.Status != "ok" || !r.Output.Published {
				t.Fatalf("explicit discard: exit%d %+v", exit, r.Diagnostics)
			}
		})
	}
}
