// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/matrix"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Text-object matrices are not graphics state: q/Q does not restore them.
// Each content-program invocation owns its own BT/ET lifetime.
type compositionText struct {
	active, known, lineKnown bool
	matrix, line             matrix.Matrix
}

func translation(x, y float64) matrix.Matrix { return matrix.Matrix{{1, 0, 0}, {0, 1, 0}, {x, y, 1}} }
func (t *compositionText) begin() {
	*t = compositionText{active: true, known: true, lineKnown: true, matrix: matrix.IdentMatrix, line: matrix.IdentMatrix}
}
func (t *compositionText) position(m matrix.Matrix) {
	t.matrix = m
	t.line = m
	t.known = finiteMatrix(m)
	t.lineKnown = t.known
}
func (t *compositionText) moveLine(x, y float64) {
	t.line = translation(x, y).Multiply(t.line)
	t.lineKnown = t.lineKnown && finiteMatrix(t.line)
	t.matrix = t.line
	t.known = t.lineKnown
}
func (t *compositionText) advance(x, y float64) {
	t.matrix = translation(x, y).Multiply(t.matrix)
	t.known = t.known && finiteMatrix(t.matrix)
}

// show observes each Type3 glyph at its actual cursor, then advances it using the
// font's declared simple-font widths. Other glyph interpreters are not duplicated:
// their cursor becomes unknown after a string; Tm or line positioning can recover
// it, and any later Type3 show must establish a known cursor before proceeding.
func (a *compositionInspector) showText(args []types.Object, state compositionState, text *compositionText, owner types.IndirectRef, depth int, op string) error {
	if !text.active {
		return a.failure(owner, "Content."+op)
	}
	var items types.Array
	switch op {
	case "TJ":
		if len(args) != 1 {
			return a.failure(owner, "Content.TJ")
		}
		var ok bool
		items, ok = args[0].(types.Array)
		if !ok {
			return a.failure(owner, "Content.TJ")
		}
	case "Tj", "'":
		if len(args) != 1 {
			return a.failure(owner, "Content."+op)
		}
		items = types.Array{args[0]}
	case "\"":
		if len(args) != 3 {
			return a.failure(owner, "Content.Quote")
		}
		items = types.Array{args[2]}
	}
	var font types.Dict
	var err error
	if state.font != nil {
		font, err = a.pdf.DereferenceDict(state.font.object)
		if err != nil {
			return err
		}
	}
	subtype, err := nameValue(a.pdf, font["Subtype"])
	if err != nil {
		return err
	}
	for _, item := range items {
		if err = a.context.Err(); err != nil {
			return err
		}
		switch v := item.(type) {
		case types.Integer, types.Float:
			value, err := a.pdf.DereferenceNumber(v)
			if err != nil {
				return err
			}
			if state.font == nil {
				text.known = false
				continue
			}
			// Type3 is horizontal. Unsupported vertical-font cursor recovery is not
			// guessed; a later Type3 must establish its own position.
			if subtype == "Type0" {
				text.known = false
				continue
			}
			text.advance(-value/1000*state.font.size*state.horizontalScale, 0)
		case types.StringLiteral, types.HexLiteral:
			var data []byte
			var err error
			switch literal := v.(type) {
			case types.StringLiteral:
				data, err = types.Unescape(string(literal))
			case types.HexLiteral:
				data, err = literal.Bytes()
			}
			if err != nil {
				return err
			}
			if subtype != "Type3" {
				if err = a.paintResources(state, text, owner, depth, op); err != nil {
					return err
				}
				text.known = false
				continue
			}
			for _, code := range data {
				a.remaining--
				if a.remaining < 0 {
					return a.failure(owner, "Content.TextWorkBudget")
				}
				if state.textMode != 3 {
					if !text.known {
						return a.failure(owner, "Content.TextCursor")
					}
					if err = a.paintResources(state, text, owner, depth, op); err != nil {
						return err
					}
				}
				width, known, err := a.type3Advance(font, int(code))
				if err != nil {
					return err
				}
				if a.remaining < 0 {
					return a.failure(owner, "Content.TextWorkBudget")
				}
				if !known || state.font == nil {
					text.known = false
					continue
				}
				spacing := state.charSpacing
				if code == 32 {
					spacing += state.wordSpacing
				}
				text.advance((width*state.font.size+spacing)*state.horizontalScale, 0)
			}
		default:
			return a.failure(owner, "Content.TextOperand")
		}
	}
	return nil
}
func (a *compositionInspector) type3Advance(font types.Dict, code int) (float64, bool, error) {
	first, err := a.pdf.DereferenceInteger(font["FirstChar"])
	if err != nil {
		return 0, false, err
	}
	last, err := a.pdf.DereferenceInteger(font["LastChar"])
	if err != nil {
		return 0, false, err
	}
	widths, err := a.pdf.DereferenceArray(font["Widths"])
	if err != nil {
		return 0, false, err
	}
	// Unsigned subtraction compares the interval span without signed overflow.
	// Missing entries inside the interval are not the outside-range zero default.
	if first == nil || last == nil || last.Value() < first.Value() || len(widths) == 0 ||
		uint(last.Value())-uint(first.Value()) != uint(len(widths)-1) {
		return 0, false, nil
	}
	value, err := a.pdf.Dereference(font["FontMatrix"])
	if err != nil || value == nil {
		return 0, false, err
	}
	fm, err := objectMatrix(a.pdf, value)
	if err != nil {
		return 0, false, err
	}
	// Do not certify a default using an unreadable metrics declaration. Charge
	// repeated metrics inspection to the same invocation budget as glyph work.
	a.remaining -= int64(len(widths))
	if a.remaining < 0 {
		return 0, false, nil
	}
	var width float64
	for i, object := range widths {
		if err := a.context.Err(); err != nil {
			return 0, false, err
		}
		n, err := a.pdf.DereferenceNumber(object)
		if err != nil {
			return 0, false, err
		}
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false, nil
		}
		if code >= first.Value() && code <= last.Value() && i == code-first.Value() {
			width = n
		}
	}
	// PDF32000-1:2008 Table112 defines width zero outside FirstChar..LastChar.
	// FontMatrix contributes only the horizontal component, including rotations.
	advance := width * fm[0][0]
	return advance, !math.IsNaN(advance) && !math.IsInf(advance, 0), nil
}
