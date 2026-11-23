// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/resoltico/pdfconcat/internal/core/diag"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type fieldRecord struct {
	site sourceSite
	name string
	dict types.Dict
}

func textOf(o types.Object) (string, error) {
	if o == nil {
		return "", nil
	}
	if name, ok := o.(types.Name); ok {
		return string(name), nil
	}
	text, err := types.StringOrHexLiteral(o)
	if err != nil {
		return "", err
	}
	if text == nil {
		return "", fmt.Errorf("expected a PDF text string, got %T", o)
	}
	return *text, nil
}

func textValue(s *model.Context, o types.Object) (string, error) {
	value, err := s.Dereference(o)
	if err != nil {
		return "", err
	}
	return textOf(value)
}
func nameValue(s *model.Context, o types.Object) (types.Name, error) {
	value, err := s.Dereference(o)
	if err != nil {
		return "", err
	}
	name, _ := value.(types.Name)
	return name, nil
}

type formFieldNode struct {
	dict   types.Dict
	ref    types.IndirectRef
	site   sourceSite
	member sourceSite
	kind   types.Name
	name   string
	widget bool
	kids   []*formFieldNode
}

func formFieldTree(c context.Context, s *model.Context, form types.Dict) ([]*formFieldNode, error) {
	seen := map[uintptr]bool{}
	origins, err := sourceOriginsFor(c, s)
	if err != nil {
		return nil, err
	}
	cat, err := s.Catalog()
	if err != nil {
		return nil, err
	}
	base := catalogSite(s).key("AcroForm").anchor(cat["AcroForm"], origins).key("Fields").anchor(form["Fields"], origins)
	var collect func(types.Object, types.Name, sourceSite, int) ([]*formFieldNode, error)
	collect = func(raw types.Object, inherited types.Name, site sourceSite, depth int) ([]*formFieldNode, error) {
		if raw == nil {
			return nil, nil
		}
		if depth > 64 {
			return nil, site.wrap(refuse(diag.SourceUnsupported, diag.FormFieldTreeExceedsTheSupportedDepthOf()))
		}
		list, err := s.DereferenceArray(raw)
		if err != nil {
			return nil, site.wrap(err)
		}
		var result []*formFieldNode
		for i, item := range list {
			if err := c.Err(); err != nil {
				return nil, err
			}
			at := site.index(i)
			ref, indirect := item.(types.IndirectRef)
			if indirect {
				if !liveReference(s, ref) {
					return nil, at.wrap(refuse(diag.SourceInvalid, diag.AFormFieldReferenceIsMissingFreeOrHasThe()))
				}
			}
			d, err := s.DereferenceDict(item)
			if err != nil || d == nil {
				return nil, at.wrap(refuse(diag.SourceInvalid, diag.FormFieldMemberRequiresDictionary()))
			}
			member := at
			at = at.anchor(item, origins)
			id := layerDictIdentity(d)
			if seen[id] {
				return nil, at.wrap(refuse(diag.SourceInvalid, diag.FormFieldTreeRepeatsANode()))
			}
			seen[id] = true
			kind, err := nameValue(s, d["FT"])
			if err != nil {
				return nil, at.wrap(err)
			}
			if kind == "" {
				kind = inherited
			}
			subtype, err := nameValue(s, d["Subtype"])
			if err != nil {
				return nil, at.wrap(err)
			}
			name, err := textValue(s, d["T"])
			if err != nil {
				return nil, at.key("T").wrap(err)
			}
			kids, err := collect(d["Kids"], kind, at.key("Kids").anchor(d["Kids"], origins), depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, &formFieldNode{dict: d, ref: ref, site: at, member: member, kind: kind, name: name, widget: subtype == "Widget", kids: kids})
		}
		return result, nil
	}
	return collect(form["Fields"], "", base, 0)
}

// Observe the original catalog edge before validation can drop an empty
// AcroForm. Admission, normalization and warnings share this observation.
func observeSourceXFA(c context.Context, s *model.Context, pages []pageRef, roots []*formFieldNode) (sourceSite, error) {
	cat, err := s.Catalog()
	if err != nil {
		return sourceSite{}, err
	}
	form, err := s.DereferenceDict(cat["AcroForm"])
	if err != nil {
		return sourceSite{}, err
	}
	raw := form["XFA"]
	if raw == nil {
		return sourceSite{}, nil
	}
	site := catalogSite(s).key("AcroForm").anchor(cat["AcroForm"], sourceOrigins{})
	xfaSite := site.key("XFA")
	if err := checkXFAReference(s, raw, xfaSite); err != nil {
		return sourceSite{}, err
	}
	xfa, err := s.Dereference(raw)
	if err != nil {
		return sourceSite{}, xfaSite.wrap(err)
	}
	if xfa == nil {
		// The optional slot is absent in PDF semantics. Mask only this
		// catalog edge so backend validation also sees absence.
		delete(form, "XFA")
		return sourceSite{}, nil
	}
	if entries, ok := xfa.(types.Array); ok {
		for i, entry := range entries {
			if err := c.Err(); err != nil {
				return sourceSite{}, err
			}
			if err := checkXFAReference(s, entry, xfaSite.index(i)); err != nil {
				return sourceSite{}, err
			}
		}
	}
	if ref, ok := form["Fields"].(types.IndirectRef); ok && !liveReference(s, ref) {
		return sourceSite{}, site.key("Fields").wrap(refuse(diag.SourceInvalid, diag.AFormFieldReferenceIsMissingFreeOrHasThe()))
	}
	usable, err := hasUsableStaticWidget(c, s, pages, roots)
	if err != nil {
		return sourceSite{}, err
	}
	if !usable {
		return sourceSite{}, xfaSite.wrap(refuse(diag.SourceDynamicForm, diag.XFAWithoutStaticFields()))
	}
	return xfaSite, nil
}

