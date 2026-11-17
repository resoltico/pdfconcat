// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

// Package app is the transport-neutral PDF packet application boundary.
package app

import (
	"math"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/files"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/plan"
)

// Operation names a core packet use case in the application request.
type Operation = packet.Operation

// Supported packet operations are declared by the core.
const (
	Check = packet.Check
	Build = packet.Build
)

// MaxPlanBytes bounds a plan including an optional UTF-8 BOM.
const MaxPlanBytes = plan.MaxBytes

// Request declares one execution and its host-relative destinations.
type Request struct {
	Operation                  Operation
	Input                      Input
	WorkingDir, Output, Report string
	FitTo                      *Sheet
	Overwrite                  bool
	SourceDateEpoch            *string
	InspectionWorkers          int
}

// Input is a closed plan-file, plan-bytes or direct-item declaration.
type Input interface{ input() }

// PlanFile identifies a named regular plan.
type PlanFile struct{ Path string }

func (PlanFile) input() {}

// PlanBytes supplies a virtual plan with display provenance and a path base.
type PlanBytes struct {
	Data           []byte
	Label, BaseDir string
}

func (PlanBytes) input() {}

// Declared supplies all packet capabilities without a JSON exchange.
type Declared struct{ Declaration packet.Declaration }

func (Declared) input() {}

// Items declares direct source and blank operands.
type Items []Item

func (Items) input() {}

// Item is a closed direct source or blank declaration.
type Item interface{ item() }

// SourceItem declares a source with a required unique reference.
type SourceItem struct{ Ref, Path string }

func (SourceItem) item() {}

// BlankItem declares one page with the default style.
type BlankItem struct{ Ref string }

func (BlankItem) item() {}

// Sheet is a validated physical size.
type Sheet struct{ size packet.Size }

// ParseSheet uses the shared physical paper and dimension grammar.
func ParseSheet(text string) (Sheet, error) {
	size, err := packet.ParseSize(text, false)
	return Sheet{size: size}, err
}
func (s Sheet) valid() bool {
	return !math.IsNaN(s.size.Width) && !math.IsNaN(s.size.Height) && s.size.Width >= 1 && s.size.Width <= packet.MaxSide && s.size.Height >= 1 && s.size.Height <= packet.MaxSide
}
func validHostText(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }
func (r Request) valid() bool {
	if r.InspectionWorkers < 1 || r.Operation != Check && r.Operation != Build || !filepath.IsAbs(r.WorkingDir) || !validHostText(r.WorkingDir) || files.ValidatePath(r.WorkingDir) != nil || !validHostText(r.Output) || !validHostText(r.Report) || r.FitTo != nil && !r.FitTo.valid() {
		return false
	}
	switch input := r.Input.(type) {
	case PlanFile:
		return input.Path != "" && validHostText(input.Path)
	case PlanBytes:
		return validHostText(input.Label) && validHostText(input.BaseDir)
	case Declared:
		return true
	case Items:
		if len(input) == 0 {
			return false
		}
		refs := map[string]bool{}
		for _, item := range input {
			var ref string
			switch value := item.(type) {
			case SourceItem:
				ref = value.Ref
				if !validHostText(value.Path) {
					return false
				}
			case BlankItem:
				ref = value.Ref
			default:
				return false
			}
			if ref == "" || !validHostText(ref) || refs[ref] {
				return false
			}
			refs[ref] = true
		}
		return true
	default:
		return false
	}
}
