// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/matrix"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

const signatureArtworkTag = "/Artifact << /Type /Layout /PDFConcatSignatureArtwork true >> BDC"

type signatureArtwork struct {
	page       int
	name       string
	placement  matrix.Matrix
	appearance [sha256.Size]byte
	pageColors string
	box        types.Rectangle
	fitting    *signatureFitting
}

type signatureFitting struct {
	clip      types.Rectangle
	transform matrix.Matrix
}

func (p *signatureDiscard) planWidgets(c context.Context, s *model.Context, pages []pageRef, contents pageContentSnapshot) error {
	if len(p.widgets) == 0 {
		return nil
	}
	seen := map[types.IndirectRef]bool{}
	evidence := newLayerProgramEvidence(s)
	for i, page := range pages {
		if err := c.Err(); err != nil {
			return err
		}
		site := sourceSite{ref: page.ref, page: i + 1}
		annots, err := s.DereferenceArray(page.dict["Annots"])
		if err != nil {
			return site.key("Annots").wrap(err)
		}
		program := evidence.program(s, layerVerify)
		var kept types.Array
		var drawings []signatureDrawing
		for _, item := range annots {
			ref, indirect := item.(types.IndirectRef)
			widget := p.widgets[ref]
			if !indirect || widget == nil {
				kept = append(kept, item)
				continue
			}
			at := sourceSite{ref: ref, page: i + 1, path: "Object"}
			owned, err := formOwnerMatches(s, widget.dict["P"], page.ref, true, at.key("P"))
			if err != nil || !owned || seen[ref] {
				return signatureUnsupported(at, diag.SignatureOwnership)
			}
			seen[ref] = true
			drawing, err := planSignatureDrawing(c, s, &program, page, widget.dict, at)
			if err != nil {
				return err
			}
			if drawing.raw != nil {
				drawings = append(drawings, drawing)
			}
		}
		if len(kept) == len(annots) {
			continue
		}
		if len(drawings) > 0 {
			// Page content paints below annotations. Prove the order without
			// relying on a viewer's guessed bounds for competing annotations.
			if err := proveSignatureStacking(c, s, kept, drawings, site); err != nil {
				return err
			}
			if err := p.planPageArtwork(c, s, page, contents[i], &program, drawings, site); err != nil {
				return err
			}
		}
		p.edits = append(p.edits, func() {
			if len(kept) == 0 {
				delete(page.dict, "Annots")
			} else {
				page.dict["Annots"] = kept
			}
		})
	}
	if len(seen) != len(p.widgets) {
		for _, ref := range slices.SortedFunc(maps.Keys(p.widgets), func(a, b types.IndirectRef) int { return cmp.Compare(a.ObjectNumber, b.ObjectNumber) }) {
			if !seen[ref] {
				return signatureUnsupported(p.widgets[ref].site, diag.SignatureOwnership)
			}
		}
	}
	return nil
}

type signatureDrawing struct {
	raw types.Object
	art signatureArtwork
}

func proveSignatureStacking(c context.Context, s *model.Context, kept types.Array, drawings []signatureDrawing, site sourceSite) error {
	bad := signatureUnsupported(site.key("Annots"), diag.SignatureCompositing)
	// Bound comparison of annotation rectangles, allowing well-separated
	// widgets and links with stable page-space bounds.
	const maxVisibleSignaturesPerPage = 64
	if len(drawings) > maxVisibleSignaturesPerPage {
		return bad
	}
	overlaps := func(a, b types.Rectangle) bool {
		return a.LL.X <= b.UR.X && b.LL.X <= a.UR.X && a.LL.Y <= b.UR.Y && b.LL.Y <= a.UR.Y
	}
	for i, one := range drawings {
		for _, other := range drawings[:i] {
			if overlaps(one.art.box, other.art.box) {
				return bad
			}
		}
	}
	for _, raw := range kept {
		if err := c.Err(); err != nil {
			return err
		}
		d, err := s.DereferenceDict(raw)
		if err != nil || d == nil {
			return bad
		}
		flags, err := s.Dereference(d["F"])
		n, ok := flags.(types.Integer)
		if err != nil || flags != nil && !ok {
			return bad
		}
		f := model.AnnotationFlags(n)
		if f&model.AnnHidden != 0 {
			continue
		}
		kind, err := nameValue(s, d["Subtype"])
		if err != nil || kind != "Widget" && kind != "Link" || f&(model.AnnNoZoom|model.AnnNoRotate|model.AnnToggleNoView) != 0 {
			return bad
		}
		rect, err := readRectangle(s, d["Rect"])
		if err != nil {
			return bad
		}
		for _, draw := range drawings {
			if overlaps(draw.art.box, *rect) {
				return bad
			}
		}
	}
	return nil
}

