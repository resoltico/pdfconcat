// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

type pageWrappers struct {
	pages      []wrapperPage
	pageOwners map[uintptr]bool
	resources  map[uintptr]bool
	properties map[uintptr]map[string]wrapperBinding
	streams    map[types.IndirectRef]bool
	names      map[types.Name]bool
}

type wrapperPage struct {
	number  int
	content pageContent
	tokens  [4]layerSpan
	ends    []int
	decoded [sha256.Size]byte
}

type wrapperBinding struct {
	owner types.Dict
	value types.Object
	site  sourceSite
}

func layerDictIdentity(d types.Dict) uintptr { return reflect.ValueOf(d).Pointer() }

// Page resources are inherited as a unit. A closer dictionary replaces the
// ancestor's dictionary even when it doesn't supply a Properties entry.
func effectiveLayerResources(s *model.Context, page pageRef, site sourceSite) (types.Dict, error) {
	resolve := func(d types.Dict) (types.Dict, error) {
		where := site.key("Resources")
		value, err := layerValue(s, d["Resources"], where)
		if err != nil || value == nil {
			return nil, err
		}
		resources, ok := value.(types.Dict)
		if !ok {
			return nil, layerRefusal(where, diag.OptionalContentRefused())
		}
		return resources, nil
	}
	d, err := resolve(page.dict)
	for node := page.parent; d == nil && err == nil && node != nil; node = node.parent {
		d, err = resolve(node.dict)
	}
	return d, err
}

func layerPagePropertiesFor(s *model.Context, page pageRef, site sourceSite) (types.Dict, types.Dict, error) {
	resources, err := effectiveLayerResources(s, page, site)
	if err != nil {
		return nil, nil, err
	}
	props, err := drawingLayerProperties(s, resources, site)
	return resources, props, err
}

func drawingLayerProperties(s *model.Context, raw types.Object, site sourceSite) (types.Dict, error) {
	where := site.key("Resources")
	value, err := layerValue(s, raw, where)
	if err != nil || value == nil {
		return nil, err
	}
	resources, ok := value.(types.Dict)
	if !ok {
		return nil, layerRefusal(where, diag.OptionalContentRefused())
	}
	where = where.key("Properties")
	value, err = layerValue(s, resources["Properties"], where)
	if err != nil || value == nil {
		return nil, err
	}
	props, ok := value.(types.Dict)
	if !ok {
		return nil, layerRefusal(where, diag.OptionalContentRefused())
	}
	return props, nil
}

func layerPropertyName(op layerOperation) (types.Name, bool) {
	if (op.name == "BDC" || op.name == "DP") && len(op.args) == 2 {
		name, ok := op.args[1].value.(types.Name)
		return name, ok
	}
	return "", false
}

