// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/fault"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
)

// OccurrenceFacts describes one retained selection, without duplicating source identity.
type OccurrenceFacts struct {
	packet.SourceFacts
	DestNames                 []string
	HasCoordinateDestinations bool
	PDF20AppearanceIssue      string
	AnnotationFittingIssue    string
	OutlineDepth              int
	StructureCoverage         string
	HasUncoveredContent       bool
}

// SourceFacts retains immutable whole-file identity and policy facts.
type SourceFacts struct {
	OccurrenceFacts
	PageSizes                              map[int]packet.Size
	SubsetPrefixes                         []string
	SHA256                                 [32]byte
	Bytes                                  int64
	Version                                string
	FormDefaultsDigest                     string
	HasFields, HasWidgets, NeedAppearances bool
}

type policyError struct {
	finding diag.Diagnostic
	err     error
}

func (e *policyError) Error() string { return e.finding.Message }
func (e *policyError) Unwrap() error { return e.err }
func refuse(code diag.Code, message string) error {
	return &policyError{finding: diag.New(code, message), err: errors.New(message)}
}

func pdfFailure(code diag.Code, message string, err error) error {
	d := diag.New(code, message)
	d.Detail = strings.ToValidUTF8(err.Error(), "�")
	var policy *policyError
	if errors.As(err, &policy) {
		d.PDFLocation = policy.finding.PDFLocation
		if policy.finding.Detail != "" {
			d.Detail += ": " + policy.finding.Detail
		}
	}
	return &policyError{finding: d, err: err}
}

func diagnostic(err error, fallback diag.Code) diag.Diagnostic {
	code := fallback
	var policy *policyError
	errors.As(err, &policy)
	var backendFault fault.Panic
	if code == diag.SourceInvalid && errors.As(err, &backendFault) {
		code = diag.SourceProcessingFailed
	}
	d := diag.New(code, err.Error())
	if policy != nil {
		d = policy.finding
	}
	if policy == nil && code == diag.SourceInvalid {
		d.Message = "The PDF could not be parsed or validated. Obtain a reviewed repair or deliver the original separately."
		d.Detail = strings.ToValidUTF8(err.Error(), "�")
	}
	if policy == nil && code == diag.SourceProcessingFailed {
		d.Message = "The PDF backend failed unexpectedly."
		d.Detail = strings.ToValidUTF8(err.Error(), "�")
	}
	var attributed *model.ValidationError
	if d.PDFLocation == nil && errors.As(err, &attributed) && attributed.ObjectNumber() > 0 {
		d.PDFLocation = &diag.PDFLocation{Object: attributed.ObjectNumber()}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		interrupted := diag.New(diag.Interrupted, "The operation was interrupted.")
		d.Code, d.Severity, d.Message = interrupted.Code, interrupted.Severity, interrupted.Message
	}
	return d
}

func readSource(c context.Context, data []byte) (s *model.Context, ds []diag.Diagnostic, err error) {
	defer func() {
		if value := recover(); value != nil {
			err = pdfFailure(diag.SourceProcessingFailed, "The PDF backend failed unexpectedly.", fmt.Errorf("%v", value))
		}
	}()
	// pdfcpu v0.16.4 scans the whole file, including displaced headers.
	// Recognition is deliberately weaker than parsing: a truncated recognizable
	// header is malformed PDF, not a different input format.
	if !bytes.Contains(data, []byte("%PDF-")) {
		return nil, nil, refuse(diag.SourceNotPDF, "The source is not a PDF: no recognizable PDF header was found.")
	}
	conf, err := configuration()
	if err != nil {
		return s, ds, pdfFailure(diag.SourceProcessingFailed, "The PDF backend could not be initialized.", err)
	}
	conf.ValidationMode = model.ValidationStrict
	s, err = api.ReadContext(c, bytes.NewReader(data), conf)
	if errors.Is(err, pdfcpu.ErrWrongPassword) || errors.Is(err, pdfcpu.ErrUnsupportedEncryptionFeature) || s != nil && s.Encrypt != nil {
		return s, ds, refuse(diag.SourceEncrypted, "Encrypted sources are unsupported, including PDFs that open without a password. Request an unrestricted, unencrypted assembly copy or deliver the originals separately.")
	}
	if err != nil {
		return s, ds, err
	}
	if err = checkDocument(c, s); err != nil {
		return s, ds, err
	}
	cat, err := s.Catalog()
	if err != nil {
		return s, ds, err
	}
	// Remove only source viewer directives, after inspecting the original graph.
	// Other discarded catalog structures still require their own validation.
	for _, key := range []string{"PageLayout", "PageMode", "ViewerPreferences"} {
		if _, present := cat[key]; !present {
			continue
		}
		delete(cat, key)
		d := diag.New(diag.SourceViewerSettingsRemoved, "Source viewer settings are omitted from composed output.")
		d.PDFLocation = &diag.PDFLocation{Object: s.Root.ObjectNumber.Value(), Path: "Catalog." + key}
		ds = append(ds, d)
	}
	s.PageLayout, s.PageMode, s.ViewerPref = nil, nil, nil
	// Parsing is strict; semantic compatibility validation tolerates ancillary
	// producer differences while page/content preservation is checked directly.
	s.Configuration.ValidationMode = model.ValidationRelaxed
	s.XRefTable.ValidationMode = model.ValidationRelaxed
	report, err := validatePreservingPages(c, s)
	findings, repair := validationNotices(report)
	ds = append(ds, findings...)
	if err != nil {
		if repair != nil {
			ds = append(ds, diagnostic(repair, diag.SourceUnsupported))
		}
		return s, ds, err
	}
	if repair != nil {
		return s, ds, repair
	}
	if err = validateStructure(c, s); err != nil {
		return s, ds, pdfFailure(diag.SourceInvalid, "The source structure is malformed or unsupported.", err)
	}
	if err = validateContentEncoding(c, s); err != nil {
		return s, ds, err
	}

	return s, ds, nil
}

