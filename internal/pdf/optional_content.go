// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

type visibleLayer struct {
	group          types.IndirectRef
	name           string
	forms          []types.IndirectRef
	nullVisibility []types.Dict
}

func layerRefusal(site sourceSite, message diag.Explanation) error {
	return site.wrap(refuse(diag.SourceOptionalContent, message))
}

// A live reference can resolve to PDF null. Check its identity before
// dereferencing so a missing, free or wrong-generation entry is still refused.
func layerValue(s *model.Context, o types.Object, site sourceSite) (types.Object, error) {
	if ref, ok := o.(types.IndirectRef); ok && !liveReference(s, ref) {
		return nil, layerRefusal(site, diag.OptionalContentProfileUnproved())
	}
	v, err := s.Dereference(o)
	return v, site.wrap(err)
}

// planOptionalContent observes the original graph before validation or selection
// can remove evidence. Only the explicitly admitted profile produces edits.
func planOptionalContent(c context.Context, s *model.Context, policy packet.OptionalContentPolicy, pages []pageRef, content pageContentSnapshot) (visibleLayer, error) {
	var plan visibleLayer
	cat, err := s.Catalog()
	if err != nil {
		return plan, err
	}
	site := catalogSite(s).key("OCProperties")
	o, err := layerValue(s, cat["OCProperties"], site)
	if err != nil {
		return plan, err
	}
	if o != nil {
		if s.XRefTable.Version() < model.V15 {
			return plan, layerRefusal(site, diag.OptionalContentRefused())
		}
		plan, err = staticVisibleLayer(c, s, o, site)
		if err := c.Err(); err != nil {
			return plan, err
		}
		if err != nil {
			var refusal *policyError
			if policy != packet.OptionalContentFlattenVisibleForms &&
				errors.As(err, &refusal) && refusal.finding.Code == diag.SourceOptionalContent {
				return plan, layerRefusal(site, diag.OptionalContentRefused())
			}
			return plan, err
		}
		if policy != packet.OptionalContentFlattenVisibleForms {
			return plan, layerRefusal(site, diag.OptionalContentDefaultRefusal(string(packet.OptionalContentFlattenVisibleForms)))
		}
	}
	if err := scanLayerGraph(c, s, pages, &plan); err != nil {
		return plan, err
	}
	if plan.group != (types.IndirectRef{}) {
		if len(plan.forms) == 0 {
			return plan, layerRefusal(catalogSite(s).key("OCProperties"), diag.OptionalContentProfileUnproved())
		}
		if err := verifyLayerPrograms(c, s, content); err != nil {
			return plan, err
		}
	}
	// Drop semantically absent edges after admission so the writer does not
	// carry live-null targets forward as dangling references.
	if o == nil {
		delete(cat, "OCProperties")
	}
	for _, owner := range plan.nullVisibility {
		delete(owner, "OC")
	}
	plan.nullVisibility = nil
	return plan, nil
}

func (p visibleLayer) apply(c context.Context, s *model.Context, pages []pageRef) (diag.Diagnostic, error) {
	cat, err := s.Catalog()
	if err != nil {
		return diag.Diagnostic{}, err
	}
	for _, ref := range p.forms {
		if err := c.Err(); err != nil {
			return diag.Diagnostic{}, err
		}
		value, e := s.Dereference(ref)
		if e != nil {
			return diag.Diagnostic{}, e
		}
		form, ok := value.(types.StreamDict)
		group, bound := form.Dict["OC"].(types.IndirectRef)
		if !ok || !bound || group != p.group {
			return diag.Diagnostic{}, layerRefusal(sourceSite{ref: ref}.key("OC"), diag.OptionalContentProfileUnproved())
		}
		delete(form.Dict, "OC")
	}
	delete(cat, "OCProperties")
	if err := scanLayerGraph(c, s, pages, &visibleLayer{}); err != nil {
		return diag.Diagnostic{}, err
	}
	site := sourceSite{ref: p.group}.key("Name")
	return sourceWarning(diag.SourceOptionalContentFlattened, diag.OptionalContentFlattened(), diag.OptionalContentGroupName(p.name), site), nil
}

type layerRole uint8

const (
	layerOther layerRole = iota
	layerResources
	layerXObjects
	layerXObject
	layerProperties
	layerProperty
	layerAlternates
	layerAlternate
	layerPageContent
	layerAppearance
	layerGlyphs
	layerGlyph
	layerPassive
	layerStructureStream
	layerAnnotation
	layerAction
	layerActions
	layerNames
	layerAcroForm
	layerOutline
)

