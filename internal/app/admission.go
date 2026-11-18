// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/files"
	"github.com/resoltico/pdfconcat/internal/plan"
)

type admission struct {
	plan          *packet.Plan
	roles         files.Roles
	diagnostics   []diag.Diagnostic
	reportRefused bool
}

func admit(ctx context.Context, req Request) *admission {
	a := &admission{roles: files.Roles{Overwrite: req.Overwrite}}
	// Reserve physical inputs before cancellation or acquisition can fail.
	switch input := req.Input.(type) {
	case PlanFile:
		if path, err := files.Path(req.WorkingDir, input.Path); err == nil {
			a.roles.Inputs = append(a.roles.Inputs, path)
		}
	case Items:
		for _, item := range input {
			if source, ok := item.(SourceItem); ok {
				if path, err := files.Path(req.WorkingDir, source.Path); err == nil {
					a.roles.Inputs = append(a.roles.Inputs, path)
				}
			}
		}
	}
	if req.Report != "" {
		path, err := files.Path(req.WorkingDir, req.Report)
		if err != nil {
			a.diagnostics = append(a.diagnostics, diag.Report(diag.PathInvalid, diag.InvalidPathFailure(err.Error())))
			return a
		}
		a.roles.Report = path
	}
	if req.Output != "" {
		path, err := files.Path(req.WorkingDir, req.Output)
		if err != nil {
			a.diagnostics = append(a.diagnostics, diag.Report(diag.PathInvalid, diag.InvalidPathFailure(err.Error())))
			return a
		}
		a.roles.Output = path
	}
	var typed *packet.Plan
	if input, ok := req.Input.(Declared); ok {
		owned, ds := packet.ValidateDeclaration(input.Declaration)
		if len(ds) == 0 {
			owned, ds = files.ResolveDeclaration(owned, req.WorkingDir)
		}
		if len(ds) > 0 {
			a.diagnostics = append(a.diagnostics, ds...)
			return a
		}
		prepared, ds := packet.AdmitDeclaration(owned)
		if len(ds) > 0 {
			a.diagnostics = append(a.diagnostics, ds...)
			return a
		}
		typed = &prepared
		for _, resource := range prepared.Inputs() {
			a.roles.Inputs = append(a.roles.Inputs, resource.Path)
		}
		a.roles.InputsKnown = true
		if req.Output == "" {
			a.roles.Output = prepared.Output()
		}
	}
	if err := ctx.Err(); err != nil {
		a.diagnostics = append(a.diagnostics, diag.Report(diag.Interrupted, diag.InterruptedFailure(err.Error())))
		return a
	}
	var declared packet.Plan
	var ds []diag.Diagnostic
	switch input := req.Input.(type) {
	case PlanFile:
		path, err := files.Path(req.WorkingDir, input.Path)
		if err != nil {
			ds = []diag.Diagnostic{diag.Report(diag.PathInvalid, diag.InvalidPathFailure(err.Error()))}
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
		declared, ds = plan.Decode(data, path, filepath.Dir(path), files.Path)
	case PlanBytes:
		base := req.WorkingDir
		var err error
		if input.BaseDir != "" {
			base, err = files.Path(req.WorkingDir, input.BaseDir)
		}
		if err != nil {
			ds = []diag.Diagnostic{diag.Report(diag.PathInvalid, diag.InvalidPathFailure(err.Error()))}
			break
		}
		if len(input.Data) > MaxPlanBytes {
			ds = []diag.Diagnostic{ioDiagnostic(diag.PlanTooLarge, files.ErrTooLarge, input.Label)}
			break
		}
		declared, ds = plan.Decode(slices.Clone(input.Data), input.Label, base, files.Path)
	case Declared:
		declared = *typed
	case Items:
		declared, ds = directPlan(input, req.WorkingDir)
	}
	a.diagnostics = append(a.diagnostics, ds...)
	if hasErrors(ds) {
		return a
	}
	effective := declared.Declaration()
	if effective.Structure == "" {
		effective.Structure = packet.StructurePreserve
	}
	if effective.Metadata == nil {
		effective.Metadata = &packet.Metadata{}
	}
	if effective.Timestamp == nil && req.SourceDateEpoch != nil {
		value := *req.SourceDateEpoch
		n, e := strconv.ParseInt(value, 10, 64)
		digits := value != ""
		for _, r := range value {
			digits = digits && r >= '0' && r <= '9'
		}
		if e != nil || !digits || n < 0 || n > packet.MaxTimestamp {
			a.diagnostics = append(a.diagnostics, diag.Report(diag.PlanValue, diag.EffectiveSOURCEDATEEPOCHMustBeUnixSecondsFromThrough()))
		} else {
			effective.Timestamp = &n
		}
	}
	a.roles.InputsKnown = true
	seenInputs := map[string]bool{}
	for _, path := range a.roles.Inputs {
		seenInputs[path] = true
	}
	for _, resource := range declared.Inputs() {
		if !seenInputs[resource.Path] {
			a.roles.Inputs = append(a.roles.Inputs, resource.Path)
			seenInputs[resource.Path] = true
		}
	}
	if req.Output != "" {
		effective.Output = a.roles.Output
	} else {
		a.roles.Output = effective.Output
	}
	if req.Fitting != nil {
		effective.Fitting = req.Fitting
	}
	if req.Operation == Build && effective.Output == "" {
		a.diagnostics = append(a.diagnostics, diag.Report(diag.OutputRequired, diag.AnOutputDestinationIsRequiredForBuild()))
	}
	var admissionDS []diag.Diagnostic
	declared, admissionDS = packet.AdmitDeclaration(effective)
	a.diagnostics = append(a.diagnostics, admissionDS...)
	if len(admissionDS) > 0 {
		return a
	}
	a.plan = &declared
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
			ds = append(ds, diag.Report(diag.PlanLimit, diag.DirectItemsExceedTheAuthoredItemOrGeneratedPageBudget(), ref))
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
		path, err := files.Path(base, source.Path)
		if err != nil {
			ds = append(ds, diag.Report(diag.PathInvalid, diag.InvalidPathFailure(err.Error()), source.Ref))
			continue
		}
		declared = append(declared, &packet.SourceItem{Ref: source.Ref, Path: path})
	}
	owned, validation := packet.ValidateDeclaration(packet.Declaration{Items: declared})
	ds = append(ds, validation...)
	if len(ds) > 0 {
		return packet.Plan{}, ds
	}
	return packet.AdmitDeclaration(owned)
}
