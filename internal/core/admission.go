// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"
	"strings"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/files"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/plan"
	"github.com/resoltico/pdfconcat/internal/core/internal/result"
)

// admissionOutcome retains known physical roles even if declaration acquisition fails.
type admissionOutcome struct {
	plan          packet.Plan
	roles         files.Files
	result        result.Result
	reportRefused bool
}

func admit(c context.Context, request Request) admissionOutcome {
	o := request.options
	input := request.input
	a := admissionOutcome{roles: files.Files{Overwrite: o.Overwrite}, result: result.New(string(request.operation))}
	// Reservations must precede cancellation and acquisition failures.
	switch input.kind {
	case 1:
		if input.source.kind == 1 {
			if path, err := packet.Path(o.WorkingDir, input.source.path); err == nil {
				a.roles.Inputs = append(a.roles.Inputs, path)
			}
		}
	case 2:
		a.roles.Inputs = append(a.roles.Inputs, input.document.reservations...)
		if input.document.origin != "" {
			a.roles.Inputs = append(a.roles.Inputs, input.document.origin)
		}
	case 3:
		for _, item := range input.items {
			if item.kind == 1 {
				if path, err := packet.Path(o.WorkingDir, item.path); err == nil {
					a.roles.Inputs = append(a.roles.Inputs, path)
				}
			}
		}
	}
	if o.Report != "" {
		path, err := packet.Path(o.WorkingDir, o.Report)
		if err != nil {
			a.result.Diagnostics = append(a.result.Diagnostics, diag.New(diag.PathInvalid, err.Error()))
			return a
		}
		a.roles.Report = path
		a.result.Report = &result.Report{Path: path}
	}
	if o.Output != "" {
		path, err := packet.Path(o.WorkingDir, o.Output)
		if err != nil {
			a.result.Diagnostics = append(a.result.Diagnostics, diag.New(diag.PathInvalid, err.Error()))
			return a
		}
		a.roles.Output = path
		a.result.Output = &result.Output{Path: path}
	}
	if err := c.Err(); err != nil {
		a.result.Diagnostics = append(a.result.Diagnostics, diag.New(diag.Interrupted, err.Error()))
		return a
	}
	var ds []diag.Diagnostic
	switch input.kind {
	case 1:
		data, label, base, _, faults := acquirePlan(c, input.source, o.WorkingDir)
		ds = faults
		if len(ds) == 0 {
			a.plan, ds = plan.Decode(data, label, base)
		}
	case 2:
		a.plan, ds = plan.Decode(input.document.data, input.document.label, input.document.base)
	case 3:
		items := directReferences(input.items)
		direct := make([]directItem, len(items))
		for n, i := range items {
			direct[n] = directItem{Path: i.path, Reference: i.reference, Blank: i.kind == 2}
		}
		a.plan, ds = directPlan(direct, o.WorkingDir)
	}
	a.result.Diagnostics = append(a.result.Diagnostics, ds...)
	if hasErrors(ds) {
		return a
	}
	a.roles.InputsKnown = true
	a.roles.Inputs = append(a.roles.Inputs, inputPaths(a.plan)...)
	if o.Output != "" {
		a.plan.Output = a.roles.Output
	} else {
		a.roles.Output = a.plan.Output
	}
	if a.plan.Output != "" {
		a.result.Output = &result.Output{Path: a.plan.Output}
	}
	if o.FitTo != nil {
		size := o.FitTo.size
		a.plan.FitTo = &size
	}
	a.result.FitTo = a.plan.FitTo
	a.result.PageNumbers = a.plan.PageNumbers
	leaves, sections := a.plan.Leaves(), a.plan.Sections()
	a.result.Pages = &result.Pages{}
	a.result.Sections = reportSections(sections, false)
	for _, leaf := range leaves {
		a.result.Parts = append(a.result.Parts, declaredReportPart(leaf))
	}
	if request.operation == Build && a.plan.Output == "" {
		a.result.Diagnostics = append(a.result.Diagnostics, diag.New(diag.UsageMissingOutput, "Give -o FILE or plan output."))
	}
	refusals := a.roles.Check()
	a.result.Diagnostics = append(a.result.Diagnostics, refusals...)
	for _, d := range refusals {
		if strings.HasPrefix(d.Code.String(), "report_") {
			a.reportRefused = true
		}
	}
	return a
}

// directItem is the frontend's source or blank operand.
type directItem struct {
	Path, Reference string
	Blank           bool
}

func directPlan(items []directItem, base string) (packet.Plan, []diag.Diagnostic) {
	var declared []packet.Item
	var ds []diag.Diagnostic
	generated := 0
	for index, item := range items {
		if item.Blank {
			generated++
		}
		if index >= packet.MaxEntries || generated > packet.MaxGeneratedPages {
			ds = append(ds, diag.New(diag.PlanLimit, "Direct items exceed the authored item or generated-page budget.", item.Reference))
			break
		}
		if item.Blank {
			declared = append(declared, &packet.PageItem{Ref: item.Reference, Style: packet.Defaults(), Count: 1})
			continue
		}
		path, err := packet.Path(base, item.Path)
		if err != nil {
			ds = append(ds, diag.New(diag.PathInvalid, err.Error(), item.Reference))
			continue
		}
		declared = append(declared, &packet.SourceItem{Ref: item.Reference, Path: path})
	}
	return packet.NewPlan("", nil, nil, packet.Defaults(), declared), ds
}
