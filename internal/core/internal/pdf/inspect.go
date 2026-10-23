// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
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
	OutlineDepth              int
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
	code  diag.Code
	err   error
	file  string
	items []string
}

func (e *policyError) Error() string { return e.err.Error() }
func (e *policyError) Unwrap() error { return e.err }
func refuse(code diag.Code, message string) error {
	return &policyError{code: code, err: errors.New(message)}
}

func diagnostic(err error, fallback diag.Code) diag.Diagnostic {
	code := fallback
	var policy *policyError
	if errors.As(err, &policy) {
		code = policy.code
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code = "interrupted"
	}
	d := diag.New(code, err.Error())
	if policy != nil {
		d.File = policy.file
		d.Items = policy.items
	}
	return d
}

// Inspect applies raw-tree/policy refusals before relaxed validation and records facts.
func Inspect(c context.Context, data []byte) (facts SourceFacts, ds []diag.Diagnostic) {
	digest := sha256.Sum256(data)
	defer func() {
		if value := recover(); value != nil {
			facts = SourceFacts{}
			ds = []diag.Diagnostic{diag.New("source_invalid", fmt.Sprintf("PDF parser panic: %v", value))}
		}
		facts.SHA256 = digest
		facts.Bytes = int64(len(data))
	}()
	s, err := read(c, data)
	if err != nil {
		if s != nil {
			facts.Pages = s.PageCount
			facts.Version = s.XRefTable.Version().String()
		}
		return facts, []diag.Diagnostic{diagnostic(err, "source_invalid")}
	}
	facts, ds, err = observe(c, s)
	if err != nil {
		ds = append(ds, diagnostic(err, "source_invalid"))
		return facts, ds
	}
	return facts, ds
}

func read(c context.Context, data []byte) (s *model.Context, err error) {
	conf, err := configuration()
	if err != nil {
		return s, err
	}
	s, err = api.ReadContext(c, bytes.NewReader(data), conf)
	if err != nil {
		if errors.Is(err, pdfcpu.ErrWrongPassword) || errors.Is(err, pdfcpu.ErrUnsupportedEncryptionFeature) {
			return s, refuse("source_encrypted", "Encrypted sources are not supported (including password-protected input).")
		}
		return s, err
	}
	if s.Encrypt != nil {
		return s, refuse("source_encrypted", "Encrypted sources are not supported.")
	}
	pages, err := rawPages(c, s)
	if err != nil {
		return s, err
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
				return refuse("source_invalid", "A resource dictionary shared with an annotation or field is unsupported; upstream validation would discard that role.")
			}
		}
		subtype, e := nameValue(s, d["Subtype"])
		if e != nil {
			return e
		}
		if subtype == types.Name("Widget") {
			rect, e := s.DereferenceArray(d["Rect"])
			if e != nil || len(rect) != 4 {
				return refuse("source_invalid", "Widget Rect must contain four coordinates.")
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
				return refuse("source_signed", "Signature values are not supported; unsigned null fields are allowed.")
			}
		}
		kind, e := nameValue(s, d["Type"])
		if e != nil {
			return e
		}
		if kind == types.Name("Sig") || kind == types.Name("DocTimeStamp") {
			return refuse("source_signed", "Signature and timestamp values are not supported.")
		}
		for key := range d {
			decoded, e := types.DecodeName(key)
			if e == nil && decoded != key {
				return refuse("source_invalid", fmt.Sprintf("Unsupported literal-hash dictionary key %q; upstream would change its identity.", key))
			}
		}
		return nil
	}); err != nil {
		return s, err
	}
	cat, err := s.Catalog()
	if err != nil {
		return s, err
	}
	for _, policy := range []struct {
		key     string
		code    diag.Code
		message string
	}{
		{"Perms", "source_signed", "Certified sources are not supported."},
		{"DocTimeStamp", "source_signed", "Document timestamps are not supported."},
		{"OCProperties", "source_optional_content", "Optional content is not supported."},
	} {
		value, e := s.Dereference(cat[policy.key])
		if e != nil {
			return s, e
		}
		if value != nil {
			return s, refuse(policy.code, policy.message)
		}
	}
	rendering, e := s.Dereference(cat["NeedsRendering"])
	if e != nil {
		return s, e
	}
	if rendering == types.Boolean(true) {
		return s, refuse("source_dynamic_form", "Dynamic forms are not supported.")
	}
	if _, err = fields(c, s); err != nil {
		return s, err
	}
	report, err := api.ValidateContextWithReport(c, s)
	if err != nil {
		return s, err
	}
	for _, n := range report.Notices() {
		if (n.Disposition == model.NoticeRepaired || n.Disposition == model.NoticeSkipped) && (n.PageNumber > 0 || strings.Contains(strings.ToLower(n.Path), "page") || strings.Contains(strings.ToLower(n.Message), "page")) {
			return s, refuse("source_damaged", n.Message)
		}
	}
	return s, nil
}

