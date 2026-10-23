// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/files"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/plan"
)

type admission struct {
	plan          *packet.Plan
	roles         files.Files
	diagnostics   []diag.Diagnostic
	reportRefused bool
}

func admit(ctx context.Context, req Request) *admission {
	a := &admission{roles: files.Files{Overwrite: req.Overwrite}}
	// Reserve physical inputs before cancellation or acquisition can fail.
	switch input := req.Input.(type) {
	case PlanFile:
		if path, err := packet.Path(req.WorkingDir, input.Path); err == nil {
			a.roles.Inputs = append(a.roles.Inputs, path)
		}
	case Items:
		for _, item := range input {
			if source, ok := item.(SourceItem); ok {
				if path, err := packet.Path(req.WorkingDir, source.Path); err == nil {
					a.roles.Inputs = append(a.roles.Inputs, path)
				}
			}
		}
	}
	if req.Report != "" {
		path, err := packet.Path(req.WorkingDir, req.Report)
		if err != nil {
			a.diagnostics = append(a.diagnostics, diag.New(diag.PathInvalid, err.Error()))
			return a
		}
		a.roles.Report = path
	}
	if req.Output != "" {
		path, err := packet.Path(req.WorkingDir, req.Output)
		if err != nil {
			a.diagnostics = append(a.diagnostics, diag.New(diag.PathInvalid, err.Error()))
			return a
		}
		a.roles.Output = path
	}
	if err := ctx.Err(); err != nil {
		a.diagnostics = append(a.diagnostics, diag.New(diag.Interrupted, err.Error()))
		return a
	}
	var declared packet.Plan
	var ds []diag.Diagnostic
	switch input := req.Input.(type) {
	case PlanFile:
		path, err := packet.Path(req.WorkingDir, input.Path)
		if err != nil {
			ds = []diag.Diagnostic{diag.New(diag.PathInvalid, err.Error())}
			break
		}
		data, err := files.Read(ctx, path, MaxPlanBytes)
		if err != nil {
			code := diag.PlanUnreadable
			if errors.Is(err, files.ErrTooLarge) {
				code = diag.PlanTooLarge
			}
			ds = []diag.Diagnostic{ioDiagnostic(code, err, path)}
			break
		}
		declared, ds = plan.Decode(data, path, filepath.Dir(path))
	case PlanBytes:
		base := req.WorkingDir
		var err error
		if input.BaseDir != "" {
			base, err = packet.Path(req.WorkingDir, input.BaseDir)
		}
		if err != nil {
			ds = []diag.Diagnostic{diag.New(diag.PathInvalid, err.Error())}
			break
		}
		if len(input.Data) > MaxPlanBytes {
			ds = []diag.Diagnostic{ioDiagnostic(diag.PlanTooLarge, files.ErrTooLarge, input.Label)}
			break
		}
		declared, ds = plan.Decode(slices.Clone(input.Data), input.Label, base)
	case Items:
		declared, ds = directPlan(input, req.WorkingDir)
	}
	a.diagnostics = append(a.diagnostics, ds...)
	if hasErrors(ds) {
		return a
	}
	a.plan = &declared
	a.roles.InputsKnown = true
	a.roles.Inputs = append(a.roles.Inputs, inputPaths(declared)...)
	if req.Output != "" {
		declared.Output = a.roles.Output
	} else {
		a.roles.Output = declared.Output
	}
	if req.FitTo != nil {
		size := req.FitTo.size
		declared.FitTo = &size
	}
	if req.Operation == Build && declared.Output == "" {
		a.diagnostics = append(a.diagnostics, diag.New(diag.UsageMissingOutput, "Give -o FILE or plan output."))
	}
	refusals := a.roles.Check()
	a.diagnostics = append(a.diagnostics, refusals...)
	for _, d := range refusals {
		if strings.HasPrefix(d.Code.String(), "report_") {
			a.reportRefused = true
		}
	}
	return a
}
func directPlan(items Items, base string) (packet.Plan, []diag.Diagnostic) {
	var declared []packet.Item
	var ds []diag.Diagnostic
	generated := 0
	for index, item := range items {
		blank, isBlank := item.(BlankItem)
		if isBlank {
			generated++
		}
		if index >= packet.MaxEntries || generated > packet.MaxGeneratedPages {
			ref := ""
			if isBlank {
				ref = blank.Ref
			} else if source, ok := item.(SourceItem); ok {
				ref = source.Ref
			}
			ds = append(ds, diag.New(diag.PlanLimit, "Direct items exceed the authored item or generated-page budget.", ref))
			break
		}
		if isBlank {
			declared = append(declared, &packet.PageItem{Ref: blank.Ref, Style: packet.Defaults(), Count: 1})
			continue
		}
		source, ok := item.(SourceItem)
		if !ok {
			continue
		} // Request validation seals the variants.
		path, err := packet.Path(base, source.Path)
		if err != nil {
			ds = append(ds, diag.New(diag.PathInvalid, err.Error(), source.Ref))
			continue
		}
		declared = append(declared, &packet.SourceItem{Ref: source.Ref, Path: path})
	}
	return packet.NewPlan("", nil, nil, packet.Defaults(), declared), ds
}
func inputPaths(p packet.Plan) []string {
	var paths []string
	for _, item := range p.Leaves() {
		if item.Kind == packet.PartSource {
			paths = append(paths, item.Source)
		}
		if item.Page != nil && item.Page.Text.Font.Value.Path != "" {
			paths = append(paths, item.Page.Text.Font.Value.Path)
		}
	}
	return paths
}
