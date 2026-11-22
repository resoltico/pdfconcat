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

type optionalContentPlan struct {
	wrappers       *pageWrappers
	removeCatalog  bool
	emptyCatalog   types.Object
	detachedRefs   map[types.IndirectRef]bool
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
		return nil, layerRefusal(site, diag.OptionalContentRefused())
	}
	v, err := s.Dereference(o)
	return v, site.wrap(err)
}

func optionalContentRefusal(err error) bool {
	var refusal *policyError
	return errors.As(err, &refusal) && refusal.finding.Code == diag.SourceOptionalContent
}

// planOptionalContent observes the original graph before validation or selection
// can remove evidence.
func planOptionalContent(c context.Context, s *model.Context, policy packet.OptionalContentPolicy, pages []pageRef, content pageContentSnapshot, nulls layerNullEvidence) (optionalContentPlan, error) {
	var plan optionalContentPlan
	cat, err := s.Catalog()
	if err != nil {
		return plan, err
	}
	site := catalogSite(s).key("OCProperties")
	o, err := layerValue(s, cat["OCProperties"], site)
	if err != nil {
		return plan, err
	}
	switch {
	case o != nil:
		if s.XRefTable.Version() < model.V15 {
			return plan, layerRefusal(site, diag.OptionalContentRefused())
		}
		if d, ok := o.(types.Dict); ok {
			groups, e := layerValue(s, d["OCGs"], site.key("OCGs"))
			if e != nil {
				return plan, e
			}
			if a, ok := groups.(types.Array); ok && len(a) == 0 {
				plan.emptyCatalog = cat["OCProperties"]
				plan.detachedRefs, err = emptyLayerCatalog(c, s, cat["OCProperties"], site, nulls)
				if err != nil {
					return plan, err
				}
			}
		}
		if plan.emptyCatalog == nil {
			plan, err = staticVisibleLayer(c, s, o, site)
			if err := c.Err(); err != nil {
				return plan, err
			}
			if err != nil {
				if policy != packet.OptionalContentFlattenVisibleContent && optionalContentRefusal(err) {
					return plan, layerRefusal(site, diag.OptionalContentRefused())
				}
				return plan, err
			}
		}
	case policy != packet.OptionalContentFlattenVisibleContent:
		return inspectCatalogFreeDefault(c, s, pages, content, nulls)
	default:
		wrappers, err := planPageWrappers(c, s, pages, content, nulls)
		if err != nil {
			return plan, err
		}
		if len(wrappers.pages) > 0 {
			plan.wrappers = wrappers
		}
	}
	registered := plan.group != (types.IndirectRef{})
	mode := layerVerify
	var evidence *layerProgramEvidence
	if registered && policy != packet.OptionalContentFlattenVisibleContent ||
		!registered && plan.emptyCatalog == nil && plan.wrappers == nil {
		mode = layerDetect
		evidence = newLayerProgramEvidence(s)
	}
	if err := scanLayerGraph(c, s, pages, &plan, mode, evidence); err != nil {
		return plan, err
	}
	if registered && len(plan.forms) == 0 {
		return plan, layerRefusal(site, diag.OptionalContentProfileUnproved())
	}
	if mode == layerDetect {
		if err := detectLayerPagePrograms(c, s, pages, content, evidence); err != nil {
			return plan, err
		}
		if registered {
			return plan, layerRefusal(site, diag.OptionalContentDefaultRefusal(string(packet.OptionalContentFlattenVisibleContent)))
		}
	} else if plan.wrappers == nil {
		if err := verifyLayerPrograms(c, s, pages, content); err != nil {
			return plan, err
		}
	}
	plan.removeCatalog = o == nil || plan.emptyCatalog != nil
	return plan, nil
}

func inspectCatalogFreeDefault(c context.Context, s *model.Context, pages []pageRef, content pageContentSnapshot, nulls layerNullEvidence) (optionalContentPlan, error) {
	plan := optionalContentPlan{removeCatalog: true}
	evidence := newLayerProgramEvidence(s)
	err := scanLayerGraph(c, s, pages, &plan, layerDetect, evidence)
	if err == nil {
		err = detectLayerPagePrograms(c, s, pages, content, evidence)
	}
	if !optionalContentRefusal(err) {
		return plan, err
	}
	// Ordinary inputs keep the detection path. A default-policy refusal
	// can name the opt-in only after the same full proof used by admission.
	wrappers, proof := planPageWrappers(c, s, pages, content, nulls)
	if proof == nil && len(wrappers.pages) > 0 {
		proof = scanLayerGraph(c, s, pages, &optionalContentPlan{wrappers: wrappers}, layerVerify, nil)
		if proof == nil {
			page := wrappers.pages[0]
			site := sourceSite{ref: page.content.ref, page: page.number}.key("Contents")
			return plan, layerRefusal(site, diag.OptionalWrapperDefaultRefusal(string(packet.OptionalContentFlattenVisibleContent)))
		}
	}
	if proof != nil {
		return plan, proof
	}
	return plan, err
}

