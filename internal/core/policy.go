// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"fmt"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
)

func resolvePolicies(a pdf.Assembly, predict bool) []diag.Diagnostic {
	ds := pdf.CheckCompatibility(a).Diagnostics
	names := map[string]bool{}
	for i, p := range a.Parts {
		if p.Kind != packet.PartSource {
			continue
		}
		f := p.Occurrence.File
		add := func(code diag.Code, message string) {
			d := diag.New(code, message, p.Ref)
			d.File = p.Source
			ds = append(ds, d)
		}
		if a.Structure != packet.StructureDiscard && p.Occurrence.Facts.HasUncoveredContent {
			add(diag.SourceStructurePartial, "Retained source content has incomplete structural coverage; preservation does not invent missing semantics.")
			ds[len(ds)-1].PDFLocation = p.Occurrence.Facts.UncoveredLocation
		}
		if f.HasFields {
			if predict && i > 0 {
				add(diag.FormFieldsRenamed, "Predicted: pdfcpu may namespace this later form, including distinct names; build records actual mappings.")
				ds[len(ds)-1].PDFLocation = f.FieldCollectionLocation
				ds[len(ds)-1].Detail = "The original Fields collection is a possible rename scope; no actual new name is predicted."
			}
		}
		for _, name := range p.Occurrence.Facts.DestNames {
			if predict && names[name] {
				for _, location := range p.Occurrence.Facts.DestinationLocations[name] {
					add(diag.DestinationsRenamed, "Predicted: colliding destination names will be renamed for affected occurrences.")
					loc := location
					ds[len(ds)-1].PDFLocation = &loc
					ds[len(ds)-1].Detail = fmt.Sprintf("Original destination %q collides; no actual replacement name is predicted.", name)
				}
			}
			names[name] = true
		}
	}
	return ds
}