func planPageWrappers(c context.Context, s *model.Context, pages []pageRef, content pageContentSnapshot, nulls layerNullEvidence) (*pageWrappers, error) {
	plan := &pageWrappers{
		pageOwners: map[uintptr]bool{},
		resources:  map[uintptr]bool{},
		properties: map[uintptr]map[string]wrapperBinding{},
		streams:    map[types.IndirectRef]bool{},
		names:      map[types.Name]bool{},
	}
	type pagePropertyUses struct {
		index int
		names []types.Name
	}
	var ordinary []pagePropertyUses
	programs := layerPrograms{remaining: maxLayerProgramBytes, pdf20: s.PDF20()}
	var firstUnproved error
	for i, page := range pages {
		site := sourceSite{ref: page.ref, page: i + 1}
		contents := site.key("Contents")
		bad := layerRefusal(contents, diag.OptionalContentProgramUnproved())
		data, ends, err := programs.page(c, s, content[i], contents)
		var name types.Name
		var tokens [4]layerSpan
		started, closed, sawOptional := false, false, false
		depth := 0
		var names []types.Name
		if err == nil {
			err = programs.scan(c, data, ends, contents, layerVerify, func(op layerOperation) error {
				optional := op.inlineLayer()
				if candidate, named := op.visibilityName(); named {
					_, props, err := layerPagePropertiesFor(s, page, site)
					if err != nil {
						sawOptional = true
						return err
					}
					optional, err = namedVisibilityProperty(s, props, candidate, contents)
					if err != nil {
						sawOptional = true
						return err
					}
				}
				if optional {
					sawOptional = true
					var named bool
					name, named = layerPropertyName(op)
					if started || op.name != "BDC" || !named {
						return bad
					}
					tokens[0], tokens[1], tokens[2] = op.args[0].layerSpan, op.args[1].layerSpan, op.layerSpan
				} else if name, named := layerPropertyName(op); named {
					names = append(names, name)
				}
				if closed {
					return bad
				}
				started = true
				switch op.name {
				case "BDC", "BMC":
					depth++
				case "EMC":
					depth--
					if depth < 0 || len(op.args) != 0 {
						return bad
					}
					if sawOptional && depth == 0 {
						tokens[3], closed = op.layerSpan, true
					}
				}
				return nil
			})
		}
		if err == nil && depth != 0 {
			err = bad
		}
		if err != nil {
			if c.Err() != nil {
				return nil, c.Err()
			}
			if sawOptional || !optionalContentRefusal(err) {
				return nil, err
			}
			if firstUnproved == nil {
				firstUnproved = err
			}
			continue
		}
		if len(names) != 0 {
			ordinary = append(ordinary, pagePropertyUses{i, names})
		}
		if !sawOptional {
			continue
		}
		resources, props, err := layerPagePropertiesFor(s, page, site)
		if err != nil {
			return nil, err
		}
		propertySite := site.key("Resources").key("Properties")
		bindingSite := propertySite.key(string(name))
		raw := props[string(name)]
		if err := proveBareGroup(c, s, raw, bindingSite, nulls); err != nil {
			return nil, err
		}
		plan.resources[layerDictIdentity(resources)] = true
		id := layerDictIdentity(props)
		if plan.properties[id] == nil {
			plan.properties[id] = map[string]wrapperBinding{}
		}
		plan.properties[id][string(name)] = wrapperBinding{props, raw, bindingSite}
		plan.names[name] = true
		for _, span := range tokens {
			index, _ := slices.BinarySearch(ends, span.start+1)
			plan.streams[content[i].streams[index].ref] = true
		}
		plan.pages = append(plan.pages, wrapperPage{i + 1, content[i], tokens, ends, sha256.Sum256(data)})
	}
	if len(plan.pages) == 0 {
		return plan, nil
	}
	if firstUnproved != nil {
		return nil, firstUnproved
	}
	if s.XRefTable.Version() < model.V15 {
		page := plan.pages[0]
		return nil, layerRefusal(sourceSite{ref: page.content.ref, page: page.number}.key("Contents"), diag.OptionalContentRefused())
	}
	if err := plan.provePageResourceOwners(c, s, pages); err != nil {
		return nil, err
	}
	for _, use := range ordinary {
		page := pages[use.index]
		site := sourceSite{ref: page.ref, page: use.index + 1}
		_, props, err := layerPagePropertiesFor(s, page, site)
		if err != nil {
			return nil, err
		}
		bindings := plan.properties[layerDictIdentity(props)]
		for _, name := range use.names {
			if _, edited := bindings[string(name)]; edited {
				return nil, layerRefusal(site.key("Contents"), diag.OptionalContentRefused())
			}
		}
	}
	return plan, nil
}

// A reader may follow Parent for inherited resources. Its ancestry must
// agree with the Kids traversal used to enumerate pages and their consumers.
func (p *pageWrappers) provePageResourceOwners(c context.Context, s *model.Context, pages []pageRef) error {
	for i, page := range pages {
		if err := c.Err(); err != nil {
			return err
		}
		where := sourceSite{ref: page.ref, page: i + 1}.key("Parent")
		if page.parent == nil || page.dict["Parent"] != page.parent.ref {
			return layerRefusal(where, diag.OptionalContentRefused())
		}
		p.pageOwners[layerDictIdentity(page.dict)] = true
		for node := page.parent; node != nil; node = node.parent {
			id := layerDictIdentity(node.dict)
			if p.pageOwners[id] {
				break
			}
			p.pageOwners[id] = true
			where := sourceSite{ref: node.ref}.key("Parent")
			if node.parent != nil {
				if node.dict["Parent"] != node.parent.ref {
					return layerRefusal(where, diag.OptionalContentRefused())
				}
			} else {
				parent, err := layerValue(s, node.dict["Parent"], where)
				if err != nil {
					return err
				}
				if parent != nil {
					return layerRefusal(where, diag.OptionalContentRefused())
				}
			}
		}
	}
	return nil
}

func proveBareGroup(c context.Context, s *model.Context, raw types.Object, site sourceSite, nulls layerNullEvidence) error {
	p := layerProfile{context: c, pdf: s}
	value := p.value(raw, site)
	d, ok := value.(types.Dict)
	p.require(ok && len(d) == 2 && !nulls.hasNull(d), site)
	p.require(p.value(d["Type"], site.key("Type")) == types.Name("OCG"), site.key("Type"))
	name := p.value(d["Name"], site.key("Name"))
	switch name.(type) {
	case types.StringLiteral, types.HexLiteral:
		_, err := s.DereferenceText(name)
		p.require(err == nil, site.key("Name"))
	default:
		p.require(false, site.key("Name"))
	}
	return p.err
}