func planSignatureDrawing(c context.Context, s *model.Context, program *layerPrograms, page pageRef, widget types.Dict, at sourceSite) (signatureDrawing, error) {
	rect, err := readRectangle(s, widget["Rect"])
	if err != nil {
		return signatureDrawing{}, at.key("Rect").wrap(err)
	}
	flags, err := s.Dereference(widget["F"])
	n, integer := flags.(types.Integer)
	if err != nil || flags != nil && !integer || n < 0 || n >= 1<<10 {
		return signatureDrawing{}, signatureUnsupported(at.key("F"), diag.SignatureVisibility)
	}
	if err := requireAbsentSignatureSlots(s, widget, at, diag.SignatureVisibility, "A", "AA"); err != nil {
		return signatureDrawing{}, err
	}
	f := model.AnnotationFlags(n)
	if f&model.AnnHidden != 0 {
		return signatureDrawing{}, nil
	}
	if rect.Width() == 0 || rect.Height() == 0 {
		if f&model.AnnPrint != 0 {
			live, err := hasLiveSignatureAppearance(s, widget)
			if err != nil {
				return signatureDrawing{}, at.key("AP").wrap(err)
			}
			if live {
				return signatureDrawing{}, signatureUnsupported(at.key("Rect"), diag.SignatureDegeneratePrint)
			}
		}
		return signatureDrawing{}, nil
	}
	if f&model.AnnPrint == 0 || f&(model.AnnNoView|model.AnnNoZoom|model.AnnNoRotate|model.AnnToggleNoView) != 0 {
		return signatureDrawing{}, signatureUnsupported(at.key("F"), diag.SignatureVisibility)
	}
	pageSite := sourceSite{ref: page.ref, page: at.page, path: "Object"}
	if err := requireAbsentSignatureSlots(s, page.dict, pageSite, diag.SignatureCompositing, "Group"); err != nil {
		return signatureDrawing{}, err
	}
	if err := requireAbsentSignatureSlots(s, widget, at, diag.SignatureCompositing, "BM", "CA", "ca"); err != nil {
		return signatureDrawing{}, err
	}
	mkSite := at.key("MK")
	characteristics, err := signatureSlot(s, widget["MK"], mkSite, diag.SignatureAppearance)
	if err != nil {
		return signatureDrawing{}, err
	}
	if characteristics != nil {
		mk, ok := characteristics.(types.Dict)
		if !ok {
			return signatureDrawing{}, signatureUnsupported(mkSite, diag.SignatureAppearance)
		}
		rotationSite := mkSite.key("R")
		rotation, err := signatureSlot(s, mk["R"], rotationSite, diag.SignatureAppearance)
		if err != nil {
			return signatureDrawing{}, err
		}
		if rotation != nil && rotation != types.Integer(0) {
			return signatureDrawing{}, signatureUnsupported(rotationSite, diag.SignatureAppearance)
		}
	}
	cat, err := s.Catalog()
	if err != nil {
		return signatureDrawing{}, err
	}
	form, err := s.DereferenceDict(cat["AcroForm"])
	if err != nil {
		return signatureDrawing{}, err
	}
	formSite := catalogSite(s).key("AcroForm").anchor(cat["AcroForm"], sourceOrigins{}).key("NeedAppearances")
	regenerate, err := signatureSlot(s, form["NeedAppearances"], formSite, diag.SignatureAppearance)
	if err != nil {
		return signatureDrawing{}, err
	}
	if regenerate != nil && regenerate != types.Boolean(false) {
		return signatureDrawing{}, signatureUnsupported(formSite, diag.SignatureAppearance)
	}
	ap, err := s.DereferenceDict(widget["AP"])
	if err != nil || ap["N"] == nil {
		return signatureDrawing{}, signatureUnsupported(at.key("AP"), diag.SignatureAppearance)
	}
	raw := ap["N"]
	resolved, err := s.Dereference(raw)
	if err != nil {
		return signatureDrawing{}, at.key("AP").wrap(err)
	}
	if states, ok := resolved.(types.Dict); ok {
		state, err := nameValue(s, widget["AS"])
		if err != nil || state == "" || states[string(state)] == nil {
			return signatureDrawing{}, signatureUnsupported(at.key("AS"), diag.SignatureAppearance)
		}
		raw = states[string(state)]
		resolved, err = s.Dereference(raw)
		if err != nil {
			return signatureDrawing{}, at.key("AP").wrap(err)
		}
	}
	stream, ok := resolved.(types.StreamDict)
	if !ok || stream.Dict["Subtype"] != types.Name("Form") {
		return signatureDrawing{}, signatureUnsupported(at.key("AP"), diag.SignatureAppearance)
	}
	if err := requireAbsentSignatureSlots(s, stream.Dict, at.key("AP"), diag.SignatureCompositing, "Group", "Ref", "OC", "F"); err != nil {
		return signatureDrawing{}, err
	}
	placement, paints, err := appearancePlacement(s, stream, *rect)
	if err != nil || !paints || !finiteSignatureMatrix(placement) ||
		placement[0][0] <= 0 || placement[1][1] <= 0 {
		return signatureDrawing{}, signatureUnsupported(at.key("AP"), diag.SignatureAppearance)
	}
	res, err := effectiveLayerResources(s, page, at)
	if err != nil {
		return signatureDrawing{}, err
	}
	digest, err := inspectSignatureAppearance(c, s, program, stream, res, at)
	if err != nil {
		return signatureDrawing{}, err
	}
	local, err := s.DereferenceDict(stream.Dict["Resources"])
	if err != nil {
		return signatureDrawing{}, err
	}
	if local == nil {
		// The widget could borrow page resources implicitly. Once called as
		// page artwork, make its supported dependencies explicit so page
		// resource optimization cannot prune defaults used only by this Form.
		borrowed := types.Dict{}
		for _, key := range []string{"Font", "ColorSpace"} {
			dict, err := s.DereferenceDict(res[key])
			if err != nil {
				return signatureDrawing{}, err
			}
			if len(dict) != 0 {
				borrowed[key] = clonedDirectDict(dict)
			}
		}
		stream.Dict = clonedDirectDict(stream.Dict)
		stream.Dict["Resources"] = borrowed
		ref, err := s.IndRefForNewObject(stream)
		if err != nil {
			return signatureDrawing{}, err
		}
		raw = *ref
	}
	return signatureDrawing{raw: raw, art: signatureArtwork{page: at.page, box: *rect, placement: placement, appearance: digest}}, nil
}

