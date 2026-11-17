// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"maps"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/matrix"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// paintResources follows retained matrix consumers at use, with the incoming
// state. No object-reference-only certificate is shared across invocations.
func (a *compositionInspector) paintResources(resources types.Dict, state, entry compositionState, owner types.IndirectRef, depth int, op string) error {
	patterns, err := a.pdf.DereferenceDict(resources["Pattern"])
	if err != nil {
		return err
	}
	for _, name := range []types.Name{state.fillPattern, state.strokePattern} {
		if name == "" {
			continue
		}
		object := patterns[string(name)]
		value, err := a.pdf.Dereference(object)
		if err != nil {
			return err
		}
		switch p := value.(type) {
		case types.StreamDict:
			// Pattern matrices are anchored to the scope's default space. Also check
			// current space: painting consumers may combine it before pattern setup.
			if err = a.form(object, resources, entry, owner, "Pattern", depth); err != nil {
				return err
			}
			if state.source != entry.source || state.fitted != entry.fitted {
				if err = a.form(object, resources, state, owner, "Pattern", depth); err != nil {
					return err
				}
			}
		case types.Dict:
			m, err := objectMatrix(a.pdf, p["Matrix"])
			if err != nil {
				return a.failure(owner, "Pattern.Matrix")
			}
			p1, p2 := entry.concat(m), state.concat(m)
			if !a.certified(&p1) || !a.certified(&p2) {
				return a.failure(owner, "Pattern.Matrix")
			}
		default:
			return a.failure(owner, "Pattern")
		}
	}
	if op != "Tj" && op != "TJ" && op != "'" && op != "\"" {
		return nil
	}
	if state.font == "" {
		return nil
	} // existing source resource policy owns missing bindings
	fonts, err := a.pdf.DereferenceDict(resources["Font"])
	if err != nil {
		return err
	}
	font, err := a.pdf.DereferenceDict(fonts[string(state.font)])
	if err != nil {
		return err
	}
	if font == nil {
		return nil
	}
	if font["FontMatrix"] != nil {
		m, err := objectMatrix(a.pdf, font["FontMatrix"])
		if err != nil {
			return a.failure(owner, "Font.FontMatrix")
		}
		state = state.concat(m)
		if !a.certified(&state) {
			return a.failure(owner, "Font.FontMatrix")
		}
	}
	subtype, err := nameValue(a.pdf, font["Subtype"])
	if err != nil {
		return err
	}
	if subtype != "Type3" {
		return nil
	}
	glyphs, err := a.pdf.DereferenceDict(font["CharProcs"])
	if err != nil {
		return err
	}
	child := resources
	if font["Resources"] != nil {
		child, err = a.pdf.DereferenceDict(font["Resources"])
		if err != nil {
			return err
		}
	}
	// Glyph resolution is not reimplemented here: certify all retained programs
	// of a used Type3 font conservatively, under this invocation's font matrix.
	for _, name := range slices.Sorted(maps.Keys(glyphs)) {
		if err = a.form(glyphs[name], child, state, owner, "Font.CharProcs", depth); err != nil {
			return err
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
					state := compositionState{source: matrix.IdentMatrix, fitted: outer}.concat(placement)
					if err = a.form(obj, page.attrs.Resources, state, page.ref, "AP."+key, 0); err != nil {
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
