// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"
	"errors"
	"path/filepath"
	"slices"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/files"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/plan"
)

// PlanDocument retains original declaration bytes and physical input reservations.
// Static validation does not read fonts or PDFs and is not a successful Check.
type PlanDocument struct {
	data                []byte
	label, base, origin string
	reservations        []string
	valid               bool
}

// JSON returns owned original accepted bytes, including BOM and formatting.
func (d PlanDocument) JSON() []byte { return slices.Clone(d.data) }

// ReplaceJSON validates edits while preserving the opened physical origin and base.
func (d PlanDocument) ReplaceJSON(data []byte) (PlanDocument, []Diagnostic) {
	if !d.valid {
		return PlanDocument{}, invalidRequest("").Diagnostics
	}
	next, ds := DecodePlan(data, d.label, d.base)
	if len(ds) == 0 {
		next.origin = d.origin
	}
	return next, ds
}

// DecodePlan validates declarations without reading physical fonts or sources.
func DecodePlan(data []byte, label, absoluteBaseDir string) (PlanDocument, []Diagnostic) {
	if !metadata(label) || !metadata(absoluteBaseDir) || !filepath.IsAbs(absoluteBaseDir) || packet.ValidatePath(absoluteBaseDir) != nil {
		return PlanDocument{}, invalidRequest("").Diagnostics
	}
	j, ds := plan.Decode(data, label, absoluteBaseDir)
	if len(ds) > 0 {
		return PlanDocument{}, ds
	}
	return PlanDocument{data: slices.Clone(data), label: label, base: absoluteBaseDir, reservations: inputPaths(j), valid: true}, nil
}

// ReadPlan acquires and statically validates a declaration, retaining physical origin.
func ReadPlan(c context.Context, source PlanSource, workingDir string) (PlanDocument, []Diagnostic) {
	if !validSource(source) || !metadata(workingDir) || !filepath.IsAbs(workingDir) || packet.ValidatePath(workingDir) != nil {
		return PlanDocument{}, invalidRequest("").Diagnostics
	}
	data, label, base, origin, ds := acquirePlan(c, source, workingDir)
	if len(ds) > 0 {
		return PlanDocument{}, ds
	}
	d, ds := DecodePlan(data, label, base)
	if len(ds) == 0 {
		d.origin = origin
	}
	return d, ds
}
func acquirePlan(c context.Context, s PlanSource, cwd string) (data []byte, label, base, origin string, ds []Diagnostic) {
	base = cwd
	label = s.label
	var err error
	if s.kind == 1 {
		origin, err = packet.Path(cwd, s.path)
		label = origin
		base = filepath.Dir(origin)
	} else if s.base != "" {
		base, err = packet.Path(cwd, s.base)
	}
	if err != nil {
		return nil, label, base, origin, []Diagnostic{diag.New("path_invalid", err.Error())}
	}
	if err = c.Err(); err == nil {
		switch s.kind {
		case 1:
			data, err = files.Read(c, origin, plan.MaxBytes)
		case 2:
			data = slices.Clone(s.data)
		case 3:
			data, err = s.load(c, plan.MaxBytes)
			data = slices.Clone(data)
		}
	}
	code := diag.Code("plan_unreadable")
	switch {
	case c.Err() != nil:
		err = c.Err()
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
	case len(data) > plan.MaxBytes || errors.Is(err, files.ErrTooLarge):
		code = "plan_too_large"
		err = files.ErrTooLarge
	}
	if err != nil {
		return nil, label, base, origin, []Diagnostic{ioDiagnostic(code, err, label)}
	}
	return data, label, base, origin, nil
}
func inputPaths(j packet.Job) []string {
	leaves, _ := packet.Declarations(j)
	var paths []string
	for _, leaf := range leaves {
		if leaf.Kind == "source" {
			paths = append(paths, leaf.Source)
		}
		if leaf.Page != nil && leaf.Page.Text.Font != "default" {
			paths = append(paths, leaf.Page.Text.Font)
		}
	}
	return paths
}
