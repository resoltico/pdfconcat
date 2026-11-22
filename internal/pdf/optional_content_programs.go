// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

const maxLayerProgramBytes = 256 << 20

type layerProgramMode uint8

const (
	layerGraphOnly layerProgramMode = iota
	layerDetect
	layerVerify
)

type layerPrograms struct {
	remaining    int64
	pdf20        bool
	decodeBudget *model.XRefTable
}

func (p *layerPrograms) decode(c context.Context, s *model.Context, stream types.StreamDict) ([]byte, error) {
	if p.decodeBudget == nil {
		return decodeDrawingStream(c, s, stream)
	}
	var decoder drawingDecoder
	return decoder.decodeWithBudget(c, s, stream, p.decodeBudget)
}

func (p *layerPrograms) stream(c context.Context, s *model.Context, stream types.StreamDict, role layerRole, kind, subtype types.Name, site sourceSite, wrappers *pageWrappers, mode layerProgramMode, stage bool, ref types.IndirectRef, verified map[types.IndirectRef]bool) error {
	if verified[ref] {
		return nil
	}
	bad := func() error { return layerRefusal(site.key("Stream"), diag.OptionalContentProgramUnproved()) }
	pattern, err := s.Dereference(stream.Dict["PatternType"])
	if err != nil {
		return err
	}
	switch {
	case subtype == "Form", pattern == types.Integer(1), role == layerGlyph, role == layerAppearance:
		if stream.Dict["F"] != nil {
			if mode == layerDetect {
				return nil
			}
			return bad()
		}
		data, e := p.decode(c, s, stream)
		if e != nil {
			if c.Err() != nil {
				return c.Err()
			}
			if mode == layerDetect {
				return nil
			}
			return bad()
		}
		properties := func() (types.Dict, error) {
			return drawingLayerProperties(s, stream.Dict["Resources"], site)
		}
		if wrappers == nil {
			names, err := p.markedNames(c, data, nil, site.key("Stream"), mode)
			if err != nil {
				return err
			}
			if mode == layerDetect && len(names) != 0 {
				// Older Forms can borrow page resources. This graph walk
				// visits definitions, so a missing local scope does not prove
				// that a name is unresolved in any invocation.
				resources, err := layerValue(s, stream.Dict["Resources"], site.key("Resources"))
				if err != nil || resources == nil {
					return err
				}
			}
			err = checkLayerNames(s, names, properties, site.key("Stream"), mode)
			if err == nil && verified != nil && mode == layerVerify {
				verified[ref] = true
			}
			return err
		}
		return p.scan(c, data, nil, site.key("Stream"), layerVerify, func(op layerOperation) error {
			if op.inlineLayer() {
				return bad()
			}
			if name, named := op.visibilityName(); named {
				if err := checkLayerNames(s, layerNamesInProgram{name: true}, properties, site.key("Stream"), layerVerify); err != nil {
					return err
				}
			}
			name, named := layerPropertyName(op)
			if !named || !wrappers.names[name] {
				return nil
			}
			// An explicit local scope can prove an independent use. Forms
			// and other programs with no local scope may inherit a binding.
			props, err := properties()
			if err != nil {
				return err
			}
			if props[string(name)] == nil || wrappers.properties[layerDictIdentity(props)][string(name)].owner != nil {
				return bad()
			}
			return nil
		})
	case subtype == "Image", kind == "Metadata", role == layerPassive, role == layerPageContent:
		return nil
	default:
		if stage || mode == layerDetect {
			return nil // Inspect known drawing roles.
		}
		return bad()
	}
}

func verifyLayerPrograms(c context.Context, s *model.Context, pages []pageRef, contents pageContentSnapshot) error {
	p := layerPrograms{remaining: maxLayerProgramBytes, pdf20: s.PDF20()}
	for i, page := range contents {
		site := sourceSite{ref: page.ref, page: i + 1}.key("Contents")
		data, ends, err := p.page(c, s, page, site)
		if err != nil {
			return err
		}
		names, err := p.markedNames(c, data, ends, site, layerVerify)
		if err != nil {
			return err
		}
		if err := checkLayerNames(s, names, pageLayerProperties(s, pages[i], site), site, layerVerify); err != nil {
			return err
		}
	}
	return nil
}

