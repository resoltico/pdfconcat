// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"maps"
	"math"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/filter"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// Successful stream proofs can be reused at other widgets in the same
// source. Widget-specific settings and every appearance reference are
// checked independently of the stream cache.
type emptySignatureAppearanceProof struct {
	decoder drawingDecoder
	proved  map[types.IndirectRef]bool
}

func (p *emptySignatureAppearanceProof) checkWidget(c context.Context, s *model.Context, program *layerPrograms, page pageRef, widget types.Dict, at sourceSite) error {
	bad := signatureUnsupported(at.key("Rect"), diag.SignatureDegeneratePrint)
	value, err := signatureSlot(s, widget["AP"], at.key("AP"), diag.SignatureDegeneratePrint)
	if err != nil || value == nil {
		return err
	}
	ap, ok := value.(types.Dict)
	if !ok {
		return bad
	}
	checkedSettings := false
	prove := func(raw, value types.Object, where sourceSite) error {
		if err := c.Err(); err != nil {
			return err
		}
		if value == nil {
			return nil
		}
		if !checkedSettings {
			if checkSignatureCompositing(s, page, widget, at) != nil || checkSignatureRegeneration(s) != nil {
				return signatureAppearanceRefused(where, raw, diag.SignatureDegeneratePrint)
			}
			checkedSettings = true
		}
		stream, ok := value.(types.StreamDict)
		if !ok {
			return signatureAppearanceRefused(where, raw, diag.SignatureDegeneratePrint)
		}
		reason, err := p.checkStream(c, s, program, raw, stream, at)
		if err != nil {
			return err
		}
		if reason != "" {
			if err := c.Err(); err != nil {
				return err
			}
			return signatureAppearanceRefused(where, raw, reason)
		}
		return nil
	}
	// Viewers can choose rollover, down, or an alternate normal state in
	// print mode, even if the current screen state is blank.
	for _, key := range []string{"N", "R", "D"} {
		where := at.key("AP").key(key)
		value, err := signatureSlot(s, ap[key], where, diag.SignatureDegeneratePrint)
		if err != nil {
			return err
		}
		if states, ok := value.(types.Dict); ok {
			for _, name := range slices.Sorted(maps.Keys(states)) {
				raw := states[name]
				state, err := signatureSlot(s, raw, where.name(name), diag.SignatureDegeneratePrint)
				if err != nil {
					return err
				}
				if err := prove(raw, state, where.name(name)); err != nil {
					return err
				}
			}
		} else if err := prove(ap[key], value, where); err != nil {
			return err
		}
	}
	return c.Err()
}

func signatureAppearanceRefused(site sourceSite, raw types.Object, reason diag.SignatureDiscardReason) error {
	finding := diag.Report(diag.SourceSignatureDiscardUnsupported, diag.SignatureDiscardUnsupported(reason))
	finding.PDFLocation = site.location()
	if ref, ok := raw.(types.IndirectRef); ok {
		diag.AttachEvidence(&finding, diag.SignatureAppearanceObject(int64(ref.ObjectNumber.Value()), int64(ref.GenerationNumber.Value())))
	}
	return &policyError{finding: finding}
}

func (p *emptySignatureAppearanceProof) checkStream(c context.Context, s *model.Context, program *layerPrograms, raw types.Object, stream types.StreamDict, site sourceSite) (diag.SignatureDiscardReason, error) {
	ref, indirect := raw.(types.IndirectRef)
	if !indirect {
		return diag.SignatureDegeneratePrint, nil
	}
	if p.proved[ref] {
		return "", nil
	}
	if !emptyAppearanceLocalForm(s, stream.Dict, site) || !emptyAppearanceEncoding(s, stream, site) {
		return diag.SignatureDegeneratePrint, nil
	}
	reason, err := appearanceBoundaryFailure(c, s, ref, stream, site)
	if err != nil || reason != "" {
		return reason, err
	}
	budget := program.decodeBudget
	if budget == nil {
		budget = s.XRefTable
	}
	content, err := p.decoder.decodeWithBudget(c, s, stream, budget)
	if err != nil {
		if errors.Is(err, filter.ErrDecodeLimitExceeded) {
			return diag.SignatureDegeneratePrint, nil
		}
		return diag.SignatureAppearanceEncoding, c.Err()
	}
	// Charge the same scan allowance as an ordinary drawing program.
	program.remaining -= int64(len(content)) + 1
	if program.remaining < 0 {
		return diag.SignatureDegeneratePrint, nil
	}
	if !appearanceTriviaOnly(c, content) {
		return diag.SignatureAppearanceNonTrivia, c.Err()
	}
	p.proved[ref] = true
	return "", nil
}

