// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/matrix"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

type signatureStage struct {
	artwork        map[int][]signatureArtwork
	programs       map[int]signatureProgramWitness
	unsignedFields int
	widgets        map[int]int
}

type signatureProgramWitness struct {
	page       [sha256.Size]byte
	fittedForm [sha256.Size]byte
}

func (v *signatureStage) track(c context.Context, s *model.Context, part Part, origins sourceOrigins) error {
	if err := v.trackUnsigned(c, s, int(part.FirstPage)-1); err != nil {
		return err
	}
	if len(origins.signatureArtwork) == 0 {
		return nil
	}
	originals := map[int][]signatureArtwork{}
	for _, art := range origins.signatureArtwork {
		originals[art.page] = append(originals[art.page], art)
	}
	occurrence := part.Occurrence.Facts
	selected := 0
	for _, span := range occurrence.Selection {
		for page := span.First; page <= span.Last; page++ {
			if err := c.Err(); err != nil {
				return err
			}
			for _, original := range originals[page] {
				art := original
				art.page = int(part.FirstPage) + selected
				if occurrence.fitting.Count > 0 {
					fit := occurrence.fitting.page(selected)
					if fit.Decision.Transform {
						attrs := model.InheritedPageAttrs{
							MediaBox: types.NewRectangle(fit.Native.Media[0], fit.Native.Media[1], fit.Native.Media[2], fit.Native.Media[3]),
							CropBox:  types.NewRectangle(fit.Native.Crop[0], fit.Native.Crop[1], fit.Native.Crop[2], fit.Native.Crop[3]),
						}
						art.fitting = &signatureFitting{clip: effectivePageBox(&attrs), transform: fit.Matrix}
					}
				}
				v.artwork[art.page] = append(v.artwork[art.page], art)
			}
			selected++
		}
	}
	return nil
}

func (v *signatureStage) trackUnsigned(c context.Context, s *model.Context, pageOffset int) error {
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	form, err := s.DereferenceDict(cat["AcroForm"])
	if err != nil {
		return err
	}
	roots, err := formFieldTree(c, s, form)
	if err != nil {
		return err
	}
	var observe func([]*formFieldNode)
	observe = func(nodes []*formFieldNode) {
		for _, node := range nodes {
			if node.kind == "Sig" {
				v.unsignedFields++
			}
			observe(node.kids)
		}
	}
	observe(roots)
	pages, err := rawPages(c, s)
	if err != nil {
		return err
	}
	for i, page := range pages {
		if err := c.Err(); err != nil {
			return err
		}
		annots, err := s.DereferenceArray(page.dict["Annots"])
		if err != nil {
			return err
		}
		for _, raw := range annots {
			d, err := s.DereferenceDict(raw)
			if err != nil {
				return err
			}
			kind, err := nameValue(s, d["Subtype"])
			if err != nil {
				return err
			}
			if kind == "Widget" {
				v.widgets[pageOffset+i+1]++
			}
		}
	}
	return nil
}

// Verify the raw bytes before the backend can repair or normalize a binding.
// Capture a separate content-program witness after assembly to guard other
// changes to the graphics state and to the order of the drawing operations.
func (v *signatureStage) verify(c context.Context, s *model.Context, pages []pageRef, content pageContentSnapshot, capture bool) error {
	if !capture {
		actual := signatureStage{widgets: map[int]int{}}
		if err := actual.trackUnsigned(c, s, 0); err != nil {
			return err
		}
		if actual.unsignedFields != v.unsignedFields || len(actual.widgets) != len(v.widgets) {
			return errors.New("serialized unsigned signature fields or page widget memberships differ from the admitted packet")
		}
		for page, count := range actual.widgets {
			if v.widgets[page] != count {
				return fmt.Errorf("page %d has unexpected widget memberships", page)
			}
		}
	}
	evidence := newLayerProgramEvidence(s)
	for i, page := range pages {
		if err := c.Err(); err != nil {
			return err
		}
		arts := v.artwork[i+1]
		site := sourceSite{ref: page.ref, page: i + 1}.key("Contents")
		program := evidence.program(s, layerVerify)
		data, ends, err := program.page(c, s, content[i], site)
		if err != nil && (len(arts) != 0 || c.Err() != nil) {
			return err
		}
		pageDigest := sha256.New()
		for _, stream := range content[i].streams {
			pageDigest.Write(stream.digest[:])
		}
		var encoded [sha256.Size]byte
		copy(encoded[:], pageDigest.Sum(nil))
		var joined []byte
		start := 0
		for _, end := range ends {
			joined = append(joined, data[start:end]...)
			joined = append(joined, '\n')
			start = end
		}
		resources, err := effectiveLayerResources(s, page, site)
		if err != nil {
			return err
		}
		if len(arts) > 0 {
			at := sourceSite{ref: page.ref, page: i + 1}
			if err := requireAbsentSignatureSlots(s, page.dict, at, diag.SignatureCompositing, "Group"); err != nil {
				return fmt.Errorf("page %d signature artwork: %w", i+1, err)
			}
		}
		forms, err := verifyPageSignatureArtwork(c, s, &program, joined, resources, site, arts)
		if err != nil {
			return fmt.Errorf("page %d signature artwork: %w", i+1, err)
		}
		witness := signatureProgramWitness{page: encoded, fittedForm: forms}
		if capture {
			v.programs[i+1] = witness
		} else if v.programs[i+1] != witness {
			return fmt.Errorf("page %d signature artwork: serialized drawing scope changed", i+1)
		}
	}
	return nil
}

