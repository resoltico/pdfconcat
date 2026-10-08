// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package scale

import (
	"fmt"
	"strings"
	"time"

	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

// Verification is the independent judgement of one output file.
type Verification struct {
	// Findings are the failed oracle checks and appearance mismatches; empty means the output is right.
	Findings []string
	// Elapsed is how long qpdf, pdftotext and pdftoppm took; it is excluded from the run's measurement.
	Elapsed time.Duration
	// Pages and Objects are the output's page and object counts as qpdf sees them.
	Pages, Objects int
}

// colorTolerance is the per-channel tolerance for rendered colors: anti-aliasing and color conversion.
const (
	colorTolerance              = 3
	maximumChangedPixelFraction = 0.001
)

// Verify checks the output at path against the workload with qpdf, pdftotext and pdftoppm: page count
// and text of every page, source identity and occurrence of every source page, link and destination
// membership, geometry, version, and the rendered background of each recorded appearance.
func Verify(tools pdforacle.Tools, workload *Workload, path string) (Verification, error) {
	started := time.Now()

	doc, err := pdforacle.Load(tools, path)
	if err != nil {
		return Verification{}, fmt.Errorf("load output: %w", err)
	}

	result := Verification{Pages: doc.PageCount(), Objects: doc.ObjectCount()}

	for _, finding := range doc.Verify(workload.Expectation) {
		result.Findings = append(result.Findings, finding.Check+": "+finding.Detail)
	}

	for _, want := range workload.Appearances {
		got, renderErr := doc.PixelColor(want.Page, want.X, want.Y)
		if renderErr != nil {
			return Verification{}, fmt.Errorf("render page %d: %w", want.Page, renderErr)
		}

		for channel := range got {
			if diff(got[channel], want.Color[channel]) > colorTolerance {
				result.Findings = append(result.Findings, mismatch(want, got))

				break
			}
		}
	}

	if appearanceErr := verifySourceAppearance(doc, tools, workload, &result); appearanceErr != nil {
		return Verification{}, appearanceErr
	}

	result.Elapsed = time.Since(started)

	return result, nil
}

// mismatch describes a rendered pixel that is not the recorded color.
func mismatch(want Appearance, got [3]uint8) string {
	return fmt.Sprintf("appearance: page %d pixel (%d,%d) is %v, want %v", want.Page, want.X, want.Y, got, want.Color)
}

func diff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}

	return b - a
}

func verifySourceAppearance(doc *pdforacle.Document, tools pdforacle.Tools, workload *Workload, result *Verification) error {
	for _, want := range workload.VisualSources {
		source, err := pdforacle.Load(tools, want.Path)
		if err != nil {
			return fmt.Errorf("load appearance source: %w", err)
		}

		difference, err := doc.AppearanceDifference(want.OutputPage, source, want.SourcePage, colorTolerance)
		if err != nil {
			return fmt.Errorf("compare source appearance: %w", err)
		}

		if difference > maximumChangedPixelFraction {
			result.Findings = append(
				result.Findings,
				fmt.Sprintf("source appearance: output page %d differs at %.4f of pixels", want.OutputPage, difference),
			)
		}
	}

	if len(workload.FullTexts) == 0 {
		return nil
	}

	pages, extractErr := doc.RawTexts()
	if extractErr != nil {
		return extractErr
	}

	for page, want := range workload.FullTexts {
		if page > len(pages) || strings.Join(strings.Fields(pages[page-1]), " ") != strings.Join(strings.Fields(want), " ") {
			result.Findings = append(result.Findings, fmt.Sprintf("full text: output page %d does not preserve its complete text", page))
		}
	}

	return nil
}
