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

func uriBaseWarnings(c context.Context, s *model.Context, origins sourceOrigins, assembly bool) ([]diag.Diagnostic, error) {
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
	e = walkRetained(c, s, origins, nil, func(binding retainedTarget) (bool, error) {
		return localTarget(binding.value, named, selected)
	}, func(d types.Dict, site sourceSite, kind types.Name) error {
		if kind != "URI" {
			return nil
		}
		text, e := textValue(s, d["URI"])
		if e != nil {
			return e
		}
		target, parseErr := url.Parse(text)
		if parseErr == nil && !target.IsAbs() {
			warning := sourceWarning(diag.SourceURIBaseRemoved, diag.SourceURIBaseIsRemovedRetainedRelativeURIActionsMay(), diag.URIBaseEvidence(int64(baseSite.ref.ObjectNumber.Value()), int64(baseSite.ref.GenerationNumber.Value()), baseSite.path, assembly), site.key("URI"))
			if assembly {
				warning.Identity.Phase = "assembly"
			}
			findings = append(findings, warning)
		}
		return nil
	}, false)
	return findings, e
}