// checkDocument enforces structural and fidelity safeguards on original source
// graphs and serialized output alike. Viewer normalization is not part of it.
func checkDocument(c context.Context, s *model.Context) error {
	pages, err := rawPages(c, s)
	if err != nil {
		return err
	}
	s.PageCount = len(pages)
	if err = walkObjects(c, s, func(d types.Dict) error {
		if resource, ok := d["Resources"]; ok {
			resourceDict, e := s.DereferenceDict(resource)
			if e != nil {
				return e
			}
			resourceType, e := nameValue(s, resourceDict["Type"])
			if e != nil {
				return e
			}
			resourceFieldType, e := s.Dereference(resourceDict["FT"])
			if e != nil {
				return e
			}
			if resourceType == types.Name("Annot") || resourceFieldType != nil {
				return refuse(diag.SourceUnsupported, "A resource dictionary shared with an annotation or field is unsupported; upstream validation would discard that role.")
			}
		}
		subtype, e := nameValue(s, d["Subtype"])
		if e != nil {
			return e
		}
		if subtype == types.Name("Widget") {
			rect, e := s.DereferenceArray(d["Rect"])
			if e != nil || len(rect) != 4 {
				return refuse(diag.SourceInvalid, "Widget Rect must contain four coordinates.")
			}
		}
		fieldType, e := nameValue(s, d["FT"])
		if e != nil {
			return e
		}
		if fieldType == types.Name("Sig") {
			value, e := s.Dereference(d["V"])
			if e != nil {
				return e
			}
			if value != nil {
				return refuse(diag.SourceSigned, "A populated signature field is unsupported because composition cannot preserve its signature assurance. Deliver the original separately; unsigned null fields are allowed.")
			}
		}
		kind, e := nameValue(s, d["Type"])
		if e != nil {
			return e
		}
		if kind == types.Name("Sig") || kind == types.Name("DocTimeStamp") {
			return refuse(diag.SourceSigned, "The source declares a signature or timestamp value whose assurance composition cannot preserve. Deliver the original separately; signatures are not authenticated.")
		}
		for key := range d {
			decoded, e := types.DecodeName(key)
			if e == nil && decoded != key {
				return refuse(diag.SourceUnsupported, fmt.Sprintf("Unsupported literal-hash dictionary key %q; upstream would change its identity.", key))
			}
		}
		return nil
	}); err != nil {
		return err
	}
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	for _, policy := range []struct {
		key     string
		code    diag.Code
		message string
	}{
		{"Perms", diag.SourceSigned, "The source declares catalog permission structures, which composition does not preserve. Deliver the original separately; certification is not authenticated."},
		{"DocTimeStamp", diag.SourceSigned, "Document timestamps are not supported."},
		{"OCProperties", diag.SourceOptionalContent, "The source declares optional-content settings (visibility layers), whose display state composition does not preserve. Deliver the original separately."},
	} {
		value, e := s.Dereference(cat[policy.key])
		if e != nil {
			return e
		}
		if value != nil {
			return refuse(policy.code, policy.message)
		}
	}
	rendering, e := s.Dereference(cat["NeedsRendering"])
	if e != nil {
		return e
	}
	if rendering == types.Boolean(true) {
		return refuse(diag.SourceDynamicForm, "The source requests dynamic rendering, whose generated appearance composition cannot preserve. Deliver the original separately.")
	}
	if _, err = fields(c, s); err != nil {
		return err
	}
	return nil
}

type pageTreeNode struct {
	dict   types.Dict
	parent *pageTreeNode
}

type pageRef struct {
	ref  types.IndirectRef
	dict types.Dict
	node *pageTreeNode
}