func sameLayerObject(a, b types.Object) bool {
	switch a := a.(type) {
	case types.IndirectRef:
		ref, ok := b.(types.IndirectRef)
		return ok && ref == a
	case types.Dict:
		d, ok := b.(types.Dict)
		return ok && layerDictIdentity(d) == layerDictIdentity(a)
	default:
		return false
	}
}

func (p *pageWrappers) apply(c context.Context, s *model.Context, pages []pageRef, original pageContentSnapshot, nulls layerNullEvidence) error {
	var remove []func()
	for _, bindings := range p.properties {
		for _, name := range slices.Sorted(maps.Keys(bindings)) {
			binding := bindings[name]
			if !sameLayerObject(binding.owner[name], binding.value) {
				return layerRefusal(binding.site, diag.OptionalContentRefused())
			}
			if err := proveBareGroup(c, s, binding.value, binding.site, nulls); err != nil {
				return err
			}
			remove = append(remove, func() { delete(binding.owner, name) })
		}
	}
	var replace []func()
	programs := layerPrograms{remaining: maxLayerProgramBytes, pdf20: s.PDF20()}
	for _, edit := range p.pages {
		if err := c.Err(); err != nil {
			return err
		}
		page := pages[edit.number-1]
		site := sourceSite{ref: edit.content.ref, page: edit.number}.key("Contents")
		bad := layerRefusal(site, diag.OptionalContentProgramUnproved())
		if page.ref != edit.content.ref || !slices.Equal(original[edit.number-1].streams, edit.content.streams) {
			return bad
		}
		data, ends, err := programs.page(c, s, original[edit.number-1], site)
		if err != nil {
			return err
		}
		programs.remaining -= int64(len(data)) + 1
		if !slices.Equal(ends, edit.ends) || sha256.Sum256(data) != edit.decoded {
			return bad
		}
		raw, err := s.Dereference(page.dict["Contents"])
		if err != nil {
			return err
		}
		var array types.Array
		if a, ok := raw.(types.Array); ok {
			array = slices.Clone(a)
		}
		start, token := 0, 0
		for i, end := range ends {
			first := token
			for token < len(edit.tokens) && edit.tokens[token].end <= end {
				token++
			}
			spans := edit.tokens[first:token]
			if len(spans) != 0 {
				ref, err := replaceWrapperStream(c, s, edit.content.streams[i], data[start:end], start, spans, bad)
				if err != nil {
					return err
				}
				if array == nil {
					replace = append(replace, func() { page.dict["Contents"] = *ref })
				} else {
					array[i] = *ref
				}
			}
			start = end
		}
		if array != nil {
			replace = append(replace, func() { page.dict["Contents"] = array })
		}
	}
	for _, apply := range replace {
		apply()
	}
	for _, apply := range remove {
		apply()
	}
	return nil
}

func replaceWrapperStream(c context.Context, s *model.Context, expected contentStream, original []byte, start int, spans []layerSpan, bad error) (*types.IndirectRef, error) {
	if !liveReference(s, expected.ref) {
		return nil, bad
	}
	raw, err := s.Dereference(expected.ref)
	if err != nil {
		return nil, err
	}
	stream, ok := raw.(types.StreamDict)
	if !ok || sha256.Sum256(stream.Raw) != expected.digest {
		return nil, bad
	}
	data := bytes.Clone(original)
	for _, span := range spans {
		for i := span.start - start; i < span.end-start; i++ {
			data[i] = ' '
		}
	}
	extra := maps.Clone(stream.Dict)
	for _, key := range []string{"Length", "Filter", "DecodeParms", "FFilter", "FDecodeParms"} {
		delete(extra, key)
	}
	ref, err := addStream(s, data, extra)
	if err != nil {
		return nil, err
	}
	value, err := s.Dereference(*ref)
	if err != nil {
		return nil, err
	}
	replacement, ok := value.(types.StreamDict)
	if !ok {
		return nil, fmt.Errorf("replacement content object %d is not a stream", ref.ObjectNumber.Value())
	}
	decoded, err := decodeDrawingStream(c, s, replacement)
	if err != nil {
		return nil, err
	}
	if !verifiedWrapperMask(original, decoded, start, spans) {
		return nil, bad
	}
	return ref, nil
}

func verifiedWrapperMask(before, after []byte, start int, spans []layerSpan) bool {
	if len(before) != len(after) {
		return false
	}
	at := 0
	for _, span := range spans {
		a, b := span.start-start, span.end-start
		if a < at || b <= a || b > len(before) || !bytes.Equal(before[at:a], after[at:a]) {
			return false
		}
		for i := a; i < b; i++ {
			if after[i] != ' ' || pdfSpace(before[i]) {
				return false
			}
		}
		at = b
	}
	return bytes.Equal(before[at:], after[at:])
}
