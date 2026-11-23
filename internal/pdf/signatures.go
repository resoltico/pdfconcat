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

// Keep the original dictionary alive until its ownership has been checked,
// including objects detached by an independently selected structure policy.
type signatureObject struct {
	dict     types.Dict
	location diag.PDFLocation
}

type signatureEvidence struct {
	dicts  map[uintptr]signatureObject
	first  *diag.PDFLocation
	reason diag.Explanation
}

func (e *signatureEvidence) note(loc diag.PDFLocation, reason diag.Explanation) {
	if e.first == nil {
		e.first, e.reason = &loc, reason
	}
}

// Observe even effective objects outside the catalog graph. In particular,
// detaching discarded structure must never suppress a signature finding.
func scanSignatures(c context.Context, s *model.Context) (signatureEvidence, error) {
	e := signatureEvidence{dicts: map[uintptr]signatureObject{}}
	err := objectLocations(c, s, false, func(d types.Dict, loc diag.PDFLocation) error {
		var signatureValue, signatureField, populated bool
		for _, key := range slices.Sorted(maps.Keys(d)) {
			decoded, err := types.DecodeName(key)
			if err != nil {
				decoded = key
			}
			switch decoded {
			case "Type", "FT", "V":
				o, err := s.Dereference(d[key])
				if err != nil {
					return err
				}
				if decoded == "V" {
					populated = populated || o != nil
					continue
				}
				n, _ := o.(types.Name)
				name, _ := types.DecodeName(string(n))
				if decoded == "Type" {
					signatureValue = signatureValue || name == "Sig" || name == "DocTimeStamp"
				} else {
					signatureField = signatureField || name == "Sig"
				}
			}
		}
		// /Type is optional for a signature value. Observe the value's
		// characteristic data entries even outside an ordinary field slot.
		untypedValue := d["ByteRange"] != nil && d["Contents"] != nil && (d["Filter"] != nil || d["SubFilter"] != nil)
		if signatureValue || untypedValue || signatureField && populated {
			e.dicts[layerDictIdentity(d)] = signatureObject{d, loc}
			e.note(loc, diag.TheSourceDeclaresASignatureOrTimestampWhoseAssuranceComposition())
		}
		return nil
	})
	if err != nil {
		return e, err
	}
	cat, err := s.Catalog()
	if err != nil {
		return e, err
	}
	for _, entry := range []struct {
		key    string
		reason diag.Explanation
	}{
		{"Perms", diag.CatalogPermissionStructuresRefused()},
		{"DocTimeStamp", diag.DocumentTimestampsRefused()},
	} {
		if value, err := s.Dereference(cat[entry.key]); err != nil {
			return e, err
		} else if value != nil {
			loc := *catalogSite(s).key(entry.key).location()
			e.note(loc, entry.reason)
		}
	}
	return e, nil
}

func observeSignatureFields(s *model.Context, roots []*formFieldNode, evidence *signatureEvidence) error {
	for _, node := range roots {
		if node.kind == "Sig" {
			value, err := s.Dereference(node.dict["V"])
			if err != nil {
				return node.site.wrap(err)
			}
			if value != nil {
				loc := *node.site.location()
				evidence.dicts[layerDictIdentity(node.dict)] = signatureObject{node.dict, loc}
				evidence.note(loc, diag.APopulatedSignatureFieldIsUnsupportedBecauseCompositionCannotPreserve())
			}
		}
		if err := observeSignatureFields(s, node.kids, evidence); err != nil {
			return err
		}
	}
	return nil
}

func originalSignatureEvidence(c context.Context, s *model.Context) (signatureEvidence, []*formFieldNode, error) {
	e, err := scanSignatures(c, s)
	if err != nil {
		return e, nil, err
	}
	cat, err := s.Catalog()
	if err != nil {
		return e, nil, err
	}
	form, err := s.DereferenceDict(cat["AcroForm"])
	if err != nil {
		return e, nil, err
	}
	roots, err := formFieldTree(c, s, form)
	if err != nil {
		return e, nil, err
	}
	return e, roots, observeSignatureFields(s, roots, &e)
}

