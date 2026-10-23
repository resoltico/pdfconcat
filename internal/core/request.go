// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package core is the transport-neutral PDF packet application boundary.
package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/result"
)

// Operation is a supported packet use case.
type Operation string

// Supported packet operations.
const (
	Check Operation = "check"
	Build Operation = "build"
)

// Request owns a declaration and host-scoped execution options.
type Request struct {
	operation Operation
	input     Input
	options   Options
}

// Input is one closed declaration variant.
type Input struct {
	kind     uint8
	source   PlanSource
	document PlanDocument
	items    []Item
}

// PlanSource is a named file, owned bytes or synchronous host loader.
type PlanSource struct {
	kind              uint8
	path, label, base string
	data              []byte
	load              PlanLoader
}

// PlanLoader returns stable owned bytes and obeys the context; it owns its lifetime.
type PlanLoader func(context.Context, int64) ([]byte, error)

// Item is a direct source occurrence or plain generated page.
type Item struct {
	kind            uint8
	path, reference string
}

// Sheet is a validated physical sheet size.
type Sheet struct{ size packet.Size }

// Options supplies host-relative paths and optional overrides.
type Options struct {
	WorkingDir, Output, Report string
	FitTo                      *Sheet
	Overwrite                  bool
}

// NewRequest copies declaration ownership and optional override values.
func NewRequest(operation Operation, input Input, options Options) Request {
	input.items = slices.Clone(input.items)
	input.source.data = slices.Clone(input.source.data)
	if options.FitTo != nil {
		value := *options.FitTo
		options.FitTo = &value
	}
	return Request{operation: operation, input: input, options: options}
}

// FilePlan identifies a named physical input, resolved against WorkingDir.
func FilePlan(path string) PlanSource { return PlanSource{kind: 1, path: path} }

// BytesPlan owns a copy of a virtual declaration; label is display provenance.
func BytesPlan(data []byte, label, baseDir string) PlanSource {
	return PlanSource{kind: 2, data: slices.Clone(data), label: label, base: baseDir}
}

// LoadPlan uses one synchronous host acquisition per execution.
func LoadPlan(label, baseDir string, load PlanLoader) PlanSource {
	return PlanSource{kind: 3, label: label, base: baseDir, load: load}
}

// FromPlan selects declaration acquisition.
func FromPlan(source PlanSource) Input {
	source.data = slices.Clone(source.data)
	return Input{kind: 1, source: source}
}

// FromDocument selects an opaque statically validated declaration.
func FromDocument(document PlanDocument) Input { return Input{kind: 2, document: document} }

// FromItems owns a copy of direct source and blank declarations.
func FromItems(items ...Item) Input { return Input{kind: 3, items: slices.Clone(items)} }

// Source declares a source occurrence with opaque frontend provenance.
func Source(path, reference string) Item { return Item{kind: 1, path: path, reference: reference} }

// Blank declares one plain generated page.
func Blank(reference string) Item { return Item{kind: 2, reference: reference} }

// ParseSheet uses the shared physical paper and dimension grammar.
func ParseSheet(text string) (Sheet, error) {
	size, err := packet.ParseSize(text, false)
	return Sheet{size: size}, err
}

// SheetPoints validates explicit physical dimensions.
func SheetPoints(width, height float64) (Sheet, error) {
	s := Sheet{size: packet.Size{Width: width, Height: height}}
	if !s.valid() {
		return Sheet{}, errors.New("each page side must be 1–14400 points")
	}
	return s, nil
}
func (s Sheet) valid() bool {
	return !math.IsNaN(s.size.Width) && !math.IsNaN(s.size.Height) && s.size.Width >= 1 && s.size.Width <= packet.MaxSide && s.size.Height >= 1 && s.size.Height <= packet.MaxSide
}
func metadata(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }
func (r Request) valid() bool {
	o := r.options
	if r.operation != Check && r.operation != Build || !filepath.IsAbs(o.WorkingDir) || !metadata(o.WorkingDir) || packet.ValidatePath(o.WorkingDir) != nil || !metadata(o.Output) || !metadata(o.Report) || o.FitTo != nil && !o.FitTo.valid() {
		return false
	}
	switch r.input.kind {
	case 1:
		return validSource(r.input.source)
	case 2:
		return r.input.document.valid
	case 3:
		if len(r.input.items) == 0 {
			return false
		}
		refs := map[string]bool{}
		for _, i := range r.input.items {
			if i.kind != 1 && i.kind != 2 || !metadata(i.path) || !metadata(i.reference) || i.reference != "" && refs[i.reference] {
				return false
			}
			if i.reference != "" {
				refs[i.reference] = true
			}
		}
		return true
	default:
		return false
	}
}
func validSource(s PlanSource) bool {
	if !metadata(s.path) || !metadata(s.label) || !metadata(s.base) {
		return false
	}
	switch s.kind {
	case 1:
		return s.path != ""
	case 2:
		return true
	case 3:
		return s.load != nil
	default:
		return false
	}
}
func invalidRequest(hint Operation) Result {
	if hint != Check && hint != Build {
		hint = ""
	}
	r := result.New(string(hint))
	r.Diagnostics = []Diagnostic{diag.New(diag.RequestInvalid, "Invalid application request.")}
	return r.Full()
}
func directReferences(items []Item) []Item {
	items = slices.Clone(items)
	reserved := map[string]bool{}
	for _, i := range items {
		if i.reference != "" {
			reserved[i.reference] = true
		}
	}
	next := 1
	for n := range items {
		if items[n].reference != "" {
			continue
		}
		for reserved[fmt.Sprintf("item:%d", next)] {
			next++
		}
		ref := fmt.Sprintf("item:%d", next)
		next++
		reserved[ref] = true
		items[n].reference = ref
	}
	return items
}
