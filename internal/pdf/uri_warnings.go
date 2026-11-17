// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"net/url"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

func uriBaseWarnings(c context.Context, s *model.Context, origins sourceOrigins) ([]diag.Diagnostic, error) {
	cat, e := s.Catalog()
	if e != nil {
		return nil, e
	}
	uri, e := s.DereferenceDict(cat["URI"])
	if e != nil {
		return nil, e
	}
	base, e := textValue(s, uri["Base"])
	if e != nil || base == "" {
		return nil, e
	}
	baseSite := origins.catalogEntry("URI.Base", catalogSite(s).key("URI").anchor(cat["URI"], origins).key("Base"))
	named, e := readDestinations(c, s)
	if e != nil {
		return nil, e
	}
	pages, e := rawPages(c, s)
	if e != nil {
		return nil, e
	}
	selected := map[types.IndirectRef]bool{}
	for _, page := range pages {
		selected[page.ref] = true
	}
	var findings []diag.Diagnostic
	e = walkRetained(c, s, origins, nil, func(d types.Dict, key string, _ sourceSite, _ sourceSite) (bool, error) {
		return localTarget(s, d[key], named, selected)
	}, func(d types.Dict, site sourceSite) error {
		kind, e := nameValue(s, d["S"])
		if e != nil || kind != "URI" {
			return e
		}
		text, e := textValue(s, d["URI"])
		if e != nil {
			return e
		}
		target, parseErr := url.Parse(text)
		if parseErr == nil && !target.IsAbs() {
			findings = append(findings, sourceWarning(diag.SourceURIBaseRemoved, diag.SourceURIBaseIsRemovedRetainedRelativeURIActionsMay(), diag.URIBaseEvidence(int64(baseSite.ref.ObjectNumber.Value()), int64(baseSite.ref.GenerationNumber.Value()), baseSite.path), site.key("URI")))
		}
		return nil
	}, false)
	return findings, e
}