func (p optionalContentPlan) applyForms(c context.Context, s *model.Context, pages []pageRef) (diag.Diagnostic, error) {
	cat, err := s.Catalog()
	if err != nil {
		return diag.Diagnostic{}, err
	}
	for _, ref := range p.forms {
		if err := c.Err(); err != nil {
			return diag.Diagnostic{}, err
		}
		if !liveReference(s, ref) {
			return diag.Diagnostic{}, layerRefusal(sourceSite{ref: ref}.key("OC"), diag.OptionalContentProfileUnproved())
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
	if err := scanLayerGraph(c, s, pages, &optionalContentPlan{}, layerGraphOnly, nil); err != nil {
		return diag.Diagnostic{}, err
	}
	site := sourceSite{ref: p.group}.key("Name")
	return sourceWarning(diag.SourceOptionalContentFlattened, diag.OptionalContentFlattened(), diag.OptionalContentGroupName(p.name), site), nil
}

type layerRole uint8

const (
	layerOther layerRole = iota
	layerResources
	layerPageResources
	layerXObjects
	layerXObject
	layerProperties
	layerPageProperties
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
func scanLayerGraph(c context.Context, s *model.Context, pages []pageRef, plan *optionalContentPlan, mode layerProgramMode, evidence *layerProgramEvidence) error {
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
	if plan.emptyCatalog != nil || plan.wrappers != nil {
		for _, key := range slices.Sorted(maps.Keys(s.OriginalTrailer)) {
			if key != "Root" {
				pending = append(pending, layerEdge{object: s.OriginalTrailer[key], site: sourceSite{path: "Trailer"}.key(key)})
			}
		}
	}
	return scanLayerEdges(c, s, origins, pending, plan, mode, false, evidence)
}

func scanLayerEdges(c context.Context, s *model.Context, origins sourceOrigins, pending []layerEdge, plan *optionalContentPlan, mode layerProgramMode, stage bool, evidence *layerProgramEvidence) error {
	proving := plan.emptyCatalog != nil || plan.group != (types.IndirectRef{}) || plan.wrappers != nil
	seen := map[layerVisit]bool{}
	forms := map[types.IndirectRef]bool{}
	var structuredForms []types.IndirectRef
	programs := layerPrograms{remaining: maxLayerProgramBytes, pdf20: s.PDF20()}
	for len(pending) > 0 {
		if err := c.Err(); err != nil {
			return err
		}
		work := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		value, site := work.object, work.site
		ref, indirect := value.(types.IndirectRef)
		if indirect {
			if plan.wrappers != nil && plan.wrappers.streams[ref] && work.role != layerPageContent {
				return layerRefusal(site, diag.OptionalContentRefused())
			}
			if plan.detachedRefs[ref] {
				return layerRefusal(site, diag.OptionalContentRefused())
			}
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
		if plan.emptyCatalog != nil || plan.wrappers != nil {
			// Include dictionaries of reachable technical streams in the
			// inbound-edge audit. Their bytes are reader bookkeeping.
			switch stream := value.(type) {
			case types.ObjectStreamDict:
				value = stream.Dict
			case types.XRefStreamDict:
				value = stream.Dict
			}
		}
		switch item := value.(type) {
		case types.IndirectRef:
			pending = append(pending, layerEdge{item, site, work.role})
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
			var removed map[string]wrapperBinding
			if plan.wrappers != nil {
				id := layerDictIdentity(d)
				removed = plan.wrappers.properties[id]
				if len(removed) != 0 && work.role != layerPageProperties ||
					plan.wrappers.resources[id] && work.role != layerPageResources {
					return layerRefusal(site, diag.OptionalContentRefused())
				}
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
			if kind == "OCG" || kind == "OCMD" || (work.role == layerProperty || (plan.emptyCatalog != nil || plan.wrappers != nil) && work.role == layerOther) && visibilityMembershipShape(d) {
				return layerRefusal(site.key("Type"), diag.OptionalContentRefused())
			}
			if action == "SetOCGState" && (plan.emptyCatalog != nil || plan.wrappers != nil || work.role == layerAction || d["State"] != nil || kind == "Action") {
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

			if mode != layerGraphOnly {
				strictSource := proving && mode == layerVerify
				if strictSource && stream && d["F"] != nil {
					return layerRefusal(site.key("Stream"), diag.OptionalContentProgramUnproved())
				}
				if strictSource && (opaqueLayerAction(action) || work.role == layerNames && d["JavaScript"] != nil ||
					work.role == layerAcroForm && d["XFA"] != nil || kind == "Filespec" || kind == "EmbeddedFile" || subtype == "RichMedia" ||
					subtype == "3D" || subtype == "Movie" || subtype == "Sound" ||
					d["Ref"] != nil && subtype == "Form" || d["OPI"] != nil) {
					return layerRefusal(site, diag.OptionalContentProfileUnproved())
				}
				if stream {
					budget := &programs
					var verified map[types.IndirectRef]bool
					if evidence != nil {
						independent := evidence.program(s, mode)
						budget = &independent
						verified = evidence.streams
					}
					if err := budget.stream(c, s, dict, work.role, kind, subtype, site, plan.wrappers, mode, stage, ref, verified); err != nil {
						return err
					}
				}
			}
			for _, key := range slices.Backward(slices.Sorted(maps.Keys(d))) {
				if _, edited := removed[key]; edited ||
					proving && ((ref == *s.Root && key == "OCProperties") || boundOC && key == "OC") ||
					stage && work.role == layerAnnotation && key != "AP" {
					continue
				}
				if layerContainer(d[key]) {
					role := layerChildRole(work.role, kind, subtype, key, stream, ref == *s.Root)
					if plan.wrappers != nil {
						pageOwner := plan.wrappers.pageOwners[layerDictIdentity(d)]
						if key == "Resources" && pageOwner {
							role = layerPageResources
						}
						if role == layerPageContent && !pageOwner {
							role = layerOther
						}
					}
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
	case layerResources, layerPageResources:
		switch key {
		case "XObject":
			return layerXObjects
		case "Properties":
			if role == layerPageResources {
				return layerPageProperties
			}
			return layerProperties
		}
	case layerXObjects:
		return layerXObject
	case layerProperties, layerPageProperties:
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
