// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"io"
	"sync"

	"github.com/resoltico/pdfconcat/internal/compose"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/files"
	"github.com/resoltico/pdfconcat/internal/pdf"
)

type inspection struct {
	fonts       compose.Fonts
	sources     []files.InputIdentity
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
	structure       packet.StructurePolicy
	optionalContent packet.OptionalContentPolicy
	path            string
	leaves          []int
	requests        []pdf.SelectionRequest
}
type sourceInspection struct {
	identity    files.InputIdentity
	file        *pdf.SourceFacts
	views       []*pdf.OccurrenceFacts
	diagnostics []diag.Diagnostic
}

const sourceMemoryThreshold = 1 << 20

func inspect(ctx context.Context, p packet.Plan, workerCount int) *inspection {
	fonts, ds := loadFonts(ctx, p)
	out := &inspection{fonts: fonts, diagnostics: ds, byLeaf: make([]inspectedLeaf, p.LeafCount())}
	if hasErrors(ds) {
		return out
	}

	uses := sourceUses(p)
	results := make([]sourceInspection, len(uses))
	type job struct{ index, weight int }
	jobs := make(chan job)
	// Reserve estimated parser work independently of the CPU worker limit.
	// Backend decode and object limits still govern individual admissions.
	const workUnits = 12
	permits := make(chan struct{}, workUnits)
	release := func(weight int) {
		for range weight {
			<-permits
		}
	}
	var workers sync.WaitGroup
	for range min(workerCount, len(uses)) {
		workers.Go(func() {
			for job := range jobs {
				results[job.index] = inspectSource(ctx, uses[job.index])
				release(job.weight)
			}
		})
	}
dispatch:
	for index, use := range uses {
		// Allow space for object dictionaries and expanded streams as well as
		// the encoded file. Oversized estimates run alone.
		weight := int(min(int64(workUnits), 1+files.SizeHint(use.path)/(2<<20)))
		for held := range weight {
			select {
			case permits <- struct{}{}:
			case <-ctx.Done():
				release(held)
				break dispatch
			}
		}
		select {
		case jobs <- job{index, weight}:
		case <-ctx.Done():
			release(weight)
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
			out.sources = append(out.sources, measured.identity)
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
func inspectSource(ctx context.Context, use sourceUse) (out sourceInspection) {
	items := make([]string, len(use.requests))
	for i, request := range use.requests {
		items[i] = request.Item
	}
	recordFailure := func(err error) {
		if err != nil {
			d := ioDiagnostic(diag.SourceUnreadable, err, use.path)
			d.Items = items
			out.diagnostics = append(out.diagnostics, d)
		}
	}
	source, err := files.Acquire(ctx, use.path, sourceMemoryThreshold)
	if err != nil {
		recordFailure(err)
		return out
	}
	defer func() { recordFailure(source.Close()) }()
	input, err := source.Open()
	if err != nil {
		recordFailure(err)
		return out
	}
	defer func() { recordFailure(input.Close()) }()
	return inspectSnapshot(ctx, use, source, input, items)
}

func inspectSnapshot(ctx context.Context, use sourceUse, source *files.Snapshot, input io.ReadSeeker, items []string) sourceInspection {
	facts, views, ds := pdf.InspectReader(ctx, input, source.Digest(), source.Size(), pdf.SourcePolicies{Structure: use.structure, OptionalContent: use.optionalContent}, use.requests)
	for i := range ds {
		ds[i].File = use.path
		if len(ds[i].Items) == 0 {
			ds[i].Items = items
		}
	}
	return sourceInspection{identity: source.Identity(), file: &facts, views: views, diagnostics: ds}
}
func sourceUses(p packet.Plan) []sourceUse {
	var uses []sourceUse
	index := map[string]int{}
	fitting := p.Fitting()
	for item := range p.Sources() {
		slot, ok := index[item.Source]
		if !ok {
			slot = len(uses)
			index[item.Source] = slot
			uses = append(uses, sourceUse{path: item.Source, structure: p.Structure(), optionalContent: p.OptionalContent()})
		}
		uses[slot].leaves = append(uses[slot].leaves, item.Index)
		uses[slot].requests = append(uses[slot].requests, pdf.SelectionRequest{Fitting: fitting, Item: item.Ref, Pages: item.Selection})
	}
	return uses
}
