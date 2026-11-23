// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
)

func TestContentsOnlyFontProtected(t *testing.T) {
	for _, role := range []string{"output", "report"} {
		for _, alias := range []bool{false, true} {
			t.Run(role+map[bool]string{false: "/same", true: "/hardlink"}[alias], func(t *testing.T) {
				dir := t.TempDir()
				original, err := os.ReadFile("testdata/corpus/fonts/Go-Mono.ttf")
				if err != nil {
					t.Fatal(err)
				}
				font := filepath.Join(dir, "font.ttf")
				if err = os.WriteFile(font, original, 0600); err != nil {
					t.Fatal(err)
				}
				dest := font
				if alias {
					dest = filepath.Join(dir, "alias.ttf")
					if err = os.Link(font, dest); err != nil {
						t.Fatal(err)
					}
				}
				req := app.Request{InspectionWorkers: 1, Operation: app.Build, WorkingDir: dir, Overwrite: true, Output: "out.pdf", Input: app.PlanBytes{Data: []byte(`{"version":11,"page_defaults":{"size":"A4","text":{"font":{"file":"font.ttf"}}},"items":[{"contents":{}},{"title":"Section","items":[{"page":{"text":{"font":"default"}}}]}]}`)}}
				if role == "output" {
					req.Output = dest
				} else {
					req.Report = dest
				}
				result := app.Execute(t.Context(), req)
				found := false
				for _, d := range result.Diagnostics {
					found = found || d.Code.String() == role+"_is_input"
				}
				if result.Status != "invalid" || !found {
					t.Fatalf("expected protected input refusal: %+v", result)
				}
				after, err := os.ReadFile(font)
				if err != nil || !bytes.Equal(original, after) {
					t.Fatalf("font changed: %v", err)
				}
			})
		}
	}
}