type pageRef struct {
	ref  types.IndirectRef
	dict types.Dict
}

// rawPages rejects content-replacing tree repair before pdfcpu validation runs.
func rawPages(c context.Context, s *model.Context) ([]pageRef, error) {
	catalog, err := s.Catalog()
	if err != nil {
		return nil, err
	}
	visited := map[types.IndirectRef]bool{}
	var pages []pageRef
	var walk func(types.Object, int) (int, error)
	walk = func(o types.Object, depth int) (int, error) {
		if err := c.Err(); err != nil {
			return 0, err
		}
		if depth > 64 {
			return 0, refuse("source_damaged", "Page tree exceeds depth 64.")
		}
		ref, indirect := o.(types.IndirectRef)
		if !indirect || visited[ref] {
			return 0, refuse("source_damaged", "Page tree contains a direct, repeated or cyclic node.")
		}
		visited[ref] = true
		d, err := s.DereferenceDict(o)
		if err != nil || len(d) == 0 {
			return 0, refuse("source_damaged", fmt.Sprintf("Page-tree object %s is missing or empty.", ref))
		}
		typ, err := s.Dereference(d["Type"])
		if err != nil {
			return 0, err
		}
		switch typ {
		case types.Name("Page"):
			kids, err := s.Dereference(d["Kids"])
			if err != nil {
				return 0, err
			}
			if kids != nil {
				return 0, refuse("source_damaged", "A Page has non-null Kids; readers may drop its content.")
			}
			pages = append(pages, pageRef{ref: ref, dict: d})
			return 1, nil
		case types.Name("Pages"):
			kids, err := s.DereferenceArray(d["Kids"])
			if err != nil || len(kids) == 0 {
				return 0, refuse("source_damaged", "Pages requires nonempty Kids.")
			}
			count := 0
			for _, kid := range kids {
				n, err := walk(kid, depth+1)
				if err != nil {
					return 0, err
				}
				count += n
			}
			v, err := s.Dereference(d["Count"])
			if err != nil || v != types.Integer(count) {
				return 0, refuse("source_damaged", fmt.Sprintf("Page-tree Count disagrees with %d leaves.", count))
			}
			return count, nil
		default:
			return 0, refuse("source_damaged", fmt.Sprintf("Page-tree node %s has no supported Type.", ref))
		}
	}
	_, err = walk(catalog["Pages"], 0)
	return pages, err
}