// Page programs are decoded in array order. The parser checks that every
// lexical unit is contained in one stream, including comments and inline images.
func (p *layerPrograms) page(c context.Context, s *model.Context, page pageContent, site sourceSite) ([]byte, []int, error) {
	if p.remaining <= 0 {
		return nil, nil, layerRefusal(site, diag.OptionalContentProgramUnproved())
	}
	var data []byte
	ends := make([]int, 0, len(page.streams))
	for _, entry := range page.streams {
		obj, err := s.Dereference(entry.ref)
		if err != nil {
			return nil, nil, err
		}
		stream, ok := obj.(types.StreamDict)
		if !ok || stream.Dict["F"] != nil {
			return nil, nil, layerRefusal(site, diag.OptionalContentProgramUnproved())
		}
		part, err := p.decode(c, s, stream)
		length := int64(len(data)) + int64(len(part))
		if length >= p.remaining {
			p.remaining = 0
		}
		if err != nil || p.remaining == 0 || checkContentLength(length, s.DecodeLimit()) != nil {
			if c.Err() != nil {
				return nil, nil, c.Err()
			}
			return nil, nil, layerRefusal(site, diag.OptionalContentProgramUnproved())
		}
		data = append(data, part...)
		ends = append(ends, len(data))
	}
	return data, ends, nil
}

type layerSpan struct {
	start, end int
}

type layerOperand struct {
	value types.Object
	layerSpan
}

type layerOperation struct {
	name string
	args []layerOperand
	layerSpan
}

// ISO 32000-1 §8.11.3.2 permits ordinary uses of the OC tag. With a
// visibility property, BDC starts a sequence and DP references a group.
func (op layerOperation) visibilityProperty() types.Object {
	if (op.name == "BDC" || op.name == "DP") && len(op.args) == 2 && op.args[0].value == types.Name("OC") {
		return op.args[1].value
	}
	return nil
}

func (op layerOperation) visibilityName() (types.Name, bool) {
	name, ok := op.visibilityProperty().(types.Name)
	return name, ok
}

func (op layerOperation) inlineLayer() bool {
	d, ok := op.visibilityProperty().(types.Dict)
	return ok && (d["Type"] == types.Name("OCG") || d["Type"] == types.Name("OCMD") || visibilityMembershipShape(d))
}

func visibilityMembershipShape(d types.Dict) bool {
	switch d["OCGs"].(type) {
	case types.Array, types.IndirectRef:
		return true
	}
	_, expression := d["VE"].(types.Array)
	return expression
}

// scan reports exact operand and operator spans. Non-operator payloads remain
// opaque, including literal strings and inline image data.
func (p *layerPrograms) scan(c context.Context, data []byte, ends []int, site sourceSite, mode layerProgramMode, observe func(layerOperation) error) error {
	bad := layerRefusal(site, diag.OptionalContentProgramUnproved())
	p.remaining -= int64(len(data)) + 1
	if p.remaining < 0 {
		return bad
	}
	insideStream := func(start, end int) bool {
		for len(ends) > 0 && ends[0] <= start {
			ends = ends[1:]
		}
		return len(ends) == 0 || end <= ends[0]
	}
	rest, args := string(data), make([]layerOperand, 0, 8)
	depth := 0
	for len(rest) > 0 {
		if err := c.Err(); err != nil {
			return err
		}
		rest = strings.TrimLeft(rest, "\x00\t\n\f\r ")
		if rest == "" {
			break
		}
		start := len(data) - len(rest)
		if rest[0] == '%' {
			n := strings.IndexAny(rest, "\r\n")
			if n < 0 {
				n = len(rest)
			}
			if !insideStream(start, start+n) {
				return bad
			}
			rest = rest[n:]
			continue
		}
		if strings.ContainsRune("/[(<+-.0123456789", rune(rest[0])) || strings.HasPrefix(rest, "true") || strings.HasPrefix(rest, "false") || strings.HasPrefix(rest, "null") {
			value, err := model.ParseObjectWithPolicy(c, &rest, 0, model.ValidationStrict, 256)
			end := len(data) - len(rest)
			if err != nil || value.StrictFailure != nil || end <= start || len(args) == 256 || !insideStream(start, end) {
				if c.Err() != nil {
					return c.Err()
				}
				return bad
			}
			args = append(args, layerOperand{value.Object, layerSpan{start, end}})
			continue
		}
		n := strings.IndexAny(rest, "\x00\t\n\f\r /[]<>()%")
		if n < 0 {
			n = len(rest)
		}
		if n == 0 {
			return bad
		}
		op := layerOperation{rest[:n], args, layerSpan{start, start + n}}
		rest = rest[n:]
		switch op.name {
		case "BI":
			if len(args) != 0 {
				return bad
			}
			var err error
			rest, err = inlineTail(c, rest, &p.remaining, 0, p.pdf20)
			if err != nil {
				if c.Err() != nil {
					return c.Err()
				}
				return bad
			}
		case "BX":
			depth++
		case "EX":
			depth--
		}
		if !insideStream(start, len(data)-len(rest)) || mode == layerVerify && (depth < 0 ||
			depth == 0 && !strings.Contains(drawingOperators, " "+op.name+" ") && op.name != "BI") {
			return bad
		}
		if err := observe(op); err != nil {
			return err
		}
		if mode == layerVerify && !op.ordinaryMarked() {
			return bad
		}
		args = args[:0]
	}
	if mode == layerVerify && (depth != 0 || len(args) != 0) {
		return bad
	}
	return c.Err()
}