func emptyAppearanceLocalForm(s *model.Context, d types.Dict, site sourceSite) bool {
	subtype, err := signatureSlot(s, d["Subtype"], site.key("Subtype"), diag.SignatureAppearance)
	if err != nil || subtype != types.Name("Form") ||
		requireAbsentSignatureSlots(s, d, site, diag.SignatureCompositing, "Group", "Ref", "OC", "F", "FFilter", "FDecodeParms", "OPI") != nil {
		return false
	}
	for key, expected := range map[string]types.Object{"Type": types.Name("XObject"), "FormType": types.Integer(1)} {
		value, err := signatureSlot(s, d[key], site.key(key), diag.SignatureAppearance)
		if err != nil || value != nil && value != expected {
			return false
		}
	}
	box, err := readRectangle(s, d["BBox"])
	if err != nil || math.IsNaN(box.Width()) || math.IsNaN(box.Height()) ||
		math.IsInf(box.Width(), 0) || math.IsInf(box.Height(), 0) {
		return false
	}
	rawMatrix, err := signatureSlot(s, d["Matrix"], site.key("Matrix"), diag.SignatureAppearance)
	if err != nil {
		return false
	}
	if rawMatrix != nil {
		values, ok := rawMatrix.(types.Array)
		if !ok || len(values) != 6 {
			return false
		}
		for _, value := range values {
			n, err := s.DereferenceNumber(value)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return false
			}
		}
	}
	return true
}

func emptyAppearanceEncoding(s *model.Context, stream types.StreamDict, site sourceSite) bool {
	filterEntry, err := signatureSlot(s, stream.Dict["Filter"], site.key("Filter"), diag.SignatureAppearance)
	if err != nil {
		return false
	}
	parmsSite := site.key("DecodeParms")
	params, err := signatureSlot(s, stream.Dict["DecodeParms"], parmsSite, diag.SignatureAppearance)
	if err != nil {
		return false
	}
	pipeline := stream.FilterPipeline
	if len(pipeline) == 0 {
		return filterEntry == nil && params == nil
	}
	if filterEntry == nil || len(pipeline) != 1 || pipeline[0].Name != filter.Flate {
		return false
	}
	if values, ok := params.(types.Array); ok {
		switch len(values) {
		case 0:
			params = nil
		case 1:
			params, err = signatureSlot(s, values[0], parmsSite.index(0), diag.SignatureAppearance)
			if err != nil {
				return false
			}
		default:
			return false
		}
	}
	if params == nil {
		return len(pipeline[0].DecodeParms) == 0
	}
	d, ok := params.(types.Dict)
	if !ok {
		return false
	}
	defaults := map[string]types.Integer{"Predictor": 1, "Colors": 1, "Columns": 1, "BitsPerComponent": 8}
	for key, raw := range d {
		expected, known := defaults[key]
		value, err := signatureSlot(s, raw, parmsSite.key(key), diag.SignatureAppearance)
		if !known || err != nil || value != nil && value != expected {
			return false
		}
	}
	return true
}

func appearanceTriviaOnly(c context.Context, data []byte) bool {
	comment := false
	for i, b := range data {
		if i&4095 == 0 && c.Err() != nil {
			return false
		}
		switch b {
		case '\r', '\n':
			comment = false
		case '%':
			comment = true
		case 0, '\t', '\f', ' ':
		default:
			if !comment {
				return false
			}
		}
	}
	return c.Err() == nil
}