// rawPages rejects content-replacing tree repair before pdfcpu validation runs.
func rawPages(c context.Context, s *model.Context) ([]pageRef, error) {
	catalog, err := s.Catalog()
	if err != nil {
		return nil, err
	}
	visited := map[types.IndirectRef]bool{}
	var pages []pageRef
	var walk func(types.Object, int, *pageTreeNode) (int, error)
	walk = func(o types.Object, depth int, parent *pageTreeNode) (int, error) {
		if err := c.Err(); err != nil {
			return 0, err
		}
		if depth > 64 {
			return 0, refuse(diag.SourceUnsupported, "Page tree exceeds the supported depth of 64.")
		}
		ref, indirect := o.(types.IndirectRef)
		if !indirect || visited[ref] {
			return 0, refuse(diag.SourceDamaged, "Page tree contains a direct, repeated or cyclic node.")
		}
		if !liveReference(s, ref) {
			return 0, refuse(diag.SourceDamaged, "A page-tree reference is missing, free, or has the wrong object generation.")
		}
		visited[ref] = true
		d, err := s.DereferenceDict(o)
		if err != nil || len(d) == 0 {
			return 0, refuse(diag.SourceDamaged, fmt.Sprintf("Page-tree object %s is missing or empty.", ref))
		}
		typ, err := s.Dereference(d["Type"])
		if err != nil {
			return 0, err
		}
		node := &pageTreeNode{dict: d, parent: parent}
		switch typ {
		case types.Name("Page"):
			kids, err := s.Dereference(d["Kids"])
			if err != nil {
				return 0, err
			}
			if kids != nil {
				return 0, refuse(diag.SourceDamaged, "A Page has non-null Kids; readers may drop its content.")
			}
			pages = append(pages, pageRef{ref: ref, dict: d, node: node})
			return 1, nil
		case types.Name("Pages"):
			kids, err := s.DereferenceArray(d["Kids"])
			if err != nil || len(kids) == 0 {
				return 0, refuse(diag.SourceDamaged, "Pages requires nonempty Kids.")
			}
			count := 0
			for _, kid := range kids {
				n, err := walk(kid, depth+1, node)
				if err != nil {
					return 0, err
				}
				count += n
			}
			v, err := s.Dereference(d["Count"])
			if err != nil || v != types.Integer(count) {
				return 0, refuse(diag.SourceDamaged, fmt.Sprintf("Page-tree Count disagrees with %d leaves.", count))
			}
			return count, nil
		default:
			return 0, refuse(diag.SourceDamaged, fmt.Sprintf("Page-tree node %s has no supported Type.", ref))
		}
	}
	_, err = walk(catalog["Pages"], 0, nil)
	return pages, err
}

