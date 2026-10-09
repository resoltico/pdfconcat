// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main_test

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdforacle"
	"github.com/resoltico/pdfconcat/internal/report"
)

const fittedSavedReport = "fitted.json"

func TestFittedBuildCapturesSourceAndGeneratedQueryFacts(t *testing.T) {
	t.Parallel()

	for _, paper := range []string{"A4", "Legal"} {
		t.Run(paper, func(t *testing.T) { t.Parallel(); checkFittedReportOutput(t, paper) })
	}
}

func checkFittedReportOutput(t *testing.T, paper string) {
	t.Helper()
	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")

	plan := `{"version":1,"items":["a.pdf",{"blank":{},"count":2},{"blank":{"size":"200x100","text":{"value":"FIT","size":10}}}]}`
	built := run(t, dir, "", commandBuild, inlinePlanFlag, plan, "--fit-to", paper, "-o", "fitted.pdf", flagReport, fittedSavedReport)
	requireExit(t, built, 0)

	root, err := os.OpenRoot(dir)
	ensure(t, err)
	file, err := root.Open(fittedSavedReport)
	ensure(t, err)
	saved, err := report.Decode(t.Context(), fittedSavedReport, file)
	ensure(t, err)
	ensure(t, file.Close())
	ensure(t, root.Close())

	if saved.Fit == nil || saved.Fit.Paper != paper || len(saved.Sources[0].Geometries) == 0 || *saved.Counts.TotalPages != 4 {
		t.Fatal("fit/source/count provenance missing")
	}

	document, err := pdforacle.Load(pdforacle.RequireTools(t), filepath.Join(dir, "fitted.pdf"))
	ensure(t, err)

	width, height := 210*72/25.4, 297*72/25.4
	if paper == "Legal" {
		width, height = 612, 1008
	}

	expected := pdforacle.Expectation{Sources: map[string]pdforacle.SourceFact{}, Pages: make([]pdforacle.ExpectedPage, 4)}
	for index := range expected.Pages {
		expected.Pages[index].Geometry = &pdforacle.Geometry{
			MediaBox: []float64{0, 0, width, height},
			CropBox:  []float64{0, 0, width, height},
			UserUnit: 1,
		}
	}
	// Text is inspected through the captured queries below; the source fixture owns its own marker.
	expected.Pages[0].Text = "a p1"

	expected.Pages[3].Text = "FIT"
	if findings := document.Verify(expected); len(findings) > 0 {
		t.Fatalf("independent fitted output: %v", findings)
	}

	ensure(t, os.Rename(filepath.Join(dir, "a.pdf"), filepath.Join(dir, "source-offline.pdf")))

	for _, page := range []int64{1, 2, 4} {
		selected := run(t, dir, "", commandReport, fittedSavedReport, "--page", strconv.FormatInt(page, 10), flagDetails)
		requireExit(t, selected, 0)
	}

	checkFittedBriefFacts(t, saved, paper, width, height)

	if saved.Styles[0].Size.Origin != report.SizeFitTarget {
		t.Fatal("implicit canvas did not resolve directly to target")
	}
}

func checkFittedBriefFacts(t *testing.T, saved *report.Report, paper string, width, height float64) {
	t.Helper()

	response, err := saved.Query(report.Request{View: report.ViewParts})
	ensure(t, err)

	parts, ok := report.ContentOf[report.ViewResponse[report.PartView]](response)
	if !ok || len(parts.Records) != 3 {
		t.Fatal("captured fitted job did not expose its three contributions")
	}

	for index := range parts.Records {
		part := &parts.Records[index]
		if part.Fit == nil || part.Fit.Paper != paper || part.FinalSize == nil {
			t.Fatal("brief contribution lost captured final fit facts")
		}

		if part.Generated != nil && (math.Abs(part.Generated.Width-width) > 1e-8 || math.Abs(part.Generated.Height-height) > 1e-8) {
			t.Fatal("generated brief reported authored dimensions as final sheet dimensions")
		}
	}

	var rendered bytes.Buffer

	ensure(t, response.RenderText(&rendered))

	if !strings.Contains(rendered.String(), "final sheet ") || !strings.Contains(rendered.String(), "fit to "+paper) {
		t.Fatal("plain captured source preview omitted final sheet dimensions or fit declaration")
	}
}