// Some viewers paint rollover or down appearances when a printable
// widget has no normal appearance. Check every available appearance
// before concluding that a degenerate widget contributes no ink.
func hasLiveSignatureAppearance(s *model.Context, widget types.Dict) (bool, error) {
	ap, err := s.DereferenceDict(widget["AP"])
	if err != nil {
		return false, err
	}
	for _, key := range []string{"N", "R", "D"} {
		value, err := s.Dereference(ap[key])
		if err != nil {
			return false, err
		}
		switch value := value.(type) {
		case types.StreamDict:
			return true, nil
		case types.Dict:
			for _, state := range slices.Sorted(maps.Keys(value)) {
				appearance, err := s.Dereference(value[state])
				if err != nil {
					return false, err
				}
				if _, live := appearance.(types.StreamDict); live {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func finiteSignatureMatrix(m matrix.Matrix) bool {
	for _, row := range m {
		for _, v := range row {
			if math.IsInf(v, 0) || math.IsNaN(v) {
				return false
			}
		}
	}
	return true
}

// The profile allows opaque path painting. Programs with other operators
// require an independent compositing proof before they can be admitted.
const signatureAppearanceOperators = " q Q cm w J j M d i ri m l c v y h re S s f F f* B B* b b* n W W* G g RG rg K k " +
	"BT ET Tc Tw Tz TL Tr Ts Td TD Tm T* Tj TJ ' \" "

func inspectSignatureAppearance(c context.Context, s *model.Context, program *layerPrograms, stream types.StreamDict, inherited types.Dict, site sourceSite) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	bad := signatureUnsupported(site.key("AP"), diag.SignatureCompositing)
	data, err := program.decode(c, s, stream)
	if err != nil {
		return digest, err
	}
	resources, err := s.DereferenceDict(stream.Dict["Resources"])
	if err != nil {
		return digest, err
	}
	if resources == nil {
		resources = inherited
	}
	hash := sha256.New()
	hash.Write(data)
	// Device color operators and the initial DeviceGray state can use
	// default-space substitutions in the effective resource dictionary.
	identity, err := signatureDeviceColors(c, s, resources)
	if err != nil {
		return digest, err
	}
	hash.Write([]byte(identity))
	fontIdentities := map[types.Name]string{}
	depth := 0
	err = program.scan(c, append(bytes.Clone(data), '\n'), nil, site, layerVerify, func(op layerOperation) error {
		switch op.name {
		case "q":
			depth++
			if len(op.args) != 0 {
				return bad
			}
		case "Q":
			depth--
			if depth < 0 || len(op.args) != 0 {
				return bad
			}
		case "Tf":
			// Type 3 glyphs execute arbitrary PDF drawing programs.
			if len(op.args) != 2 {
				return bad
			}
			name, ok := op.args[0].value.(types.Name)
			if !ok {
				return bad
			}
			identity, found := fontIdentities[name]
			if !found {
				fonts, err := s.DereferenceDict(resources["Font"])
				if err != nil {
					return err
				}
				font, err := s.DereferenceDict(fonts[string(name)])
				if err != nil || font == nil {
					return bad
				}
				if subtype := font.Subtype(); subtype == nil || *subtype == "Type3" {
					return bad
				}
				identity, err = resourceIdentity(c, s, font)
				if err != nil {
					return err
				}
				fontIdentities[name] = identity
			}
			hash.Write([]byte(identity))
			hash.Write([]byte{0})
		default:
			if !strings.Contains(signatureAppearanceOperators, " "+op.name+" ") {
				return bad
			}
		}
		return nil
	})
	if err != nil || depth != 0 {
		if c.Err() != nil {
			return digest, c.Err()
		}
		return digest, bad
	}
	for _, key := range []string{"BBox", "Matrix"} {
		numbers, err := fitNumbers(s, stream.Dict[key])
		if err != nil {
			return digest, err
		}
		for _, n := range numbers {
			hash.Write([]byte(number(n)))
			hash.Write([]byte{0})
		}
	}
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

func signatureDeviceColors(c context.Context, s *model.Context, resources types.Dict) (string, error) {
	spaces, err := s.DereferenceDict(resources["ColorSpace"])
	if err != nil {
		return "", err
	}
	defaults := types.Array{spaces["DefaultGray"], spaces["DefaultRGB"], spaces["DefaultCMYK"]}
	return resourceIdentity(c, s, defaults)
}

func (p *signatureDiscard) planPageArtwork(c context.Context, s *model.Context, page pageRef, content pageContent, program *layerPrograms, drawings []signatureDrawing, site sourceSite) error {
	data, ends, err := program.page(c, s, content, site)
	if err != nil {
		return err
	}
	// Work with actual stream boundaries. A trailing comment in an original
	// stream must also terminate before the next stream begins.
	var padded []byte
	var paddedEnds []int
	start := 0
	for _, end := range ends {
		padded = append(padded, data[start:end]...)
		padded = append(padded, '\n')
		paddedEnds = append(paddedEnds, len(padded))
		start = end
	}
	bad := signatureUnsupported(site.key("Contents"), diag.SignatureContentBoundary)
	graphics, text, marked := 0, 0, 0
	invoked := map[string]bool{}
	err = program.scan(c, padded, paddedEnds, site, layerVerify, func(op layerOperation) error {
		if name, ok := signatureDoName(op); ok {
			invoked[string(name)] = true
		}
		switch op.name {
		case "q", "Q", "BT", "ET", "EMC":
			if len(op.args) != 0 {
				return bad
			}
		}
		switch op.name {
		case "q":
			graphics++
		case "Q":
			graphics--
		case "BT":
			text++
		case "ET":
			text--
		case "BMC", "BDC":
			marked++
		case "EMC":
			marked--
		}
		if graphics < 0 || text < 0 || text > 1 || marked < 0 {
			return bad
		}
		return nil
	})
	if err != nil || graphics != 0 || text != 0 || marked != 0 {
		if c.Err() != nil {
			return c.Err()
		}
		return bad
	}
	resources, err := effectiveLayerResources(s, page, site)
	if err != nil {
		return err
	}
	pageColors, err := signatureDeviceColors(c, s, resources)
	if err != nil {
		return err
	}
	command := []byte("\nn\nQ\n")
	for _, drawing := range drawings {
		var name string
		resources, name, err = withFormResourceAvoiding(s, resources, "PCSignature", drawing.raw, invoked)
		if err != nil {
			return err
		}
		art := drawing.art
		art.name, art.pageColors = name, pageColors
		p.artwork = append(p.artwork, art)
		command = append(command, signatureArtworkCommand(art)...)
	}
	prefix, err := addStream(s, []byte("q\n"), nil)
	if err != nil {
		return err
	}
	tail, err := addStream(s, command, nil)
	if err != nil {
		return err
	}
	entries := types.Array{*prefix}
	for _, stream := range content.streams {
		entries = append(entries, stream.ref)
	}
	entries = append(entries, *tail)
	p.edits = append(p.edits, func() {
		page.dict["Contents"], page.dict["Resources"] = entries, resources
	})
	return nil
}

func signatureArtworkCommand(art signatureArtwork) []byte {
	command := fmt.Appendf(nil, "%s\nq\n", signatureArtworkTag)
	command = append(command, matrixCommand(art.placement)...)
	return fmt.Appendf(command, "/%s Do\nQ\nEMC\n", art.name)
}

func signatureMarker(op layerOperation) bool {
	if op.name != "BDC" || len(op.args) != 2 || op.args[0].value != types.Name("Artifact") {
		return false
	}
	props, _ := op.args[1].value.(types.Dict)
	return props["PDFConcatSignatureArtwork"] == types.Boolean(true)
}
