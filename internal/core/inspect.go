// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"
	"runtime"
	"sync"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/files"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

type inspection struct {
	fonts       map[packet.Font]*typeset.Font
	occurrences []*pdf.Occurrence
	shapes      []packet.SourceFacts
	diagnostics []diag.Diagnostic
}
type sourceUse struct {
	path     string
	leaves   []int
	requests []pdf.SelectionRequest
}
type sourceInspection struct {
	file        *pdf.SourceFacts
	views       []*pdf.OccurrenceFacts
	diagnostics []diag.Diagnostic
}

func inspect(ctx context.Context, p packet.Plan) *inspection {
	fonts, ds := loadFonts(ctx, p)
	out := &inspection{fonts: fonts, diagnostics: ds, occurrences: make([]*pdf.Occurrence, len(p.Leaves())), shapes: make([]packet.SourceFacts, len(p.Leaves()))}
	if hasErrors(ds) {
		return out
	}

	uses := sourceUses(p)
	results := make([]sourceInspection, len(uses))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(uses)) {
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
		if measured.file != nil {
			for index, leaf := range use.leaves {
				var view *pdf.OccurrenceFacts
				if index < len(measured.views) {
					view = measured.views[index]
				}
				out.occurrences[leaf] = &pdf.Occurrence{File: measured.file, Facts: view}
				if view != nil {
					out.shapes[leaf] = view.SourceFacts
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
	facts, views, ds := pdf.Inspect(ctx, data, use.requests)
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
			uses = append(uses, sourceUse{path: item.Source})
		}
		uses[slot].leaves = append(uses[slot].leaves, leafIndex)
		uses[slot].requests = append(uses[slot].requests, pdf.SelectionRequest{Item: item.Ref, Pages: item.Selection, MeasureFooter: p.PageNumbers != nil && p.FitTo == nil})
	}
	return uses
}
