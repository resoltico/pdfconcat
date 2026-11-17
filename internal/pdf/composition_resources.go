// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"maps"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/matrix"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Selected resources are immutable graphics-state bindings. Resource names are
// resolved at selection, never reinterpreted in an invoked program's scope.
type selectedFont struct {
	object types.Object
	size   float64
}
type selectedPattern struct {
	object                     types.Object
	resources                  types.Dict
	anchorSource, anchorFitted matrix.Matrix
}

type compositionProgramRole uint8

const (
	formProgram compositionProgramRole = iota
	appearanceProgram
	tilingPatternProgram
	softMaskProgram
	type3GlyphProgram
)

func paintChannels(op string, textMode int) (fill, stroke bool) {
	switch op {
	case "S", "s":
		return false, true
	case "f", "F", "f*":
		return true, false
	case "B", "B*", "b", "b*":
		return true, true
	case "Tj", "TJ", "'", "\"":
		switch textMode {
		case 0, 4:
			return true, false
		case 1, 5:
			return false, true
		case 2, 6:
			return true, true
		}
	}
	return false, false
}
func textShowing(op string) bool { return op == "Tj" || op == "TJ" || op == "'" || op == "\"" }

// paintResources dispatches actual resource consumers. Type3 programs execute
// their own paint operators, so inherited patterns are not consumed at Tj itself.
func (a *compositionInspector) paintResources(state compositionState, owner types.IndirectRef, depth int, op string) error {
	if textShowing(op) && state.textMode != 3 && state.font != nil {
		font, err := a.pdf.DereferenceDict(state.font.object)
		if err != nil {
			return err
		}
		if font != nil {
			subtype, err := nameValue(a.pdf, font["Subtype"])
			if err != nil {
				return err
			}
			if subtype == "Type3" {
				m, err := objectMatrix(a.pdf, font["FontMatrix"])
				if err != nil {
					return a.failure(owner, "Font.FontMatrix")
				}
				textScale := matrix.Matrix{{state.font.size * state.horizontalScale, 0, 0}, {0, state.font.size, 0}, {0, state.textRise, 1}}
				glyphState := state.concat(textScale).concat(m)
				if !a.certified(&glyphState) {
					return a.failure(owner, "Font.FontMatrix")
				}
				glyphs, err := a.pdf.DereferenceDict(font["CharProcs"])
				if err != nil {
					return err
				}
				child := a.pageResources
				if font["Resources"] != nil {
					child, err = a.pdf.DereferenceDict(font["Resources"])
					if err != nil {
						return err
					}
				}
				for _, name := range slices.Sorted(maps.Keys(glyphs)) {
					if err = a.program(type3GlyphProgram, glyphs[name], child, glyphState, owner, "Font.CharProcs", depth); err != nil {
						return err
					}
				}
				return nil
			}
		}
	}
	fill, stroke := paintChannels(op, state.textMode)
	var patterns []*selectedPattern
	if fill && state.fillPattern != nil {
		patterns = append(patterns, state.fillPattern)
	}
	if stroke && state.strokePattern != nil && (!fill || state.strokePattern != state.fillPattern) {
		patterns = append(patterns, state.strokePattern)
	}
	for _, binding := range patterns {
		value, err := a.pdf.Dereference(binding.object)
		if err != nil {
			return err
		}
		anchor := compositionState{source: binding.anchorSource, fitted: binding.anchorFitted, horizontalScale: 1}
		switch p := value.(type) {
		case types.StreamDict:
			if err = a.program(tilingPatternProgram, binding.object, binding.resources, anchor, owner, "Pattern", depth); err != nil {
				return err
			}
		case types.Dict:
			m, err := objectMatrix(a.pdf, p["Matrix"])
			if err != nil {
				return a.failure(owner, "Pattern.Matrix")
			}
			placed := anchor.concat(m)
			if !a.certified(&placed) {
				return a.failure(owner, "Pattern.Matrix")
			}
		default:
			return a.failure(owner, "Pattern")
		}
	}
	return nil
}

func (a *compositionInspector) appearances(page pageGeometry, plan pageFitting) error {
	annotations, err := a.pdf.DereferenceArray(page.dict["Annots"])
	if err != nil {
		return err
	}
	for _, object := range annotations {
		d, err := a.pdf.DereferenceDict(object)
		if err != nil {
			return err
		}
		if d == nil {
			continue
		}
		if d["F"] != nil {
			flags, e := a.pdf.DereferenceNumber(d["F"])
			if e != nil {
				return e
			}
			if int(flags)&int(model.AnnHidden) != 0 {
				continue
			}
		}
		ap, err := a.pdf.DereferenceDict(d["AP"])
		if err != nil {
			return err
		}
		if ap == nil {
			continue
		}
		rect, err := readRectangle(a.pdf, d["Rect"])
		if err != nil {
			return err
		}
		outer, err := annotationTransform(a.pdf, d, *rect, plan.Matrix, plan.Scale)
		if err != nil {
			return err
		}
		old := a.outer
		a.outer = outer
		err = func() error {
			for _, key := range []string{"N", "R", "D"} {
				value, err := a.pdf.Dereference(ap[key])
				if err != nil {
					return err
				}
				if value == nil {
					continue
				}
				objects := []types.Object{ap[key]}
				if states, ok := value.(types.Dict); ok {
					objects = nil
					for _, name := range slices.Sorted(maps.Keys(states)) {
						objects = append(objects, states[name])
					}
				}
				for _, obj := range objects {
					stream, _, err := a.pdf.DereferenceStreamDict(obj)
					if err != nil {
						return err
					}
					if stream == nil {
						continue
					}
					placement, painted, err := appearancePlacement(a.pdf, *stream, *rect)
					if err != nil {
						return err
					}
					if !painted {
						continue
					}
					state := compositionState{source: matrix.IdentMatrix, fitted: outer, horizontalScale: 1}.concat(placement)
					if err = a.program(appearanceProgram, obj, page.attrs.Resources, state, page.ref, "AP."+key, 0); err != nil {
						return err
					}
				}
			}
			return nil
		}()
		a.outer = old
		if err != nil {
			return err
		}
	}
	return nil
}
