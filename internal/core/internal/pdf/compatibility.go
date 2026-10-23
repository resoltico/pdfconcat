// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
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
			d := diag.New(diag.SourceInvalid, "Source occurrence facts are unavailable.", part.Ref)
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
		add := func(code diag.Code, message string) {
			d := diag.New(code, message, p.Ref)
			d.File = p.Source
			result.Diagnostics = append(result.Diagnostics, d)
		}
		authoredDepth := authoredDepths[p.Owner]
		// The backend counts the first native item at depth zero.
		if authoredDepth+selected.OutlineDepth > model.DefaultResourceLimits().MaxRecursionDepth+1 {
			add(diag.SourceOutlineDepthUnsupported, "Authored sections and imported bookmarks exceed the PDF backend's supported outline hierarchy; reduce section nesting or source bookmark depth.")
		}
		if a.FitTo != nil && f.HasWidgets {
			add(diag.FitFormUnsupported, "Fitting source form widgets is unsupported.")
		}
		if a.FitTo != nil && selected.HasCoordinateDestinations {
			add(diag.FitDestinationUnsupported, "Fitting internal page destinations with numeric position coordinates is unsupported; use the original page sizes.")
		}
		if f.HasFields {
			if defaults != "" && defaults != f.FormDefaultsDigest {
				add(diag.SourceFormUnsupported, "Nonempty forms require identical resolved DR/DA/Q representations; these differ; assemble separately or flatten with a suitable PDF tool.")
			} else {
				defaults = f.FormDefaultsDigest
			}
		}
		if result.version >= model.V20 && selected.PDF20AppearanceIssue != "" {
			add(diag.SourceOutputIncompatible, fmt.Sprintf("%s; source %q requires PDF 2.0 output. Supply complete annotation appearances or assemble the affected source separately.", selected.PDF20AppearanceIssue, versionSource))
		}
	}
	return result
}
