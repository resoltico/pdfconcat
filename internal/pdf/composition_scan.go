// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/matrix"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

type compositionInspector struct {
	context       context.Context
	pdf           *model.Context
	outer         matrix.Matrix
	frame         types.Rectangle
	target        packet.Size
	original      int
	remaining     int64
	active        map[types.IndirectRef]bool
	pageResources types.Dict
}

func (a *compositionInspector) failure(owner types.IndirectRef, path string) error {
	d := diag.Report(diag.FitGeometryUnsupported, diag.FittingCompositionAccuracyCannotBeEstablished())
	d.PDFLocation = &diag.PDFLocation{Object: owner.ObjectNumber.Value(), Page: a.original, Path: path}
	return &policyError{finding: d}
}
func (a *compositionInspector) certified(p *compositionState) bool {
	if !p.known {
		p.accurate = accurateComposition(*p, a.outer, a.frame, a.target)
		p.known = true
	}
	return p.accurate
}
func contentMatrix(args []types.Object) (matrix.Matrix, bool) {
	if len(args) != 6 {
		return matrix.Matrix{}, false
	}
	var n [6]float64
	for i, v := range args {
		switch v := v.(type) {
		case types.Integer:
			n[i] = float64(v)
		case types.Float:
			n[i] = v.Value()
		default:
			return matrix.Matrix{}, false
		}
	}
	m := matrix.Matrix{{n[0], n[1], 0}, {n[2], n[3], 0}, {n[4], n[5], 1}}
	return m, finiteMatrix(m)
}
func objectMatrix(s *model.Context, object types.Object) (matrix.Matrix, error) {
	value, e := s.Dereference(object)
	if e != nil {
		return matrix.Matrix{}, e
	}
	if value == nil {
		return matrix.IdentMatrix, nil
	}
	values, err := s.DereferenceArray(value)
	if err != nil {
		return matrix.Matrix{}, err
	}
	args := make([]types.Object, len(values))
	for i, v := range values {
		args[i], err = s.Dereference(v)
		if err != nil {
			return matrix.Matrix{}, err
		}
	}
	m, ok := contentMatrix(args)
	if !ok {
		return matrix.Matrix{}, errors.New("invalid resource matrix")
	}
	return m, nil
}
func (a *compositionInspector) program(role compositionProgramRole, object types.Object, resources types.Dict, state compositionState, owner types.IndirectRef, path string, depth int) error {
	if err := a.context.Err(); err != nil {
		return err
	}
	stream, _, err := a.pdf.DereferenceStreamDict(object)
	if err != nil {
		return err
	}
	if stream == nil {
		return a.failure(owner, path)
	}
	ref, ok := object.(types.IndirectRef)
	if !ok {
		return a.failure(owner, path)
	}
	if a.active[ref] || depth > 256 {
		return a.failure(ref, path)
	}
	if role != type3GlyphProgram {
		m, e := objectMatrix(a.pdf, stream.Dict["Matrix"])
		if e != nil {
			return a.failure(ref, "Matrix")
		}
		state = state.concat(m)
		if !a.certified(&state) {
			return a.failure(ref, "Matrix")
		}
		if role == formProgram {
			resources = a.pageResources
		}
		if stream.Dict["Resources"] != nil {
			local, err := a.pdf.DereferenceDict(stream.Dict["Resources"])
			if err != nil {
				return err
			}
			if local != nil {
				resources = local
			}
		}
	}
	data, err := decodeDrawingStream(a.context, a.pdf, *stream)
	if err != nil {
		return err
	}
	a.active[ref] = true
	err = a.scan(data, resources, state, ref, depth+1)
	delete(a.active, ref)
	return err
}

