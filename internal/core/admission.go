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
	job           packet.Job
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
			a.job, ds = plan.Decode(data, label, base)
		}
	case 2:
		a.job, ds = plan.Decode(input.document.data, input.document.label, input.document.base)
	case 3:
		items := directReferences(input.items)
		direct := make([]plan.DirectItem, len(items))
		for n, i := range items {
			direct[n] = plan.DirectItem{Path: i.path, Reference: i.reference, Blank: i.kind == 2}
		}
		a.job, ds = plan.Direct(direct, o.WorkingDir)
	}
	a.result.Diagnostics = append(a.result.Diagnostics, ds...)
	if hasErrors(ds) {
		return a
	}
	a.roles.InputsKnown = true
	a.roles.Inputs = append(a.roles.Inputs, inputPaths(a.job)...)
	if o.Output != "" {
		a.job.Output = a.roles.Output
	} else {
		a.roles.Output = a.job.Output
	}
	if a.job.Output != "" {
		a.result.Output = &result.Output{Path: a.job.Output}
	}
	if o.FitTo != nil {
		size := o.FitTo.size
		a.job.FitTo = &size
	}
	a.result.FitTo = a.job.FitTo
	a.result.PageNumbers = a.job.PageNumbers
	leaves, sections := packet.Declarations(a.job)
	a.result.Pages = &result.Pages{}
	a.result.Sections = reportSections(sections, false)
	for _, leaf := range leaves {
		a.result.Parts = append(a.result.Parts, declaredReportPart(leaf))
	}
	if request.operation == Build && a.job.Output == "" {
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