func (e signatureEvidence) refusal(source bool) error {
	if e.first == nil {
		return nil
	}
	finding := diag.Report(diag.SourceSigned, e.reason)
	if source {
		diag.AttachEvidence(&finding, diag.SourceSignatureDiscardGuidance(packet.SignaturePolicyMember, string(packet.SignaturesDiscard)))
	}
	return structureAt(&policyError{finding: finding}, *e.first)
}

func signatureUnsupported(site sourceSite, reason diag.SignatureDiscardReason) error {
	return site.wrap(refuse(diag.SourceSignatureDiscardUnsupported, diag.SignatureDiscardUnsupported(reason)))
}

type signatureOwnerEdge struct {
	ref  types.IndirectRef
	path string
}

func signatureEdge(site sourceSite) signatureOwnerEdge {
	return signatureOwnerEdge{site.ref, site.path}
}

type signatureDiscard struct {
	evidence signatureEvidence
	// Only these proved owners can disappear. Any surviving reference to
	// one of them stops admission before unreachable objects are freed.
	removed       map[uintptr]bool
	exclusive     map[types.IndirectRef]bool
	changedFields map[types.IndirectRef]map[signatureOwnerEdge]bool
	widgets       map[types.IndirectRef]*formFieldNode
	owners        []referenceRoot
	fieldNames    map[string]bool
	edits         []func()
	artwork       []signatureArtwork
	first         *diag.PDFLocation
	counts        diag.SignatureDiscardCounts
	seenValues    map[uintptr]bool
}

func (p *signatureDiscard) note(site sourceSite) {
	if p.first == nil {
		loc := site.location()
		p.first = loc
	}
}

func (p *signatureDiscard) removeNode(s *model.Context, node *formFieldNode) error {
	owned := maps.Clone(node.dict)
	delete(owned, "Parent")
	delete(owned, "P")
	if kids, ok := owned["Kids"].(types.IndirectRef); ok && !liveReference(s, kids) {
		return signatureUnsupported(node.site.key("Kids"), diag.SignatureOwnership)
	}
	delete(owned, "Kids")
	p.owners = append(p.owners, referenceRoot{owned, node.site})
	if node.name != "" {
		name, err := qualifiedName(s, node.dict)
		if err != nil {
			return node.site.wrap(err)
		}
		p.fieldNames[name] = true
		for i, r := range name {
			if r == '.' {
				p.fieldNames[name[:i]] = true
			}
		}
	}
	p.removed[layerDictIdentity(node.dict)] = true
	if node.ref != (types.IndirectRef{}) {
		p.exclusive[node.ref] = true
	}
	if node.widget {
		p.widgets[node.ref] = node
	}
	return nil
}

func (p *signatureDiscard) value(c context.Context, s *model.Context, o types.Object, site sourceSite) error {
	d, err := s.DereferenceDict(o)
	if err != nil || d == nil {
		return signatureUnsupported(site, diag.SignatureOwnership)
	}
	kind, err := nameValue(s, d["Type"])
	if err != nil || (d["Type"] != nil && kind != "Sig" && kind != "DocTimeStamp") {
		return signatureUnsupported(site, diag.SignatureOwnership)
	}
	id := layerDictIdentity(d)
	p.removed[id] = true
	if ref, ok := o.(types.IndirectRef); ok {
		p.exclusive[ref] = true
	}
	if p.seenValues[id] {
		return nil
	}
	p.seenValues[id] = true
	if err := p.opaqueSignatureEdges(c, s, d, site, 0); err != nil {
		return err
	}
	subFilter, err := nameValue(s, d["SubFilter"])
	if err != nil {
		return site.wrap(err)
	}
	if kind == "DocTimeStamp" || subFilter == "ETSI.RFC3161" {
		p.counts.DocumentTimestamps++
	} else {
		p.counts.SignatureValues++
	}
	p.note(site)
	return nil
}