// scan observes only composition-sensitive state, not general PDF resource or
// drawing semantics. Existing strict object/inline-image parsers keep lexical
// payloads opaque. The invocation budget counts repeats, never just object refs.
func (a *compositionInspector) scan(data []byte, resources types.Dict, state compositionState, owner types.IndirectRef, depth int) error {
	if depth > 256 {
		return a.failure(owner, "Content")
	}
	a.remaining -= int64(len(data)) + 1
	if a.remaining < 0 {
		return a.failure(owner, "Content.WorkBudget")
	}
	entry := state
	text := compositionText{}
	stack := []compositionState{}
	args := []types.Object{}
	rest := string(data)
	for len(rest) > 0 {
		if err := a.context.Err(); err != nil {
			return err
		}
		rest = strings.TrimLeft(rest, "\x00\t\n\f\r ")
		if rest == "" {
			break
		}
		if rest[0] == '%' {
			n := strings.IndexAny(rest, "\r\n")
			if n < 0 {
				break
			}
			rest = rest[n:]
			continue
		}
		if strings.ContainsRune("/[(<+-.0123456789", rune(rest[0])) || strings.HasPrefix(rest, "true") || strings.HasPrefix(rest, "false") || strings.HasPrefix(rest, "null") {
			before := len(rest)
			value, err := model.ParseObjectWithPolicy(a.context, &rest, 0, model.ValidationStrict, 256)
			if err != nil || value.StrictFailure != nil || len(rest) >= before {
				return a.failure(owner, "Content.Operand")
			}
			args = append(args, value.Object)
			if len(args) > 256 {
				return a.failure(owner, "Content.Operands")
			}
			continue
		}
		n := strings.IndexAny(rest, "\x00\t\n\f\r /[]<>()%")
		if n < 0 {
			n = len(rest)
		}
		if n == 0 {
			return a.failure(owner, "Content.Operator")
		}
		op := rest[:n]
		rest = rest[n:]
		switch op {
		case "BT":
			text.begin()
		case "ET":
			text.active = false
			text.known = false
			text.lineKnown = false
		case "Tm":
			m, ok := contentMatrix(args)
			if !ok || !text.active {
				return a.failure(owner, "Content.Tm")
			}
			text.position(m)
		case "Td", "TD":
			if len(args) != 2 || !text.active {
				return a.failure(owner, "Content."+op)
			}
			x, e := a.pdf.DereferenceNumber(args[0])
			if e != nil {
				return e
			}
			y, e := a.pdf.DereferenceNumber(args[1])
			if e != nil {
				return e
			}
			if op == "TD" {
				state.leading = -y
			}
			text.moveLine(x, y)
		case "TL", "Tc", "Tw":
			if len(args) != 1 {
				return a.failure(owner, "Content."+op)
			}
			value, e := a.pdf.DereferenceNumber(args[0])
			if e != nil {
				return e
			}
			switch op {
			case "TL":
				state.leading = value
			case "Tc":
				state.charSpacing = value
			case "Tw":
				state.wordSpacing = value
			}
		case "T*":
			if !text.active {
				return a.failure(owner, "Content.T*")
			}
			text.moveLine(0, -state.leading)
		case "Tj", "TJ", "'", "\"":
			if op == "\"" {
				if len(args) != 3 {
					return a.failure(owner, "Content.Quote")
				}
				var e error
				state.wordSpacing, e = a.pdf.DereferenceNumber(args[0])
				if e != nil {
					return e
				}
				state.charSpacing, e = a.pdf.DereferenceNumber(args[1])
				if e != nil {
					return e
				}
			}
			if op == "'" || op == "\"" {
				text.moveLine(0, -state.leading)
			}
			if e := a.showText(args, state, &text, owner, depth, op); e != nil {
				return e
			}
			if state.textMode != 3 && !a.certified(&state) {
				return a.failure(owner, "Content."+op)
			}
		case "q":
			if len(stack) >= 256 || len(args) != 0 {
				return a.failure(owner, "Content.q")
			}
			stack = append(stack, state)
		case "Q":
			// An unmatched Q is already supported by the source boundary. It cannot
			// restore the enclosing generated Form's state; certify the retained state.
			if len(args) != 0 {
				return a.failure(owner, "Content.Q")
			}
			if len(stack) > 0 {
				state = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		case "cm":
			m, ok := contentMatrix(args)
			if !ok {
				return a.failure(owner, "Content.cm")
			}
			state = state.concat(m)
		case "scn", "SCN":
			var binding *selectedPattern
			if len(args) > 0 {
				if name, ok := args[len(args)-1].(types.Name); ok {
					patterns, e := a.pdf.DereferenceDict(resources["Pattern"])
					if e != nil {
						return e
					}
					parent := entry
					binding = &selectedPattern{object: patterns[string(name)], resources: resources, parent: &parent}
				}
			}
			if op == "scn" {
				state.fillPattern = binding
			} else {
				state.strokePattern = binding
			}
		case "cs", "CS", "sc", "SC", "g", "G", "rg", "RG", "k", "K":
			if op == "cs" || op == "sc" || op == "g" || op == "rg" || op == "k" {
				state.fillPattern = nil
			} else {
				state.strokePattern = nil
			}
		case "gs":
			if len(args) == 1 {
				if name, ok := args[0].(types.Name); ok {
					states, e := a.pdf.DereferenceDict(resources["ExtGState"])
					if e != nil {
						return e
					}
					graphics, e := a.pdf.DereferenceDict(states[string(name)])
					if e != nil {
						return e
					}
					if graphics["Font"] != nil {
						values, e := a.pdf.DereferenceArray(graphics["Font"])
						if e != nil {
							return e
						}
						if len(values) != 2 {
							return a.failure(owner, "ExtGState.Font")
						}
						size, e := a.pdf.DereferenceNumber(values[1])
						if e != nil {
							return e
						}
						state.font = &selectedFont{object: values[0], size: size}
					}
					value, e := a.pdf.Dereference(graphics["SMask"])
					if e != nil {
						return e
					}
					if mask, ok := value.(types.Dict); ok && mask["G"] != nil {
						if e = a.program(softMaskProgram, mask["G"], resources, state, owner, "ExtGState.SMask.G", depth); e != nil {
							return e
						}
					}
				}
			}
		case "Tf":
			if len(args) != 2 {
				return a.failure(owner, "Content.Tf")
			}
			name, ok := args[0].(types.Name)
			if !ok {
				return a.failure(owner, "Content.Tf")
			}
			fonts, e := a.pdf.DereferenceDict(resources["Font"])
			if e != nil {
				return e
			}
			size, e := a.pdf.DereferenceNumber(args[1])
			if e != nil {
				return e
			}
			state.font = &selectedFont{object: fonts[string(name)], size: size}
		case "Tr":
			if len(args) != 1 {
				return a.failure(owner, "Content.Tr")
			}
			mode, e := a.pdf.DereferenceInteger(args[0])
			if e != nil || mode == nil || mode.Value() < 0 || mode.Value() > 7 {
				return a.failure(owner, "Content.Tr")
			}
			state.textMode = mode.Value()
		case "Tz", "Ts":
			if len(args) != 1 {
				return a.failure(owner, "Content."+op)
			}
			value, e := a.pdf.DereferenceNumber(args[0])
			if e != nil {
				return e
			}
			if op == "Tz" {
				state.horizontalScale = value / 100
			} else {
				state.textRise = value
			}
		case "Do":
			if len(args) != 1 {
				return a.failure(owner, "Content.Do")
			}
			name, ok := args[0].(types.Name)
			if !ok {
				return a.failure(owner, "Content.Do")
			}
			objects, err := a.pdf.DereferenceDict(resources["XObject"])
			if err != nil {
				return err
			}
			object := objects[string(name)]
			value, err := a.pdf.Dereference(object)
			if err != nil {
				return err
			}
			stream, ok := value.(types.StreamDict)
			if !ok {
				return a.failure(owner, "Content.Do")
			}
			subtype, err := nameValue(a.pdf, stream.Dict["Subtype"])
			if err != nil {
				return err
			}
			if subtype == "Form" {
				if err = a.program(formProgram, object, resources, state, owner, "Content.Do", depth); err != nil {
					return err
				}
			} else if !a.certified(&state) {
				return a.failure(owner, "Content.Do")
			}
		case "BI":
			if !a.certified(&state) {
				return a.failure(owner, "Content.BI")
			}
			budget := int64(len(rest))*32 + 1
			var err error
			rest, err = inlineTail(a.context, rest, &budget, 0, a.pdf.PDF20())
			if err != nil {
				return a.failure(owner, "Content.BI")
			}
		default:
			if compositionPaintOperator(op) {
				if err := a.paintResources(state, &text, owner, depth, op); err != nil {
					return err
				}
			}
			// Path construction also consumes CTM before a later q/Q or painting op.
			if (compositionPathOperator(op) || (compositionPaintOperator(op) && (!textShowing(op) || state.textMode != 3))) && !a.certified(&state) {
				return a.failure(owner, "Content."+op)
			}
		}
		args = args[:0]
	}
	return nil
}

func fittingCompositionFindings(c context.Context, s *model.Context, pages []pageGeometry, plans fittingPlan) ([]diag.Diagnostic, error) {
	contents, err := capturePageContent(c, s)
	if err != nil {
		return nil, err
	}
	var findings []diag.Diagnostic
	for i, page := range pages {
		plan := plans.page(i)
		if !plan.Decision.Transform {
			continue
		}
		data, err := joinedPageContent(c, s, contents[i], i+1)
		if err != nil {
			return nil, err
		}
		limit := s.DecodeLimit()
		if limit < 0 {
			limit = model.DefaultResourceLimits().MaxDecodeBytes
		}
		observer := compositionInspector{context: c, pdf: s, outer: plan.Matrix, frame: page.box, target: plan.Decision.Output, original: plan.Original, remaining: limit, pageResources: page.attrs.Resources, active: map[types.IndirectRef]bool{}}
		err = observer.scan(data, page.attrs.Resources, compositionState{source: matrix.IdentMatrix, fitted: plan.Matrix, horizontalScale: 1}, page.ref, 0)
		if err == nil {
			err = observer.appearances(page, plan)
		}
		if err != nil {
			var policy *policyError
			if errors.As(err, &policy) && policy.finding.Code == diag.FitGeometryUnsupported {
				findings = append(findings, policy.finding)
				continue
			}
			return nil, err
		}
	}
	return findings, nil
}

func compositionPaintOperator(op string) bool {
	switch op {
	case "S", "s", "f", "F", "f*", "B", "B*", "b", "b*", "sh", "Tj", "TJ", "'", "\"":
		return true
	}
	return false
}
func compositionPathOperator(op string) bool {
	switch op {
	case "m", "l", "c", "v", "y", "re", "W", "W*":
		return true
	}
	return false
}
