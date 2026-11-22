// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"maps"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

type layerProfile struct {
	context context.Context
	pdf     *model.Context
	err     error
}

func (p *layerProfile) require(ok bool, site sourceSite) {
	if !ok && p.err == nil {
		p.err = layerRefusal(site, diag.OptionalContentProfileUnproved())
	}
}

func (p *layerProfile) value(o types.Object, site sourceSite) types.Object {
	if p.err != nil {
		return nil
	}
	if p.err = p.context.Err(); p.err != nil {
		return nil
	}
	v, err := layerValue(p.pdf, o, site)
	p.err = err
	return v
}

func (p *layerProfile) dict(o types.Object, site sourceSite, keys ...string) types.Dict {
	d, ok := p.value(o, site).(types.Dict)
	p.require(ok, site)
	for _, key := range slices.Sorted(maps.Keys(d)) {
		if p.err != nil {
			break
		}
		if !slices.Contains(keys, key) {
			where := site.key(key)
			p.require(p.value(d[key], where) == nil, where)
		}
	}
	return d
}

func (p *layerProfile) array(d types.Dict, key string, site sourceSite) types.Array {
	where := site.key(key)
	value := p.value(d[key], where)
	if value == nil {
		return nil
	}
	a, ok := value.(types.Array)
	p.require(ok, where)
	return a
}

func (p *layerProfile) optionalName(d types.Dict, key string, defaultValue types.Name, site sourceSite) types.Name {
	where := site.key(key)
	value := p.value(d[key], where)
	if value == nil {
		return defaultValue
	}
	n, ok := value.(types.Name)
	p.require(ok, where)
	return n
}

func (p *layerProfile) intent(d types.Dict, site sourceSite) {
	where := site.key("Intent")
	value := p.value(d["Intent"], where)
	if value == nil {
		return
	}
	if a, ok := value.(types.Array); ok && len(a) == 1 {
		value = p.value(a[0], where.index(0))
	}
	p.require(value == types.Name("View"), where)
}

// staticVisibleLayer checks catalog and group metadata without observing any
// Form owners or drawing programs.
func staticVisibleLayer(c context.Context, s *model.Context, raw types.Object, site sourceSite) (optionalContentPlan, error) {
	p := layerProfile{context: c, pdf: s}
	var result optionalContentPlan
	root := p.dict(raw, site, "OCGs", "D", "Configs")
	groups := p.array(root, "OCGs", site)
	p.require(len(groups) == 1, site.key("OCGs"))
	if p.err != nil {
		return result, p.err
	}
	group, indirect := groups[0].(types.IndirectRef)
	p.require(indirect, site.key("OCGs").index(0))
	groupSite := site.key("OCGs").index(0).anchor(group, sourceOrigins{})
	d := p.dict(group, groupSite, "Type", "Name", "Usage", "Intent")
	p.intent(d, groupSite)
	p.require(p.value(d["Type"], groupSite.key("Type")) == types.Name("OCG"), groupSite.key("Type"))
	name := p.value(d["Name"], groupSite.key("Name"))
	switch name.(type) {
	case types.StringLiteral, types.HexLiteral:
		result.name, p.err = s.DereferenceText(name)
	default:
		p.require(false, groupSite.key("Name"))
	}
	events := []string{"View", "Print", "Export"}
	usageSite := groupSite.key("Usage")
	usageRaw := p.value(d["Usage"], usageSite)
	hasUsage := usageRaw != nil
	if hasUsage {
		usage := p.dict(usageRaw, usageSite, "View", "Print", "Export", "PageElement")
		for _, event := range events {
			eventSite := usageSite.key(event)
			state := event + "State"
			u := p.dict(usage[event], eventSite, state)
			p.require(p.value(u[state], eventSite.key(state)) == types.Name("ON"), eventSite.key(state))
		}
		elementSite := usageSite.key("PageElement")
		element := p.dict(usage["PageElement"], elementSite, "Subtype")
		subtype := p.value(element["Subtype"], elementSite.key("Subtype"))
		p.require(subtype == types.Name("FG") || subtype == types.Name("BG"), elementSite.key("Subtype"))
	}
	p.require(len(p.array(root, "Configs", site)) == 0, site.key("Configs"))
	configSite := site.key("D")
	config := p.dict(root["D"], configSite, "BaseState", "ON", "OFF", "Order", "RBGroups", "AS", "Intent")
	p.intent(config, configSite)
	base := p.optionalName(config, "BaseState", "ON", configSite)
	on := p.array(config, "ON", configSite)
	exactOn := len(on) == 1 && on[0] == group
	p.require(base == "ON", configSite.key("BaseState"))
	p.require(exactOn || len(on) == 0, configSite.key("ON"))
	for _, key := range []string{"OFF", "Order", "RBGroups"} {
		p.require(len(p.array(config, key, configSite)) == 0, configSite.key(key))
	}
	applications := p.array(config, "AS", configSite)
	p.require(len(applications) == len(events) || len(applications) == 0 && !hasUsage, configSite.key("AS"))
	if p.err != nil {
		return result, p.err
	}
	seen := map[string]bool{}
	for i, entry := range applications {
		appSite := configSite.key("AS").index(i)
		app := p.dict(entry, appSite, "Event", "Category", "OCGs")
		event := string(p.optionalName(app, "Event", "", appSite))
		p.require(slices.Contains(events, event) && !seen[event], appSite.key("Event"))
		seen[event] = true
		categories := p.array(app, "Category", appSite)
		p.require(len(categories) == 1 && p.value(categories[0], appSite.key("Category").index(0)) == types.Name(event), appSite.key("Category"))
		members := p.array(app, "OCGs", appSite)
		p.require(len(members) == 1 && members[0] == group, appSite.key("OCGs"))
	}
	result.group = group
	return result, p.err
}