// Signature dictionaries may point back to the signed document via Data in
// transform references. Those edges do not confer ownership of the document.
func (p *signatureDiscard) opaqueSignatureEdges(c context.Context, s *model.Context, d types.Dict, site sourceSite, depth int) error {
	for _, key := range slices.Sorted(maps.Keys(d)) {
		if key == "Data" && d[key] == *s.Root {
			continue
		}
		if err := p.ownMetadata(c, s, d[key], site.key(key), depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (p *signatureDiscard) ownMetadata(c context.Context, s *model.Context, o types.Object, site sourceSite, depth int) error {
	if err := c.Err(); err != nil {
		return err
	}
	if depth > 64 {
		return signatureUnsupported(site, diag.SignatureOwnership)
	}
	if ref, ok := o.(types.IndirectRef); ok {
		if !liveReference(s, ref) || ref == *s.Root {
			return signatureUnsupported(site, diag.SignatureOwnership)
		}
		if p.exclusive[ref] {
			return nil
		}
		var err error
		o, err = s.Dereference(ref)
		if err != nil {
			return site.wrap(err)
		}
		if o == nil {
			return nil
		}
		p.exclusive[ref] = true
	}
	switch v := o.(type) {
	case types.Dict:
		return p.opaqueSignatureEdges(c, s, v, site, depth)
	case types.StreamDict:
		return p.opaqueSignatureEdges(c, s, v.Dict, site, depth)
	case types.Array:
		for _, item := range v {
			if err := p.ownMetadata(c, s, item, site, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *signatureDiscard) pruneFields(c context.Context, s *model.Context, nodes []*formFieldNode, parent types.IndirectRef) (types.Array, bool, error) {
	result := make(types.Array, 0, len(nodes))
	changed := false
	for _, node := range nodes {
		if err := c.Err(); err != nil {
			return nil, false, err
		}
		_, affected := p.evidence.dicts[layerDictIdentity(node.dict)]
		if affected && node.kind == "Sig" {
			if err := p.removeField(c, s, node, parent); err != nil {
				return nil, false, err
			}
			changed = true
			continue
		}
		kids, reduced, err := p.pruneFields(c, s, node.kids, node.ref)
		if err != nil {
			return nil, false, err
		}
		if reduced {
			// A changed branch must agree with the original field topology.
			owned, err := formParentMatches(s, node, parent)
			if err != nil || !owned || node.widget {
				return nil, false, signatureUnsupported(node.site, diag.SignatureOwnership)
			}
			if len(kids) == 0 {
				if err := requireAbsentSignatureSlots(s, node.dict, node.site, diag.SignatureOwnership, "V", "DV", "AA", "A"); err != nil {
					return nil, false, err
				}
				if err := p.removeNode(s, node); err != nil {
					return nil, false, err
				}
				changed = true
				continue
			}
			if node.ref != (types.IndirectRef{}) {
				allowed := map[signatureOwnerEdge]bool{signatureEdge(node.member): true}
				for _, kid := range node.kids {
					allowed[signatureEdge(kid.site.key("Parent"))] = true
				}
				p.changedFields[node.ref] = allowed
			}
			p.edits = append(p.edits, func() { node.dict["Kids"] = kids })
		}
		if node.ref == (types.IndirectRef{}) {
			result = append(result, node.dict)
		} else {
			result = append(result, node.ref)
		}
	}
	return result, changed, nil
}

func (p *signatureDiscard) removeField(c context.Context, s *model.Context, node *formFieldNode, parent types.IndirectRef) error {
	owned, err := formParentMatches(s, node, parent)
	if err != nil || !owned || node.widget && (len(node.kids) != 0 || node.ref == (types.IndirectRef{})) {
		return signatureUnsupported(node.site, diag.SignatureOwnership)
	}
	if err := requireAbsentSignatureSlots(s, node.dict, node.site, diag.SignatureVisibility, "AA", "A"); err != nil {
		return err
	}
	for _, key := range []string{"Lock", "SV"} {
		if node.dict[key] != nil {
			if err := p.ownMetadata(c, s, node.dict[key], node.site.key(key), 0); err != nil {
				return err
			}
		}
	}
	if err := p.value(c, s, node.dict["V"], node.site.key("V")); err != nil {
		return err
	}
	for _, kid := range node.kids {
		owned, err := formParentMatches(s, kid, node.ref)
		if err != nil || !owned || !kid.widget || kid.ref == (types.IndirectRef{}) || kid.kind != "Sig" || len(kid.kids) != 0 ||
			kid.dict["T"] != nil || kid.dict["V"] != nil {
			return signatureUnsupported(kid.site, diag.SignatureOwnership)
		}
		if err := p.removeNode(s, kid); err != nil {
			return err
		}
	}
	if err := p.removeNode(s, node); err != nil {
		return err
	}
	p.counts.PopulatedSignatureFields++
	p.note(node.site)
	return nil
}

// An optional slot can be absent, or point to a live null. Undefined
// references are kept distinct from null by the original reference checks.
func signatureSlot(s *model.Context, raw types.Object, site sourceSite, reason diag.SignatureDiscardReason) (types.Object, error) {
	if ref, ok := raw.(types.IndirectRef); ok && !liveReference(s, ref) {
		return nil, signatureUnsupported(site, reason)
	}
	value, err := s.Dereference(raw)
	return value, site.wrap(err)
}

func requireAbsentSignatureSlots(s *model.Context, d types.Dict, site sourceSite, reason diag.SignatureDiscardReason, keys ...string) error {
	for _, key := range keys {
		at := site.key(key)
		value, err := signatureSlot(s, d[key], at, reason)
		if err != nil {
			return err
		}
		if value != nil {
			return signatureUnsupported(at, reason)
		}
	}
	return nil
}

func (p *signatureDiscard) planCatalog(c context.Context, s *model.Context, cat types.Dict) error {
	for _, key := range []string{"Perms", "DocTimeStamp", "DSS", "VRI"} {
		raw := cat[key]
		if raw == nil {
			continue
		}
		site := catalogSite(s).key(key)
		value, err := signatureSlot(s, raw, site, diag.SignaturePermissions)
		if err != nil {
			return err
		}
		if value == nil {
			continue
		}
		d, ok := value.(types.Dict)
		if !ok {
			return signatureUnsupported(site, diag.SignaturePermissions)
		}
		switch key {
		case "DocTimeStamp":
			if err := p.value(c, s, raw, site); err != nil {
				return err
			}
		case "Perms":
			for _, name := range slices.Sorted(maps.Keys(d)) {
				switch name {
				case "DocMDP", "UR", "UR3":
					at := site.key(name)
					value, err := signatureSlot(s, d[name], at, diag.SignaturePermissions)
					if err != nil {
						return err
					}
					if value == nil {
						continue
					}
					if err := p.value(c, s, d[name], at); err != nil {
						return err
					}
				default:
					return signatureUnsupported(site.key(name), diag.SignaturePermissions)
				}
			}
			p.removed[layerDictIdentity(d)] = true
			if ref, ok := raw.(types.IndirectRef); ok {
				p.exclusive[ref] = true
			}
			p.counts.PermissionStructures++
		case "DSS", "VRI":
			if err := checkSignatureValidationStore(c, s, key, d, site); err != nil {
				return err
			}
			if err := p.ownMetadata(c, s, raw, site, 0); err != nil {
				return err
			}
			p.removed[layerDictIdentity(d)] = true
			p.counts.ValidationStores++
		}
		p.note(site)
		p.edits = append(p.edits, func() { delete(cat, key) })
	}
	return nil
}

func (p *signatureDiscard) planForm(c context.Context, s *model.Context, cat types.Dict, roots []*formFieldNode, xfa sourceSite) error {
	form, err := s.DereferenceDict(cat["AcroForm"])
	if err != nil || form == nil {
		return err
	}
	fields, reduced, err := p.pruneFields(c, s, roots, types.IndirectRef{})
	if err != nil {
		return err
	}
	flagsSite := catalogSite(s).key("AcroForm").key("SigFlags")
	flags, err := signatureSlot(s, form["SigFlags"], flagsSite, diag.SignaturePermissions)
	if err != nil {
		return err
	}
	if flags == nil && p.counts.PopulatedSignatureFields == 0 {
		return nil
	}
	if p.counts.PopulatedSignatureFields > 0 && xfa != (sourceSite{}) {
		return signatureUnsupported(xfa, diag.SignatureVisibility)
	}
	if ref, ok := cat["AcroForm"].(types.IndirectRef); ok {
		p.exclusive[ref] = true
	}
	updated := maps.Clone(form)
	delete(updated, "SigFlags")
	if flags != nil {
		n, ok := flags.(types.Integer)
		if !ok || n < 0 || n > 3 {
			return signatureUnsupported(flagsSite, diag.SignaturePermissions)
		}
		p.counts.SignatureFlagEntries++
		p.note(flagsSite)
	}
	if reduced {
		updated["Fields"] = fields
	}
	if p.counts.PopulatedSignatureFields > 0 {
		order, err := s.DereferenceArray(form["CO"])
		if err != nil {
			return err
		}
		if len(order) != 0 {
			kept := make(types.Array, 0, len(order))
			for _, ref := range order {
				d, err := s.DereferenceDict(ref)
				if err != nil || d == nil {
					return signatureUnsupported(catalogSite(s).key("AcroForm").key("CO"), diag.SignatureOwnership)
				}
				if !p.removed[layerDictIdentity(d)] {
					kept = append(kept, ref)
				}
			}
			if len(kept) == 0 {
				delete(updated, "CO")
			} else {
				updated["CO"] = kept
			}
		}
	}
	p.edits = append(p.edits, func() {
		if len(fields) == 0 {
			delete(cat, "AcroForm")
		} else {
			cat["AcroForm"] = updated
		}
	})
	return nil
}

func planSignatureDiscard(c context.Context, s *model.Context, pages []pageRef, content pageContentSnapshot, evidence signatureEvidence, roots []*formFieldNode, xfa sourceSite) (signatureDiscard, error) {
	cat, err := s.Catalog()
	if err != nil {
		return signatureDiscard{}, err
	}
	p := signatureDiscard{
		evidence: evidence, first: evidence.first,
		removed: map[uintptr]bool{}, exclusive: map[types.IndirectRef]bool{},
		widgets: map[types.IndirectRef]*formFieldNode{}, seenValues: map[uintptr]bool{},
		changedFields: map[types.IndirectRef]map[signatureOwnerEdge]bool{}, fieldNames: map[string]bool{},
	}
	if err := p.planForm(c, s, cat, roots, xfa); err != nil {
		return p, err
	}
	if err := p.planCatalog(c, s, cat); err != nil {
		return p, err
	}
	if err := p.checkEvidenceOwners(); err != nil {
		return p, err
	}
	if err := p.checkChangedFieldOwners(c, s); err != nil {
		return p, err
	}
	if err := p.checkFormActions(c, s); err != nil {
		return p, err
	}
	_, err = walkReferences(c, s, sourceOrigins{}, p.owners,
		func(_ types.IndirectRef, site sourceSite) (types.IndirectRef, error) {
			return types.IndirectRef{}, signatureUnsupported(site, diag.SignatureOwnership)
		}, nil)
	if err != nil {
		return p, err
	}
	return p, p.planWidgets(c, s, pages, content)
}

func (p *signatureDiscard) apply(c context.Context, s *model.Context, pages []pageRef, content pageContentSnapshot) ([]signatureArtwork, diag.Diagnostic, error) {
	for _, edit := range p.edits {
		if err := c.Err(); err != nil {
			return nil, diag.Diagnostic{}, err
		}
		edit()
	}
	if err := p.checkRemainingOwners(c, s); err != nil {
		return nil, diag.Diagnostic{}, err
	}
	if err := p.verifyEncodedContent(c, s, pages, content); err != nil {
		return nil, diag.Diagnostic{}, err
	}
	if err := freeUnreachable(c, s); err != nil {
		return nil, diag.Diagnostic{}, err
	}
	if len(p.artwork) > 0 {
		stage := newSignatureStage(packet.SignaturesDiscard)
		for _, art := range p.artwork {
			stage.artwork[art.page] = append(stage.artwork[art.page], art)
		}
		normalized, err := observePageContent(c, s, pages)
		if err != nil {
			return nil, diag.Diagnostic{}, err
		}
		if err := stage.verify(c, s, pages, normalized, true); err != nil {
			return nil, diag.Diagnostic{}, err
		}
	}
	p.counts.Widgets = len(p.widgets)
	d := diag.SignatureDiscarded(p.counts)
	d.PDFLocation = p.first
	return p.artwork, d, nil
}

// The normalized page either retains the original stream array or adds one
// generated stream on each end. Compare original identity, order, and encoding.
func (p *signatureDiscard) verifyEncodedContent(c context.Context, s *model.Context, pages []pageRef, before pageContentSnapshot) error {
	after, err := observePageContent(c, s, pages)
	if err != nil {
		return err
	}
	drawingPages := map[int]bool{}
	for _, art := range p.artwork {
		drawingPages[art.page] = true
	}
	for i := range after {
		if !drawingPages[i+1] {
			continue
		}
		streams := after[i].streams
		if len(streams) < 2 {
			return contentFailure(pages[i], i+1, diag.PDFValidationChangedThePageSequenceOrOriginalEncodedContent())
		}
		after[i].streams = streams[1 : len(streams)-1]
	}
	return before.compare(after)
}

// An action can target a form field by its full or parent name without leaving
// an indirect edge to it. Scripts and untargeted form actions do not supply
// sufficient evidence of noninterference.
func (p *signatureDiscard) checkFormActions(c context.Context, s *model.Context) error {
	if p.counts.PopulatedSignatureFields == 0 {
		return nil
	}
	return objectLocations(c, s, false, func(d types.Dict, loc diag.PDFLocation) error {
		action, err := nameValue(s, d["S"])
		if err != nil {
			return err
		}
		bad := refuse(diag.SourceSignatureDiscardUnsupported, diag.SignatureDiscardUnsupported(diag.SignatureVisibility))
		var raw types.Object
		switch action {
		case "JavaScript", "ImportData":
			return structureAt(bad, loc)
		case "Hide":
			raw = d["T"]
		case "ResetForm", "SubmitForm":
			raw = d["Fields"]
		default:
			return nil
		}
		value, err := s.Dereference(raw)
		if err != nil || value == nil {
			return structureAt(bad, loc)
		}
		targets, array := value.(types.Array)
		if !array {
			targets = types.Array{raw}
		}
		for _, target := range targets {
			value, err := s.Dereference(target)
			if err != nil {
				return err
			}
			// Indirect field/widget identity is checked in the surviving graph.
			if _, dict := value.(types.Dict); dict {
				continue
			}
			text, err := textOf(value)
			if err != nil || text == "" {
				return structureAt(bad, loc)
			}
			if p.fieldNames[text] {
				return structureAt(bad, loc)
			}
		}
		return nil
	})
}

func (p *signatureDiscard) checkEvidenceOwners() error {
	var conflict *diag.PDFLocation
	for _, object := range p.evidence.dicts {
		loc := object.location
		if !p.removed[layerDictIdentity(object.dict)] && (conflict == nil || loc.Object < conflict.Object || loc.Object == conflict.Object && loc.Path < conflict.Path) {
			conflict = &loc
		}
	}
	if conflict != nil {
		return structureAt(refuse(diag.SourceSignatureDiscardUnsupported, diag.SignatureDiscardUnsupported(diag.SignatureOwnership)), *conflict)
	}
	return nil
}

func (p *signatureDiscard) checkChangedFieldOwners(c context.Context, s *model.Context) error {
	if len(p.changedFields) == 0 {
		return nil
	}
	roots := originalReferenceRoots(s)
	trailer := maps.Clone(s.OriginalTrailer)
	delete(trailer, "Root")
	roots = append(roots, referenceRoot{trailer, sourceSite{path: "Trailer"}})
	_, err := walkReferences(c, s, sourceOrigins{}, roots,
		func(types.IndirectRef, sourceSite) (types.IndirectRef, error) {
			return types.IndirectRef{}, nil
		}, func(ref types.IndirectRef, site sourceSite) error {
			if allowed := p.changedFields[ref]; allowed != nil && !allowed[signatureEdge(site)] {
				return signatureUnsupported(site, diag.SignatureOwnership)
			}
			return nil
		})
	return err
}

func (p *signatureDiscard) checkRemainingOwners(c context.Context, s *model.Context) error {
	roots := originalReferenceRoots(s)
	if s.OriginalTrailer != nil {
		roots = append(roots, referenceRoot{s.OriginalTrailer, sourceSite{path: "Trailer"}})
	}
	_, err := walkReferences(c, s, sourceOrigins{}, roots,
		func(types.IndirectRef, sourceSite) (types.IndirectRef, error) {
			return types.IndirectRef{}, nil
		}, func(ref types.IndirectRef, site sourceSite) error {
			if p.exclusive[ref] {
				return signatureUnsupported(site, diag.SignatureOwnership)
			}
			return nil
		})
	return err
}

func checkSignatureValidationStore(c context.Context, s *model.Context, key string, d types.Dict, site sourceSite) error {
	for _, name := range slices.Sorted(maps.Keys(d)) {
		if err := c.Err(); err != nil {
			return err
		}
		raw := d[name]
		at := site.key(name)
		switch {
		case key == "VRI":
			entry, err := s.DereferenceDict(raw)
			if err != nil || entry == nil {
				return signatureUnsupported(at, diag.SignaturePermissions)
			}
			if err := checkSignatureValidationStore(c, s, "VRI entry", entry, at); err != nil {
				return err
			}
		case name == "Type":
			kind, err := nameValue(s, raw)
			if err != nil || key == "DSS" && kind != "DSS" || key == "VRI entry" && kind != "VRI" {
				return signatureUnsupported(at, diag.SignaturePermissions)
			}
		case key == "DSS" && name == "VRI":
			entries, err := s.DereferenceDict(raw)
			if err != nil || entries == nil {
				return signatureUnsupported(at, diag.SignaturePermissions)
			}
			if err := checkSignatureValidationStore(c, s, "VRI", entries, at); err != nil {
				return err
			}
		case key == "DSS" && (name == "Certs" || name == "OCSPs" || name == "CRLs"),
			key == "VRI entry" && (name == "Cert" || name == "OCSP" || name == "CRL"):
			members, err := s.DereferenceArray(raw)
			if err != nil || members == nil {
				return signatureUnsupported(at, diag.SignaturePermissions)
			}
			for _, member := range members {
				if err := c.Err(); err != nil {
					return err
				}
				stream, _, err := s.DereferenceStreamDict(member)
				if err != nil || stream == nil {
					return signatureUnsupported(at, diag.SignaturePermissions)
				}
			}
		case key == "VRI entry" && (name == "TU" || name == "TS"):
			// Time and time-stamp token entries are opaque metadata.
		default:
			return signatureUnsupported(at, diag.SignaturePermissions)
		}
	}
	return nil
}

func checkOutputSignatures(c context.Context, s *model.Context, discard bool) error {
	e, _, err := originalSignatureEvidence(c, s)
	if err != nil {
		return err
	}
	if err := e.refusal(false); err != nil {
		return err
	}
	if !discard {
		return nil
	}
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	form, err := s.DereferenceDict(cat["AcroForm"])
	if err != nil {
		return err
	}
	for _, raw := range []types.Object{cat["Perms"], cat["DocTimeStamp"], cat["DSS"], cat["VRI"], form["SigFlags"]} {
		value, err := s.Dereference(raw)
		if err != nil {
			return err
		}
		if value != nil {
			return errors.New("signature metadata remains in the serialized output")
		}
	}
	return nil
}
