// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"sync"

	"github.com/resoltico/pdfconcat/internal/compose"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/files"
	"github.com/resoltico/pdfconcat/internal/pdf"
)

type inspection struct {
	fonts       compose.Fonts
	byLeaf      []inspectedLeaf
	diagnostics []diag.Diagnostic
}
type inspectedLeaf struct {
	occurrence *pdf.Occurrence
	shape      packet.SourceFacts
}

func (i *inspection) sourceFacts() []packet.SourceFacts {
	facts := make([]packet.SourceFacts, len(i.byLeaf))
	for index, leaf := range i.byLeaf {
		facts[index] = leaf.shape
	}
	return facts
}

type sourceUse struct {
	structure packet.StructurePolicy
	path      string
	leaves    []int
	requests  []pdf.SelectionRequest
}
type sourceInspection struct {
	file        *pdf.SourceFacts
	views       []*pdf.OccurrenceFacts
	diagnostics []diag.Diagnostic
}

func inspect(ctx context.Context, p packet.Plan, workerCount int) *inspection {
	fonts, ds := loadFonts(ctx, p)
	out := &inspection{fonts: fonts, diagnostics: ds, byLeaf: make([]inspectedLeaf, len(p.Leaves()))}
	if hasErrors(ds) {
		return out
	}

	uses := sourceUses(p)
	results := make([]sourceInspection, len(uses))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(workerCount, len(uses)) {
		workers.Go(func() {
			for index := range jobs {
				results[index] = inspectSource(ctx, uses[index])
			}
		})
	}
dispatch:
	for index := range uses {
		if ctx.Err() != nil {
			break
		}
		select {
		case jobs <- index:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	workers.Wait()
	for index, use := range uses {
		measured := results[index]
		out.diagnostics = append(out.diagnostics, measured.diagnostics...)
		if measured.file != nil && len(measured.views) != len(use.leaves) {
			out.diagnostics = append(out.diagnostics, diag.Report(diag.SourceProcessingFailed, diag.SourceInspectionReturnedInconsistentOccurrenceOwnership()))
			continue
		}
		if measured.file != nil {
			for index, leaf := range use.leaves {
				view := measured.views[index]
				out.byLeaf[leaf].occurrence = &pdf.Occurrence{File: measured.file, Facts: view}
				if view != nil {
					out.byLeaf[leaf].shape = view.SourceFacts
				}
			}
		}
	}

	return out
}
func inspectSource(ctx context.Context, use sourceUse) sourceInspection {
	items := make([]string, len(use.requests))
	for i, request := range use.requests {
		items[i] = request.Item
	}
	data, err := files.Read(ctx, use.path, 0)
	if err != nil {
		d := ioDiagnostic(diag.SourceUnreadable, err, use.path)
		d.Items = items
		return sourceInspection{diagnostics: []diag.Diagnostic{d}}
	}
	facts, views, ds := pdf.Inspect(ctx, data, use.structure, use.requests)
	for i := range ds {
		ds[i].File = use.path
		if len(ds[i].Items) == 0 {
			ds[i].Items = items
		}
	}
	return sourceInspection{file: &facts, views: views, diagnostics: ds}
}
func sourceUses(p packet.Plan) []sourceUse {
	var uses []sourceUse
	index := map[string]int{}
	for leafIndex, item := range p.Leaves() {
		if item.Kind != packet.PartSource {
			continue
		}
		slot, ok := index[item.Source]
		if !ok {
			slot = len(uses)
			index[item.Source] = slot
			uses = append(uses, sourceUse{path: item.Source, structure: p.Structure()})
		}
		uses[slot].leaves = append(uses[slot].leaves, leafIndex)
		uses[slot].requests = append(uses[slot].requests, pdf.SelectionRequest{FitTo: p.FitTo(), Item: item.Ref, Pages: item.Selection, MeasureNumbers: p.PageNumbers() != nil && p.FitTo() == nil && p.PageNumbers().Eligible(item.Kind, item.NumberingEnabled)})
	}
	return uses
}