// Keep the observed dictionary alive for identity comparisons until planning
// finishes. The parser removes explicit null values from the semantic map.
type layerNullEvidence map[uintptr]types.Dict

func (e layerNullEvidence) hasNull(d types.Dict) bool {
	return e[layerDictIdentity(d)] != nil
}

// emptyLayerCatalog proves the exact settings shape and records every indirect
// identity that must be confined to the catalog edge.
func emptyLayerCatalog(c context.Context, s *model.Context, raw types.Object, site sourceSite, nulls layerNullEvidence) (map[types.IndirectRef]bool, error) {
	refs := map[types.IndirectRef]bool{}
	resolve := func(raw types.Object, site sourceSite) (types.Object, error) {
		if err := c.Err(); err != nil {
			return nil, err
		}
		if ref, ok := raw.(types.IndirectRef); ok {
			refs[ref] = true
		}
		return layerValue(s, raw, site)
	}
	root, err := resolve(raw, site)
	if err != nil {
		return nil, err
	}
	d, ok := root.(types.Dict)
	if !ok || len(d) != 2 || nulls.hasNull(d) {
		return nil, layerRefusal(site, diag.OptionalContentRefused())
	}
	empty := func(raw types.Object, site sourceSite) error {
		value, err := resolve(raw, site)
		if err != nil {
			return err
		}
		if a, ok := value.(types.Array); !ok || len(a) != 0 {
			return layerRefusal(site, diag.OptionalContentRefused())
		}
		return nil
	}
	if err := empty(d["OCGs"], site.key("OCGs")); err != nil {
		return nil, err
	}
	configSite := site.key("D")
	value, err := resolve(d["D"], configSite)
	if err != nil {
		return nil, err
	}
	config, ok := value.(types.Dict)
	if !ok || nulls.hasNull(config) {
		return nil, layerRefusal(configSite, diag.OptionalContentRefused())
	}
	for _, key := range slices.Sorted(maps.Keys(config)) {
		where := configSite.key(key)
		switch key {
		case "ON", "Order", "RBGroups":
			if err := empty(config[key], where); err != nil {
				return nil, err
			}
		default:
			return nil, layerRefusal(where, diag.OptionalContentRefused())
		}
	}
	return refs, nil
}