// walkObjects visits direct containers in all materialized indirect objects once.
// Indirect links are handled by the outer xref loop, avoiding graph recursion.
func walkObjects(c context.Context, s *model.Context, visit func(types.Dict) error) error {
	var walk func(types.Object, int) error
	walk = func(o types.Object, depth int) error {
		if depth > 128 {
			return refuse("source_invalid", "Direct object nesting exceeds 128.")
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
				return refuse("source_invalid", "Unsupported literal-hash name identity; upstream may decode it twice.")
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
	var ds []diag.Diagnostic
	add := func(code diag.Code, message string) {
		for _, d := range ds {
			if d.Code == code {
				return
			}
		}
		ds = append(ds, diag.New(code, message))
	}

	for _, entry := range []struct {
		key  string
		code diag.Code
	}{{"StructTreeRoot", "source_structure_removed"}, {"PageLabels", "source_page_labels_removed"}, {"OutputIntents", "source_output_intents_removed"}, {"OpenAction", "source_document_actions_removed"}, {"AA", "source_document_actions_removed"}, {"AF", "source_attachments_removed"}} {
		if catalog[entry.key] != nil {
			add(entry.code, entry.key+" will be removed from the output catalog.")
		}
	}
	names, err := s.DereferenceDict(catalog["Names"])
	if err != nil {
		return facts, ds, err
	}
	if names["JavaScript"] != nil {
		add("source_document_actions_removed", "Document JavaScript will be removed.")
	}
	if names["EmbeddedFiles"] != nil {
		add("source_attachments_removed", "Embedded-file indexes will be removed.")
	}
	form, err := s.DereferenceDict(catalog["AcroForm"])
	if err != nil {
		return facts, ds, err
	}
	if form["XFA"] != nil {
		add("source_xfa_removed", "XFA will be removed; static fields remain.")
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
		add("form_appearance_regeneration_requested", "The source requests viewer-regenerated field appearances.")
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
				add("source_uri_base_removed", "Source URI Base is removed; retained relative URI actions may resolve differently.")
			}
		}
		return nil
	})
	if err != nil {
		return facts, ds, err
	}
	for _, page := range []int{1, s.PageCount} {
		size, e := displayedSize(c, s, page)
		if e != nil {
			return facts, ds, e
		}
		if page == 1 {
			facts.FirstSize = size
		}
		if page == s.PageCount {
			facts.LastSize = size
		}
	}
	destinations, err := readDestinations(c, s)
	if err != nil {
		return facts, ds, err
	}
	for name, d := range destinations {
		facts.DestNames = append(facts.DestNames, name)
		if d.page < 1 {
			add("destinations_dropped", "A destination outside its source document will be omitted.")
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
	facts.Selection = []packet.PageRange{{First: 1, Last: s.PageCount}}
	facts.OutlineDepth, err = outlineDepth(c, s)
	if err != nil {
		return facts, ds, err
	}
	slices.Sort(facts.DestNames)
	return facts, ds, nil
}

func displayedSize(c context.Context, s *model.Context, page int) (packet.Size, error) {
	d, _, attrs, err := s.PageDict(c, page, false)
	if err != nil {
		return packet.Size{}, err
	}
	box := *attrs.MediaBox
	if crop := attrs.CropBox; crop != nil {
		box.LL.X = math.Max(box.LL.X, crop.LL.X)
		box.LL.Y = math.Max(box.LL.Y, crop.LL.Y)
		box.UR.X = math.Min(box.UR.X, crop.UR.X)
		box.UR.Y = math.Min(box.UR.Y, crop.UR.Y)
	}
	unit := 1.0
	if v, err := s.Dereference(d["UserUnit"]); err != nil {
		return packet.Size{}, err
	} else if v != nil {
		switch n := v.(type) {
		case types.Integer:
			unit = float64(n)
		case types.Float:
			unit = float64(n)
		default:
			return packet.Size{}, refuse("source_invalid", "Invalid UserUnit.")
		}
	}
	width, height := box.Width()*unit, box.Height()*unit
	if attrs.Rotate%180 != 0 {
		width, height = height, width
	}
	if width <= 0 || height <= 0 || math.IsInf(width, 0) || math.IsInf(height, 0) || math.IsNaN(width) || math.IsNaN(height) {
		return packet.Size{}, refuse("source_invalid", "Invalid physical page dimensions.")
	}
	return packet.Size{Width: width, Height: height}, nil
}

func generation(e *model.XRefTableEntry) int {
	if e.Generation == nil {
		return 0
	}
	return *e.Generation
}