func checkXFAReference(s *model.Context, raw types.Object, site sourceSite) error {
	if ref, ok := raw.(types.IndirectRef); ok && !liveReference(s, ref) {
		return site.wrap(refuse(diag.SourceInvalid,
			diag.UndefinedXFAReference(int64(ref.ObjectNumber.Value()), int64(ref.GenerationNumber.Value()))))
	}
	return nil
}

func formSlotValue(s *model.Context, raw types.Object, site sourceSite) (types.Object, error) {
	if ref, ok := raw.(types.IndirectRef); ok && !liveReference(s, ref) {
		return nil, site.wrap(refuse(diag.SourceInvalid, diag.AFormFieldReferenceIsMissingFreeOrHasThe()))
	}
	value, err := s.Dereference(raw)
	return value, site.wrap(err)
}

func xfaFieldFlags(s *model.Context, raw types.Object, site sourceSite, inherited int) (int, error) {
	value, err := formSlotValue(s, raw, site)
	if err != nil || value == nil {
		return inherited, err
	}
	flags, ok := value.(types.Integer)
	if !ok {
		return 0, site.wrap(fmt.Errorf("form field or widget flags must be an integer, got %T", value))
	}
	return flags.Value(), nil
}

// Resolve absence first and compare the original reference when an owner
// exists. Equivalent dictionaries do not establish the same ownership.
func formOwnerMatches(s *model.Context, raw types.Object, expected types.IndirectRef, allowAbsent bool, site sourceSite) (bool, error) {
	owner, err := formSlotValue(s, raw, site)
	if err != nil {
		return false, err
	}
	if owner == nil {
		return allowAbsent, nil
	}
	return raw == expected, nil
}

func formParentMatches(s *model.Context, node *formFieldNode, parent types.IndirectRef) (bool, error) {
	return formOwnerMatches(s, node.dict["Parent"], parent, parent == (types.IndirectRef{}), node.site.key("Parent"))
}

// XFA can coexist with a static form when at least one ordinary data widget
// is usable on a page.
func hasUsableStaticWidget(c context.Context, s *model.Context, pages []pageRef, roots []*formFieldNode) (bool, error) {
	widgets := map[types.IndirectRef]*formFieldNode{}
	var collect func([]*formFieldNode, types.IndirectRef, int) error
	collect = func(nodes []*formFieldNode, parent types.IndirectRef, inheritedFlags int) error {
		for _, node := range nodes {
			if err := c.Err(); err != nil {
				return err
			}
			owned, err := formParentMatches(s, node, parent)
			if err != nil {
				return err
			}
			if !owned {
				continue
			}
			flags, err := xfaFieldFlags(s, node.dict["Ff"], node.site.key("Ff"), inheritedFlags)
			if err != nil {
				return err
			}
			if len(node.kids) > 0 {
				if node.ref == (types.IndirectRef{}) {
					continue
				}
				if err := collect(node.kids, node.ref, flags); err != nil {
					return err
				}
				continue
			}
			if !node.widget || node.ref == (types.IndirectRef{}) {
				continue
			}
			switch node.kind {
			case "Tx", "Ch":
			case "Btn":
				const pushbuttonFieldFlag = 1 << 16
				if flags&pushbuttonFieldFlag != 0 {
					continue
				}
			default:
				continue
			}
			widgets[node.ref] = node
		}
		return nil
	}
	if err := collect(roots, types.IndirectRef{}, 0); err != nil || len(widgets) == 0 {
		return false, err
	}

	geometry, err := geometriesForPages(c, s, pages)
	if err != nil {
		return false, err
	}
	memberships := map[types.IndirectRef]int{}
	visible := map[types.IndirectRef]bool{}
	for i, page := range pages {
		if err := c.Err(); err != nil {
			return false, err
		}
		annots, err := s.DereferenceArray(page.dict["Annots"])
		if err != nil {
			return false, err
		}
		for _, obj := range annots {
			if err := c.Err(); err != nil {
				return false, err
			}
			ref, _ := obj.(types.IndirectRef)
			node := widgets[ref]
			if node == nil {
				continue
			}
			memberships[ref]++
			owned, err := formOwnerMatches(s, node.dict["P"], page.ref, true, node.site.key("P"))
			if err != nil {
				return false, err
			}
			if !owned {
				continue
			}
			flags, err := xfaFieldFlags(s, node.dict["F"], node.site.key("F"), 0)
			if err != nil {
				return false, err
			}
			if model.AnnotationFlags(flags)&(model.AnnHidden|model.AnnNoView|model.AnnToggleNoView) != 0 {
				continue
			}
			rect, err := readRectangle(s, node.dict["Rect"])
			if err != nil {
				return false, node.site.key("Rect").wrap(err)
			}
			frame := geometry[i].box
			visible[ref] = max(rect.LL.X, frame.LL.X) < min(rect.UR.X, frame.UR.X) &&
				max(rect.LL.Y, frame.LL.Y) < min(rect.UR.Y, frame.UR.Y)
		}
	}
	for ref, shown := range visible {
		if shown && memberships[ref] == 1 {
			return true, nil
		}
	}
	return false, nil
}