func (op layerOperation) ordinaryMarked() bool {
	switch op.name {
	case "BDC", "DP":
		return len(op.args) == 2 && ordinaryMarkedTag(op.args[0].value) && ordinaryMarkedProperty(op.args[1].value)
	case "BMC", "MP":
		return len(op.args) == 1 && ordinaryMarkedTag(op.args[0].value)
	default:
		return true
	}
}

func ordinaryMarkedTag(value types.Object) bool {
	_, ok := value.(types.Name)
	return ok
}

func ordinaryMarkedProperty(value types.Object) bool {
	switch value.(type) {
	case types.Name, types.Dict:
		return true
	default:
		return false
	}
}

type layerNamesInProgram map[types.Name]bool

func layerOperatorRefusal(site sourceSite, mode layerProgramMode) error {
	if mode == layerDetect {
		return layerRefusal(site, diag.OptionalContentOperatorFound())
	}
	return layerRefusal(site, diag.OptionalContentProgramUnproved())
}

// Detection acts on successfully tokenized evidence. Unsupported syntax or
// an ambiguous inline image ends observation of the remaining program.
func (p *layerPrograms) markedNames(c context.Context, data []byte, ends []int, site sourceSite, mode layerProgramMode) (layerNamesInProgram, error) {
	var names layerNamesInProgram
	var confirmed error
	err := p.scan(c, data, ends, site, mode, func(op layerOperation) error {
		if op.inlineLayer() {
			confirmed = layerOperatorRefusal(site, mode)
			return confirmed
		}
		if name, ok := op.visibilityName(); ok {
			if names == nil {
				names = layerNamesInProgram{}
			}
			names[name] = true
		}
		return nil
	})
	if mode == layerDetect && confirmed == nil {
		err = c.Err()
	}
	return names, err
}

func pageLayerProperties(s *model.Context, page pageRef, site sourceSite) func() (types.Dict, error) {
	return func() (types.Dict, error) {
		_, props, err := layerPagePropertiesFor(s, page, site)
		return props, err
	}
}

func namedVisibilityProperty(s *model.Context, props types.Dict, name types.Name, site sourceSite) (bool, error) {
	value, err := layerValue(s, props[string(name)], site)
	if err != nil {
		return false, err
	}
	d, ok := value.(types.Dict)
	if !ok {
		return true, nil // An unresolved named property could have been a layer.
	}
	kind, err := layerValue(s, d["Type"], site)
	return kind == types.Name("OCG") || kind == types.Name("OCMD") || visibilityMembershipShape(d), err
}

func checkLayerNames(s *model.Context, names layerNamesInProgram, properties func() (types.Dict, error), site sourceSite, mode layerProgramMode) error {
	if len(names) == 0 {
		return nil
	}
	props, err := properties()
	if err != nil {
		return err
	}
	for name := range names {
		optional, err := namedVisibilityProperty(s, props, name, site)
		if err != nil {
			return err
		}
		if optional {
			return layerOperatorRefusal(site, mode)
		}
	}
	return nil
}

// Keep decoded-size limits with the backend. An independent program gets its
// full decoded allowance plus bounded inline-image lookahead. A packet may
// repeat pages or create a fitting Form from an entire joined page program.
func independentLayerProgram(s *model.Context, budget *model.XRefTable) layerPrograms {
	limit := s.DecodeLimit()
	if limit < 0 {
		limit = maxLayerProgramBytes
	}
	return layerPrograms{remaining: limit + 1 + min(limit, maxLayerProgramBytes), decodeBudget: budget, pdf20: s.PDF20()}
}