type layerEdge struct {
	object types.Object
	site   sourceSite
	role   layerRole
}

type layerVisit struct {
	ref  types.IndirectRef
	role layerRole
}

// scanLayerGraph uses effective roots and complete indirect identities. Role
// edges are retained across indirect containers and arrays. The profile's
// catalog subtree has already been proved and is excluded from this walk.
func scanLayerGraph(c context.Context, s *model.Context, pages []pageRef, plan *visibleLayer) error {
	proving := plan.group != (types.IndirectRef{})
	origins := sourceOrigins{pages: make(map[types.IndirectRef]int, len(pages))}
	for i, p := range pages {
		origins.pages[p.ref] = i + 1
	}
	pending := []layerEdge{{object: *s.Root, site: catalogSite(s)}, {object: s.ID}}
	if s.Info != nil {
		pending = append(pending, layerEdge{object: *s.Info})
	} else if s.DirectInfoDict != nil {
		pending = append(pending, layerEdge{object: s.DirectInfoDict, site: sourceSite{path: "Trailer.Info"}})
	}
	seen := map[layerVisit]bool{}
	forms := map[types.IndirectRef]bool{}
	var structuredForms []types.IndirectRef
	programs := layerPrograms{remaining: maxLayerProgramBytes}
	for len(pending) > 0 {
		if err := c.Err(); err != nil {
			return err
		}
		work := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		value, site := work.object, work.site
		ref, indirect := value.(types.IndirectRef)
		if indirect {
			if !liveReference(s, ref) {
				if proving || work.role == layerProperty || work.role == layerAlternate || work.role == layerXObject {
					return layerRefusal(site, diag.OptionalContentProfileUnproved())
				}
				continue
			}
			visit := layerVisit{ref, work.role}
			if seen[visit] {
				continue
			}
			seen[visit] = true
			oldPath := site.path
			site = site.anchor(ref, origins)
			if ref == *s.Root {
				site.path = oldPath
			}
			var err error
			value, err = s.Dereference(ref)
			if err != nil {
				return err
			}
		}
		switch item := value.(type) {
		case types.Array:
			role := work.role
			if role == layerAlternates {
				role = layerAlternate
			}
			icc := len(item) == 2 && item[0] == types.Name("ICCBased")
			for i := len(item) - 1; i >= 0; i-- {
				childRole := role
				if icc && i == 1 {
					childRole = layerPassive
				}
				if layerContainer(item[i]) {
					pending = append(pending, layerEdge{item[i], site.index(i), childRole})
				}
			}
		case types.StreamDict, types.Dict:
			dict, stream := value.(types.StreamDict)
			d := dict.Dict
			if !stream {
				d, _ = item.(types.Dict)
			}
			kind, err := nameValue(s, d["Type"])
			if err != nil {
				return err
			}
			subtype, err := nameValue(s, d["Subtype"])
			if err != nil {
				return err
			}
			action, err := nameValue(s, d["S"])
			if err != nil {
				return err
			}
			if kind == "OCG" || kind == "OCMD" || work.role == layerProperty && (d["OCGs"] != nil || d["VE"] != nil) {
				return layerRefusal(site.key("Type"), diag.OptionalContentRefused())
			}
			if action == "SetOCGState" && (work.role == layerAction || d["State"] != nil || kind == "Action") {
				return layerRefusal(site.key("S"), diag.OptionalContentRefused())
			}
			boundOC := false
			if raw := d["OC"]; raw != nil && (work.role == layerAlternate ||
				stream && (subtype == "Image" || subtype == "Form") ||
				kind == "Annot" || work.role == layerAnnotation || subtype == "Widget") {
				oc, err := layerValue(s, raw, site.key("OC"))
				if err != nil {
					return err
				}
				if oc == nil {
					plan.nullVisibility = append(plan.nullVisibility, d)
				} else {
					group, bound := raw.(types.IndirectRef)
					if !proving || !stream || !indirect || !bound || group != plan.group ||
						subtype != "Form" || (kind != "XObject" && d["Type"] != nil) ||
						work.role != layerXObject && work.role != layerStructureStream {
						return layerRefusal(site.key("OC"), diag.OptionalContentRefused())
					}
					boundOC = true
					if work.role == layerStructureStream {
						structuredForms = append(structuredForms, ref)
					}
					if work.role == layerXObject && !forms[ref] {
						forms[ref] = true
						plan.forms = append(plan.forms, ref)
					}
				}
			}

			if proving {
				if opaqueLayerAction(action) || work.role == layerNames && d["JavaScript"] != nil ||
					work.role == layerAcroForm && d["XFA"] != nil || kind == "Filespec" || kind == "EmbeddedFile" || subtype == "RichMedia" ||
					subtype == "3D" || subtype == "Movie" || subtype == "Sound" ||
					d["Ref"] != nil && subtype == "Form" || d["OPI"] != nil {
					return layerRefusal(site, diag.OptionalContentProfileUnproved())
				}
				if stream {
					if err := programs.stream(c, s, dict, work.role, kind, subtype, site); err != nil {
						return err
					}
				}
			}
			for _, key := range slices.Backward(slices.Sorted(maps.Keys(d))) {
				if proving && ((ref == *s.Root && key == "OCProperties") || boundOC && key == "OC") {
					continue
				}
				if layerContainer(d[key]) {
					role := layerChildRole(work.role, kind, subtype, key, stream, ref == *s.Root)
					pending = append(pending, layerEdge{d[key], site.key(key), role})
				}
			}
		}
	}
	// A structure reference can accompany an XObject resource binding, but
	// cannot itself prove that a layered Form has an ordinary drawing owner.
	for _, ref := range structuredForms {
		if !forms[ref] {
			return layerRefusal(sourceSite{ref: ref}.key("OC"), diag.OptionalContentProfileUnproved())
		}
	}
	return nil
}