// walkObjects visits direct containers in all materialized indirect objects once.
// Indirect links are handled by the outer xref loop, avoiding graph recursion.
func walkObjects(c context.Context, s *model.Context, visit func(types.Dict) error) error {
	var walk func(types.Object, int) error
	walk = func(o types.Object, depth int) error {
		if depth > 128 {
			return refuse(diag.SourceUnsupported, "Direct object nesting exceeds 128.")
		}
		switch v := o.(type) {
		case types.Dict:
			if err := visit(v); err != nil {
				return err
			}
			for _, child := range v {
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		case types.StreamDict:
			return walk(v.Dict, depth+1)
		case types.Name:
			decoded, err := types.DecodeName(string(v))
			if err == nil && decoded != string(v) {
				return refuse(diag.SourceUnsupported, "Unsupported literal-hash name identity; upstream may decode it twice.")
			}
		case types.Array:
			for _, child := range v {
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for n, e := range s.Table {
		if err := c.Err(); err != nil {
			return err
		}
		if e == nil || e.Free {
			continue
		}
		o, err := s.Dereference(types.IndirectRef{ObjectNumber: types.Integer(n), GenerationNumber: types.Integer(generation(e))})
		if err != nil {
			return err
		}
		if err = walk(o, 0); err != nil {
			return err
		}
	}
	return nil
}

func observe(c context.Context, s *model.Context) (SourceFacts, []diag.Diagnostic, error) {
	facts := SourceFacts{OccurrenceFacts: OccurrenceFacts{SourceFacts: packet.SourceFacts{Pages: s.PageCount}}, Version: s.XRefTable.Version().String()}
	prefixes, err := importedSubsetPrefixes(c, s)
	facts.SubsetPrefixes = prefixes
	if err != nil {
		return facts, nil, err
	}
	catalog, err := s.Catalog()
	if err != nil {
		return facts, nil, err
	}
	marked, e := markedContent(c, s)
	if e != nil {
		return facts, nil, e
	}
	facts.StructureCoverage = marked.coverage()
	facts.HasUncoveredContent = !marked.complete

	var ds []diag.Diagnostic
	add := func(code diag.Code, message string) {
		for _, d := range ds {
			if d.Code == code && d.Message == message {
				return
			}
		}
		ds = append(ds, diag.New(code, message))
	}

	for _, entry := range []struct {
		key  string
		code diag.Code
	}{{"PageLabels", diag.SourcePageLabelsRemoved}, {"OutputIntents", diag.SourceOutputIntentsRemoved}, {"OpenAction", diag.SourceDocumentActionsRemoved}, {"AA", diag.SourceDocumentActionsRemoved}, {"AF", diag.SourceAttachmentsRemoved}} {
		if catalog[entry.key] != nil {
			add(entry.code, entry.key+" will be removed from the output catalog.")
		}
	}
	names, err := s.DereferenceDict(catalog["Names"])
	if err != nil {
		return facts, ds, err
	}
	if names["JavaScript"] != nil {
		add(diag.SourceDocumentActionsRemoved, "Document JavaScript will be removed.")
	}
	if names["EmbeddedFiles"] != nil {
		add(diag.SourceAttachmentsRemoved, "Embedded-file indexes will be removed.")
	}
	form, err := s.DereferenceDict(catalog["AcroForm"])
	if err != nil {
		return facts, ds, err
	}
	if form["XFA"] != nil {
		add(diag.SourceXfaRemoved, "XFA will be removed; static fields remain.")
	}
	rendering, err := s.Dereference(form["NeedAppearances"])
	if err != nil {
		return facts, ds, err
	}
	facts.NeedAppearances = rendering == types.Boolean(true)
	roots, err := s.DereferenceArray(form["Fields"])
	if err != nil {
		return facts, ds, err
	}
	facts.HasFields = len(roots) > 0
	if facts.NeedAppearances && facts.HasFields {
		add(diag.FormAppearanceRegenerationRequested, "The source requests viewer-regenerated field appearances.")
	}
	if facts.HasFields {
		signature := types.Dict{"DR": form["DR"], "DA": form["DA"], "Q": form["Q"]}
		facts.FormDefaultsDigest, err = resourceIdentity(s, signature)
		if err != nil {
			return facts, ds, err
		}
	}
	uri, err := s.DereferenceDict(catalog["URI"])
	if err != nil {
		return facts, ds, err
	}
	uriBase, err := s.Dereference(uri["Base"])
	if err != nil {
		return facts, ds, err
	}
	err = walkObjects(c, s, func(d types.Dict) error {
		subtype, e := nameValue(s, d["Subtype"])
		if e != nil {
			return e
		}
		if subtype == types.Name("Widget") {
			facts.HasWidgets = true
		}

		action, e := nameValue(s, d["S"])
		if e != nil {
			return e
		}
		if uriBase != nil && action == types.Name("URI") {
			target, e := textValue(s, d["URI"])
			if e != nil {
				return e
			}
			u, e := url.Parse(target)
			if e == nil && !u.IsAbs() {
				add(diag.SourceURIBaseRemoved, "Source URI Base is removed; retained relative URI actions may resolve differently.")
			}
		}
		return nil
	})
	if err != nil {
		return facts, ds, err
	}
	geometries, err := pageGeometries(c, s)
	if err != nil {
		return facts, ds, err
	}
	facts.FirstSize = geometries[0].size
	facts.LastSize = geometries[len(geometries)-1].size

	destinations, err := readDestinations(c, s)
	if err != nil {
		return facts, ds, err
	}
	for name, d := range destinations {
		facts.DestNames = append(facts.DestNames, name)
		if d.page < 1 {
			add(diag.DestinationsDropped, "A destination outside its source document will be omitted.")
		}
	}
	facts.HasCoordinateDestinations, err = coordinateDestinations(c, s, destinations)
	if err != nil {
		return facts, ds, err
	}
	facts.PDF20AppearanceIssue, err = appearanceIssue(c, s)
	if err != nil {
		return facts, ds, err
	}
	facts.AnnotationFittingIssue, err = annotationFittingIssue(c, s, geometries)
	if err != nil {
		return facts, ds, err
	}
	facts.Selection = []packet.PageRange{{First: 1, Last: s.PageCount}}
	facts.OutlineDepth, err = outlineDepth(c, s)
	if err != nil {
		return facts, ds, err
	}
	slices.Sort(facts.DestNames)
	return facts, ds, nil
}

func generation(e *model.XRefTableEntry) int {
	if e.Generation == nil {
		return 0
	}
	return *e.Generation
}

// liveReference checks identity before dereferencing: the backend's low-level
// dereference API can resolve an object number despite a generation mismatch.
func liveReference(s *model.Context, ref types.IndirectRef) bool {
	e := s.Table[ref.ObjectNumber.Value()]
	return e != nil && !e.Free && generation(e) == ref.GenerationNumber.Value()
}
