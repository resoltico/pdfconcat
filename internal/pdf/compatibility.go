// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// Compatibility contains the assembly policy shared by prediction and writing.
// The version stays private to the adapter because callers need only diagnostics.
type Compatibility struct {
	version     model.Version
	Diagnostics []diag.Diagnostic
}

// CheckCompatibility applies rules depending on the complete ordered packet.
func CheckCompatibility(a Assembly) Compatibility {
	result := Compatibility{version: model.V17}
	for _, part := range a.Parts {
		if part.Kind == packet.PartSource && (part.Occurrence == nil || part.Occurrence.File == nil || part.Occurrence.Facts == nil) {
			d := diag.Report(diag.SourceProcessingFailed, diag.SourceOccurrenceFactsAreUnavailable(), part.Ref)
			d.File = part.Source
			result.Diagnostics = append(result.Diagnostics, d)
		}
	}
	if len(result.Diagnostics) > 0 {
		return result
	}

	versionSource := ""
	for _, p := range a.Parts {
		if p.Source == "" {
			continue
		}
		f := p.Occurrence.File
		if f.Version == "2.0" {
			result.version = model.V20
			if versionSource == "" {
				versionSource = p.Source
			}
		}
	}
	authoredDepths := make(map[string]int, len(a.Sections))
	for _, section := range a.Sections {
		authoredDepths[section.Item] = section.Depth + 1
	}
	defaults := ""
	for _, p := range a.Parts {
		if p.Source == "" {
			continue
		}
		f := p.Occurrence.File
		selected := p.Occurrence.Facts
		add := func(code diag.Code, message diag.Explanation) {
			d := diag.Report(code, message, p.Ref)
			d.File = p.Source
			result.Diagnostics = append(result.Diagnostics, d)
		}
		authoredDepth := authoredDepths[p.Owner]
		// The backend counts the first native item at depth zero.
		if authoredDepth+selected.OutlineDepth > model.DefaultResourceLimits().MaxRecursionDepth+1 {
			add(diag.SourceOutlineDepthUnsupported, diag.AuthoredSectionsAndImportedBookmarksExceedThePDFBackendS())
		}
		if !fittingOwnsSelection(selected, a.FitTo, f.Pages) {
			add(diag.SourceProcessingFailed, diag.SourceOccurrenceGeometryIsInconsistent())
			continue
		}
		for _, finding := range selected.fitFindings {
			finding.File = p.Source
			finding.Items = []string{p.Ref}
			result.Diagnostics = append(result.Diagnostics, finding)
		}
		if f.HasFields {
			if defaults != "" && defaults != f.FormDefaultsDigest {
				add(diag.SourceFormUnsupported, diag.NonemptyFormsRequireIdenticalResolvedDRDAQRepresentationsThese())
			} else {
				defaults = f.FormDefaultsDigest
			}
		}
		if result.version >= model.V20 && selected.PDF20AppearanceIssue.Present() {
			add(diag.SourceOutputIncompatible, diag.PDF20AnnotationRefusal(selected.PDF20AppearanceIssue, versionSource))
			loc := selected.PDF20AppearanceIssue.Site
			result.Diagnostics[len(result.Diagnostics)-1].PDFLocation = &loc
		}
	}
	return result
}

func sameFittingTarget(a, b *packet.Size) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