func signatureDoName(op layerOperation) (types.Name, bool) {
	if op.name != "Do" || len(op.args) != 1 {
		return "", false
	}
	name, ok := op.args[0].value.(types.Name)
	return name, ok
}

// Current artwork belongs to the allocated binding in a known drawing scope.
// Source artifact tags, including those copied from earlier packets, carry
// no ownership. The reopened program witness also covers pages without edits.
func verifyPageSignatureArtwork(c context.Context, s *model.Context, program *layerPrograms, data []byte, resources types.Dict, site sourceSite, arts []signatureArtwork) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	var fitting *signatureFitting
	if len(arts) != 0 {
		fitting = arts[0].fitting
	}
	var inner []byte
	var innerResources types.Dict
	var scope signatureGraphicsScope
	previousWasFittingMatrix := false
	fittingInvocations := 0
	witness := sha256.New()
	mode := layerDetect
	if fitting != nil {
		mode = layerVerify
	}
	err := program.scan(c, data, nil, site, mode, func(op layerOperation) error {
		followsFittingMatrix := previousWasFittingMatrix
		previousWasFittingMatrix = fitting != nil && !scope.dirty && op.name == "cm" && signatureMatrixMatches(op.args, fitting.transform)
		scope.observe(op)
		name, ok := signatureDoName(op)
		if !ok {
			return nil
		}
		if fitting != nil {
			fittingInvocations++
			if !followsFittingMatrix || fittingInvocations != 1 {
				return errors.New("invalid fitted signature artwork invocation or fitting matrix")
			}
			form, err := signatureForm(s, resources, name)
			if err != nil {
				return err
			}
			if err := verifySignatureFittingForm(s, form, fitting.clip); err != nil {
				return err
			}
			innerResources, err = s.DereferenceDict(form.Dict["Resources"])
			if err != nil || innerResources == nil {
				return errors.New("fitted signature Form must have local resources")
			}
			inner, err = program.decode(c, s, form)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(inner)
			witness.Write(sum[:])
			return nil
		}
		// Compare invoked fitting programs even when this page carries no
		// fresh artwork. Their original marks are ordinary source content.
		if strings.HasPrefix(string(name), "PCSource") {
			if form, ok := possibleSignatureForm(s, resources, name); ok {
				sum := sha256.Sum256(form.Raw)
				witness.Write(sum[:])
			}
		}
		return nil
	})
	if err != nil && (len(arts) != 0 || c.Err() != nil) {
		return digest, err
	}
	copy(digest[:], witness.Sum(nil))
	if fitting != nil {
		if fittingInvocations != 1 {
			return digest, errors.New("fitted signature artwork must be invoked once")
		}
		return digest, verifySignatureInvocations(c, s, program, inner, innerResources, site, arts)
	}
	return digest, verifySignatureInvocations(c, s, program, data, resources, site, arts)
}

func verifySignatureFittingForm(s *model.Context, form types.StreamDict, clip types.Rectangle) error {
	box, err := readRectangle(s, form.Dict["BBox"])
	if err != nil || *box != clip {
		return errors.New("fitted signature Form BBox differs from the admitted page clip")
	}
	raw, err := s.Dereference(form.Dict["Matrix"])
	if err != nil {
		return err
	}
	if raw != nil {
		values, err := fitNumbers(s, raw)
		if err != nil || len(values) != 6 || values[0] != 1 || values[1] != 0 ||
			values[2] != 0 || values[3] != 1 || values[4] != 0 || values[5] != 0 {
			return errors.New("fitted signature Form must have an identity Matrix")
		}
	}
	// These are the entries the fitting producer can put in its local
	// Form. A new rendering or compositing entry needs its own proof.
	for key := range form.Dict {
		switch key {
		case "BBox", "Matrix", "Type", "Subtype", "FormType", "Resources", "StructParents", "Length", "Filter", "DecodeParms", "Group":
		default:
			return errors.New("fitted signature Form has unexpected drawing state")
		}
	}
	kind, err := s.Dereference(form.Dict["Type"])
	if err != nil || kind != nil && kind != types.Name("XObject") {
		return errors.New("fitted signature Form must be an XObject")
	}
	formType, err := s.Dereference(form.Dict["FormType"])
	if err != nil || formType != nil && formType != types.Integer(1) {
		return errors.New("fitted signature Form has an unsupported FormType")
	}
	return nil
}