func fields(c context.Context, s *model.Context) ([]fieldRecord, error) {
	cat, err := s.Catalog()
	if err != nil {
		return nil, err
	}
	form, err := s.DereferenceDict(cat["AcroForm"])
	if err != nil {
		return nil, err
	}
	roots, err := formFieldTree(c, s, form)
	if err != nil {
		return nil, err
	}
	var result []fieldRecord
	var visit func(*formFieldNode, string, bool)
	visit = func(node *formFieldNode, prefix string, root bool) {
		name := prefix
		if node.name != "" {
			if name != "" {
				name += "."
			}
			name += node.name
		}
		hasFields := false
		for _, kid := range node.kids {
			visit(kid, name, false)
			hasFields = hasFields || kid.name != ""
		}
		if (node.name != "" || root && node.kind != "") && !hasFields {
			result = append(result, fieldRecord{name: name, dict: node.dict, site: node.site.key("T")})
		}
	}
	for _, root := range roots {
		visit(root, "", true)
	}
	return result, nil
}

func qualifiedName(s *model.Context, d types.Dict) (string, error) {
	var names []string
	seen := map[types.IndirectRef]bool{}
	for depth := 0; ; depth++ {
		if depth > 64 {
			return "", errors.New("merged field parent chain exceeds depth 64")
		}
		name, err := textValue(s, d["T"])
		if err != nil {
			return "", err
		}
		if name != "" {
			names = append(names, name)
		}
		parent := d["Parent"]
		if parent == nil {
			break
		}
		if ref, ok := parent.(types.IndirectRef); ok {
			if seen[ref] {
				return "", errors.New("merged field parent cycle")
			}
			seen[ref] = true
		}
		d, err = s.DereferenceDict(parent)
		if err != nil {
			return "", err
		}
	}
	slices.Reverse(names)
	return strings.Join(names, "."), nil
}

// resourceIdentity compares encoded resource semantics across object tables.
// Memoized indirect subgraphs keep work proportional to the encoded graph,
// even when the same font data is referenced through many resource paths.
func resourceIdentity(c context.Context, s *model.Context, root types.Object) (string, error) {
	type digest = [sha256.Size]byte
	active := map[types.IndirectRef]bool{}
	memo := map[types.IndirectRef]digest{}
	var visit func(types.Object, int) (digest, error)
	visit = func(o types.Object, depth int) (digest, error) {
		if err := c.Err(); err != nil {
			return digest{}, err
		}
		if depth > 64 {
			return digest{}, refuse(diag.SourceFormUnsupported, diag.FormDefaultResourcesAreNestedTooDeeplyForCompatibilityComparison())
		}
		if ref, ok := o.(types.IndirectRef); ok {
			if active[ref] {
				return digest{}, refuse(diag.SourceFormUnsupported, diag.CyclicFormDefaultResourcesAreUnsupportedForCompatibilityComparison())
			}
			if sum, ok := memo[ref]; ok {
				return sum, nil
			}
			resolved, err := s.Dereference(ref)
			if err != nil {
				return digest{}, err
			}
			active[ref] = true
			sum, err := visit(resolved, depth+1)
			delete(active, ref)
			if err == nil {
				memo[ref] = sum
			}
			return sum, err
		}
		hash := sha256.New()
		child := func(value types.Object) error {
			sum, err := visit(value, depth+1)
			if err == nil {
				hash.Write(sum[:])
			}
			return err
		}
		switch v := o.(type) {
		case types.Dict:
			hash.Write([]byte{'d'})
			for _, key := range slices.Sorted(maps.Keys(v)) {
				hash.Write(binary.AppendUvarint(nil, uint64(len(key))))
				hash.Write([]byte(key))
				if err := child(v[key]); err != nil {
					return digest{}, err
				}
			}
		case types.Array:
			hash.Write([]byte{'a'})
			for _, value := range v {
				if err := child(value); err != nil {
					return digest{}, err
				}
			}
		case types.StreamDict:
			hash.Write([]byte{'s'})
			if err := child(v.Dict); err != nil {
				return digest{}, err
			}
			hash.Write(v.Raw)
		default:
			hash.Write(fmt.Appendf(nil, "v%T:%#v", o, o))
		}
		var sum digest
		copy(sum[:], hash.Sum(nil))
		return sum, nil
	}
	sum, err := visit(root, 0)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum[:]), nil
}
