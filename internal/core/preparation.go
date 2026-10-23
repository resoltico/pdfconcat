// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/files"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

type preparationOutcome struct {
	fonts       map[string]*typeset.Font
	facts       map[string]pdf.SourceFacts
	occurrences map[string]pdf.OccurrenceFacts
	domain      map[string]packet.SourceFacts
	diagnostics []diag.Diagnostic
}

func prepare(c context.Context, j packet.Job) preparationOutcome {
	fonts, ds := loadFonts(c, j)
	if hasErrors(ds) {
		return preparationOutcome{fonts: fonts, diagnostics: ds}
	}
	facts := map[string]pdf.SourceFacts{}
	occurrences := map[string]pdf.OccurrenceFacts{}
	domain := map[string]packet.SourceFacts{}
	for _, use := range sourceUses(j) {
		data, err := files.Read(c, use.path, 0)
		if err != nil {
			d := ioDiagnostic(diag.SourceUnreadable, err, use.path)
			d.Items = use.items
			ds = append(ds, d)
		} else {
			f, views, findings := pdf.InspectSelections(c, data, use.requests)
			for i := range findings {
				findings[i].File = use.path
				if len(findings[i].Items) == 0 {
					findings[i].Items = use.items
				}
			}
			ds = append(ds, findings...)
			facts[use.path] = f
			for item, view := range views {
				occurrences[item] = view
				domain[item] = view.SourceFacts
			}
		}
		if c.Err() != nil {
			break
		}
	}
	return preparationOutcome{fonts: fonts, facts: facts, occurrences: occurrences, domain: domain, diagnostics: ds}
}

type sourceUse struct {
	path     string
	items    []string
	requests []pdf.SelectionRequest
}

func sourceUses(j packet.Job) []sourceUse {
	var uses []sourceUse
	index := map[string]int{}
	leaves, _ := packet.Declarations(j)
	for _, p := range leaves {
		if p.Kind != "source" {
			continue
		}
		i, ok := index[p.Source]
		if !ok {
			i = len(uses)
			index[p.Source] = i
			uses = append(uses, sourceUse{path: p.Source})
		}
		uses[i].items = append(uses[i].items, p.Pointer)
		uses[i].requests = append(uses[i].requests, pdf.SelectionRequest{Item: p.Pointer, Pages: p.Selection, MeasureFooter: j.PageNumbers != nil && j.FitTo == nil})
	}
	return uses
}
