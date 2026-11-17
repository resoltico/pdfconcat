// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdf"
)

func resolvePolicies(a pdf.Assembly, predict bool) []diag.Diagnostic {
	ds := pdf.CheckCompatibility(a).Diagnostics
	names := map[string]bool{}
	for i, p := range a.Parts {
		if p.Kind != packet.PartSource {
			continue
		}
		f := p.Occurrence.File
		add := func(code diag.Code, message diag.Explanation) {
			d := diag.Report(code, message, p.Ref)
			d.File = p.Source
			d.Identity.Phase = "source_compatibility"
			ds = append(ds, d)
		}
		if a.Structure != packet.StructureDiscard && p.Occurrence.Facts.HasUncoveredContent {
			add(diag.SourceStructurePartial, diag.RetainedSourceContentHasIncompleteStructuralCoveragePreservationDoesNot())
			ds[len(ds)-1].PDFLocation = p.Occurrence.Facts.UncoveredLocation
		}
		if f.HasFields {
			if predict && i > 0 {
				add(diag.FormFieldsRenamed, diag.PredictedPdfcpuMayNamespaceThisLaterFormIncludingDistinctNames())
				ds[len(ds)-1].PDFLocation = f.FieldCollectionLocation
				diag.AttachEvidence(&ds[len(ds)-1], diag.PredictedFieldScopeEvidence())
			}
		}
		for _, name := range p.Occurrence.Facts.DestNames {
			if predict && names[name] {
				for _, location := range p.Occurrence.Facts.DestinationLocations[name] {
					add(diag.DestinationsRenamed, diag.PredictedCollidingDestinationNamesWillBeRenamedForAffectedOccurrences())
					loc := location
					ds[len(ds)-1].PDFLocation = &loc
					diag.AttachEvidence(&ds[len(ds)-1], diag.DestinationCollisionEvidence(name))
				}
			}
			names[name] = true
		}
	}
	return ds
}