func opaqueLayerAction(action types.Name) bool {
	switch action {
	case "JavaScript", "Rendition", "RichMediaExecute", "Movie", "Sound", "Launch", "GoTo3DView":
		return true
	default:
		return false
	}
}

func layerContainer(object types.Object) bool {
	switch object.(type) {
	case types.IndirectRef, types.Array, types.Dict, types.StreamDict:
		return true
	default:
		return false
	}
}

func layerChildRole(role layerRole, kind, subtype types.Name, key string, stream, root bool) layerRole {
	if root {
		switch key {
		case "Names":
			return layerNames
		case "AcroForm":
			return layerAcroForm
		case "OpenAction":
			return layerAction
		case "Outlines":
			return layerOutline
		}
	}
	switch role {
	case layerResources:
		switch key {
		case "XObject":
			return layerXObjects
		case "Properties":
			return layerProperties
		}
	case layerXObjects:
		return layerXObject
	case layerProperties:
		return layerProperty
	case layerAppearance:
		return layerAppearance
	case layerGlyphs:
		return layerGlyph
	case layerActions:
		return layerAction
	case layerAction:
		if key == "Next" {
			return layerAction
		}
	case layerAnnotation:
		switch key {
		case "Popup", "IRT":
			return layerAnnotation
		case "Parent":
			if subtype == "Popup" {
				return layerAnnotation
			}
		}
	case layerOutline:
		if key == "First" || key == "Next" {
			return layerOutline
		}
	case layerAcroForm:
		if key == "DR" {
			return layerResources
		}
	case layerOther, layerXObject, layerProperty, layerAlternates, layerAlternate,
		layerPageContent, layerGlyph, layerPassive, layerStructureStream, layerNames:
	}
	switch key {
	case "Resources":
		if kind == "Page" || kind == "Pages" || subtype == "Type3" || stream {
			return layerResources
		}
	case "Annots":
		if kind == "Page" {
			return layerAnnotation
		}
	case "A":
		if kind == "Annot" || role == layerAnnotation || role == layerOutline || subtype == "Widget" {
			return layerAction
		}
	case "AA":
		return layerActions
	case "Alternates":
		if subtype == "Image" {
			return layerAlternates
		}
	case "Contents":
		if kind == "Page" {
			return layerPageContent
		}
	case "AP":
		return layerAppearance
	case "CharProcs":
		if subtype == "Type3" {
			return layerGlyphs
		}
	case "Stm":
		if kind == "MCR" {
			return layerStructureStream
		}
	case "Metadata", "FontFile", "FontFile2", "FontFile3", "ToUnicode", "CIDToGIDMap", "CIDSet", "DestOutputProfile", "JBIG2Globals":
		return layerPassive
	}
	return layerOther
}
