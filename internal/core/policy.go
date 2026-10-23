// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
)

func resolvePolicies(layout packet.Layout, facts map[string]pdf.SourceFacts, occurrences map[string]pdf.OccurrenceFacts, fit *packet.Size, predict bool) []diag.Diagnostic {
	ds := pdf.CheckCompatibility(layout, facts, occurrences, fit).Diagnostics
	names := map[string]bool{}
	for i, p := range layout.Parts {
		if p.Kind != packet.PartSource {
			continue
		}
		f := facts[p.Source]
		add := func(code diag.Code, message string) {
			d := diag.New(code, message, p.Ref)
			d.File = p.Source
			ds = append(ds, d)
		}
		if f.HasFields {
			if predict && i > 0 {
				add(diag.FormFieldsRenamed, "Predicted: pdfcpu may namespace this later form, including distinct names; build records actual mappings.")
			}
		}
		collision := false
		for _, name := range occurrences[p.Ref].DestNames {
			if names[name] {
				collision = true
			}
			names[name] = true
		}
		if predict && collision {
			add(diag.DestinationsRenamed, "Predicted: colliding destination names will be renamed for affected occurrences.")
		}
	}
	return ds
}