func verifySignatureInvocations(c context.Context, s *model.Context, program *layerPrograms, data []byte, resources types.Dict, site sourceSite, arts []signatureArtwork) error {
	if len(arts) == 0 {
		return nil
	}
	colors, err := signatureDeviceColors(c, s, resources)
	if err != nil {
		return err
	}
	if colors != arts[0].pageColors {
		return errors.New("default color spaces differ from the admitted page drawing scope")
	}
	owned := make(map[types.Name]int, len(arts))
	for i, art := range arts {
		owned[types.Name(art.name)] = i
	}
	seen, opening, closing := 0, 0, 0
	var scope signatureGraphicsScope
	bad := errors.New("signature artwork invocation, order, placement or resource binding differs")
	err = program.scan(c, append(bytes.Clone(data), '\n'), nil, site, layerVerify, func(op layerOperation) error {
		clean := !scope.dirty
		scope.observe(op)
		if closing != 0 {
			expected := "Q"
			if closing == 1 {
				expected = "EMC"
			}
			if op.name != expected || len(op.args) != 0 {
				return bad
			}
			closing--
			return nil
		}
		if name, do := signatureDoName(op); do {
			if index, fresh := owned[name]; fresh {
				if index != seen || opening != 3 {
					return bad
				}
				stream, err := signatureForm(s, resources, name)
				if err != nil {
					return bad
				}
				binding := newLayerProgramEvidence(s).program(s, layerVerify)
				digest, err := inspectSignatureAppearance(c, s, &binding, stream, resources, site)
				if err != nil || digest != arts[index].appearance {
					return bad
				}
				seen++
				closing = 2
			}
		}
		switch {
		case signatureMarker(op):
			opening = 1
		case opening == 1 && clean && op.name == "q" && len(op.args) == 0:
			opening = 2
		case opening == 2 && seen < len(arts) && op.name == "cm" && signatureMatrixMatches(op.args, arts[seen].placement):
			opening = 3
		default:
			opening = 0
		}
		return nil
	})
	if err != nil {
		return err
	}
	if closing != 0 || seen != len(arts) {
		return bad
	}
	return nil
}

type signatureGraphicsScope struct {
	stack []bool
	dirty bool
}

// Any preceding operation might change defaults inherited by an appearance.
// An enclosing saved graphics state supplies evidence of restoration.
func (s *signatureGraphicsScope) observe(op layerOperation) {
	switch op.name {
	case "q":
		s.stack = append(s.stack, s.dirty)
	case "Q":
		if len(s.stack) == 0 {
			s.dirty = true
			return
		}
		s.dirty = s.stack[len(s.stack)-1]
		s.stack = s.stack[:len(s.stack)-1]
	case "BMC", "BDC", "EMC":
	default:
		s.dirty = true
	}
}

func signatureMatrixMatches(args []layerOperand, expected matrix.Matrix) bool {
	if len(args) != 6 {
		return false
	}
	for i, arg := range args {
		var actual float64
		switch v := arg.value.(type) {
		case types.Float:
			actual = float64(v)
		case types.Integer:
			actual = float64(v)
		default:
			return false
		}
		value, err := strconv.ParseFloat(number(expected[i/2][i%2]), 64)
		if err != nil || value != actual {
			return false
		}
	}
	return true
}

// The fitted-source prefix is also legal for an unrelated authored binding.
func possibleSignatureForm(s *model.Context, resources types.Dict, name types.Name) (types.StreamDict, bool) {
	stream, err := signatureForm(s, resources, name)
	return stream, err == nil
}

func signatureForm(s *model.Context, resources types.Dict, name types.Name) (types.StreamDict, error) {
	xobjects, err := s.DereferenceDict(resources["XObject"])
	if err != nil {
		return types.StreamDict{}, err
	}
	stream, _, err := s.DereferenceStreamDict(xobjects[string(name)])
	if err != nil || stream == nil || stream.Dict["Subtype"] != types.Name("Form") {
		return types.StreamDict{}, errors.New("signature artwork must bind to a local opaque Form")
	}
	if err := requireAbsentSignatureSlots(s, stream.Dict, sourceSite{}, diag.SignatureCompositing, "Group", "Ref", "OC", "F"); err != nil {
		return types.StreamDict{}, err
	}
	return *stream, nil
}

func newSignatureStage(policy packet.SignaturePolicy) *signatureStage {
	if policy != packet.SignaturesDiscard {
		return nil
	}
	return &signatureStage{
		artwork:  map[int][]signatureArtwork{},
		programs: map[int]signatureProgramWitness{},
		widgets:  map[int]int{},
	}
}