type layerProgramEvidence struct {
	pages   map[[sha256.Size]byte]layerNamesInProgram
	streams map[types.IndirectRef]bool
	budget  *model.XRefTable
}

func newLayerProgramEvidence(s *model.Context) *layerProgramEvidence {
	// Best-effort detection has its own document-wide decode allowance. It
	// cannot consume the budget used by validation or strict stage proof.
	return &layerProgramEvidence{
		pages: map[[sha256.Size]byte]layerNamesInProgram{}, streams: map[types.IndirectRef]bool{},
		budget: &model.XRefTable{Conf: s.Conf},
	}
}

func (e *layerProgramEvidence) program(s *model.Context, mode layerProgramMode) layerPrograms {
	budget := e.budget
	if mode == layerVerify {
		// Strict stage verification has a bounded allowance for each unique
		// program, including intermediate filters. Packet cardinality does
		// not consume another program's proof or the validator's allowance.
		budget = &model.XRefTable{Conf: s.Conf}
	}
	return independentLayerProgram(s, budget)
}

// Use the ordered full identities within this context. Two different resource
// scopes sharing the same lexical program still get their own graph checks.
func layerPageProgramKey(page pageContent) [sha256.Size]byte {
	hash := sha256.New()
	var entry [2 * binary.MaxVarintLen64]byte
	for _, stream := range page.streams {
		key := binary.AppendVarint(entry[:0], int64(stream.ref.ObjectNumber))
		key = binary.AppendVarint(key, int64(stream.ref.GenerationNumber))
		hash.Write(key)
	}
	return [sha256.Size]byte(hash.Sum(nil))
}

func detectLayerPagePrograms(c context.Context, s *model.Context, pages []pageRef, content pageContentSnapshot, evidence *layerProgramEvidence) error {
	for i, page := range content {
		if err := c.Err(); err != nil {
			return err
		}
		site := sourceSite{ref: page.ref, page: i + 1}.key("Contents")
		if err := evidence.checkPage(c, s, pages[i], page, site, layerDetect); err != nil {
			return err
		}
	}
	return nil
}

func (e *layerProgramEvidence) checkPage(c context.Context, s *model.Context, page pageRef, program pageContent, site sourceSite, mode layerProgramMode) error {
	key := layerPageProgramKey(program)
	names, known := e.pages[key]
	if !known {
		p := e.program(s, mode)
		data, ends, err := p.page(c, s, program, site)
		if err != nil {
			if mode == layerDetect && optionalContentRefusal(err) {
				e.pages[key] = nil // Existing encoding checks own ordinary failures.
				return nil
			}
			return err
		}
		if mode == layerDetect {
			ends = nil // Observe the backend's concatenated drawing program.
		}
		names, err = p.markedNames(c, data, ends, site, mode)
		if err != nil {
			return err
		}
		e.pages[key] = names
	}
	return checkLayerNames(s, names, pageLayerProperties(s, page, site), site, mode)
}

// Scope strict lexical verification to affected physical pages. Their effective
// resources also reach Forms introduced by fitting.
func verifySerializedWrapperPages(c context.Context, s *model.Context, pages []pageRef, content pageContentSnapshot, affected []int64, evidence *layerProgramEvidence) error {
	var roots []layerEdge
	for _, number := range affected {
		if err := c.Err(); err != nil {
			return err
		}
		if number < 1 || number > int64(len(pages)) {
			return fmt.Errorf("missing physical page %d for optional-content verification", number)
		}
		page := pages[number-1]
		site := sourceSite{ref: page.ref, page: int(number)}
		if err := evidence.checkPage(c, s, page, content[number-1], site.key("Contents"), layerVerify); err != nil {
			return err
		}
		resources, err := effectiveLayerResources(s, page, site)
		if err != nil {
			return err
		}
		roots = append(roots,
			layerEdge{resources, site.key("Resources"), layerResources},
			layerEdge{page.dict["Annots"], site.key("Annots"), layerAnnotation},
		)
	}
	return scanLayerEdges(c, s, sourceOrigins{}, roots, &optionalContentPlan{}, layerVerify, true, evidence)
}
