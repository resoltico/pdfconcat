// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"net/url"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

type externalActionObserver struct {
	source   *model.Context
	base     sourceSite
	assembly bool
	findings []diag.Diagnostic
}

func newExternalActionObserver(s *model.Context, origins sourceOrigins, assembly bool) (externalActionObserver, error) {
	observer := externalActionObserver{source: s, assembly: assembly}
	cat, err := s.Catalog()
	if err != nil {
		return observer, err
	}
	uri, err := s.DereferenceDict(cat["URI"])
	if err != nil {
		return observer, err
	}
	base, err := textValue(s, uri["Base"])
	if err != nil {
		return observer, err
	}
	if base != "" {
		observer.base = origins.catalogEntry("URI.Base", catalogSite(s).key("URI").anchor(cat["URI"], origins).key("Base"))
	}
	return observer, nil
}

// Projection calls this on retained actions before their owning slots are
// edited. This observer measures a location dependency on authored bytes.
func (o *externalActionObserver) visit(d types.Dict, site sourceSite, kind types.Name) error {
	var finding diag.Diagnostic
	switch kind {
	case "URI":
		value, defined, err := resolveNavigation(o.source, d["URI"])
		if !defined && err == nil {
			return nil
		}
		var target string
		if err == nil {
			target, err = textOf(value)
		}
		if err != nil {
			if o.base != (sourceSite{}) {
				return err
			}
			return nil
		}
		if !relativeURI(target) {
			return nil
		}
		site = site.key("URI")
		if o.base != (sourceSite{}) {
			evidence := diag.URIBaseEvidence(int64(o.base.ref.ObjectNumber.Value()), int64(o.base.ref.GenerationNumber.Value()), o.base.path, o.assembly)
			finding = sourceWarning(diag.SourceURIBaseRemoved, diag.SourceURIBaseIsRemovedRetainedRelativeURIActionsMay(), evidence, site)
		} else {
			finding = o.relativeFinding(site, "URI", nil)
		}
	case "GoToR":
		keys, relative := relativeRemoteFile(o.source, d["F"])
		if !relative {
			return nil
		}
		finding = o.relativeFinding(site.key("F"), "GoToR", keys)
	default:
		return nil
	}
	if o.assembly {
		finding.Identity.Phase = "assembly"
	}
	o.findings = append(o.findings, finding)
	return nil
}

func (o *externalActionObserver) relativeFinding(site sourceSite, kind string, filenameKeys []string) diag.Diagnostic {
	return sourceWarning(diag.SourceRelativeActionContext, diag.RelativeActionUsesPacketLocation(),
		diag.RelativeActionContextEvidence(kind, filenameKeys, o.assembly), site)
}

func relativeURI(target string) bool {
	value, err := url.Parse(target)
	return err == nil && !value.IsAbs()
}

func relativeRemoteFile(s *model.Context, raw types.Object) ([]string, bool) {
	value, defined, err := resolveNavigation(s, raw)
	if err != nil || !defined {
		return nil, false
	}
	if spec, ok := value.(types.Dict); ok {
		return relativeRemoteSpec(s, spec)
	}
	name, ok := remoteFilename(value)
	return nil, ok && relativePDFFile(name)
}

func relativeRemoteSpec(s *model.Context, spec types.Dict) ([]string, bool) {
	embedded, defined, err := resolveNavigation(s, spec["EF"])
	if err != nil || !defined || embedded != nil {
		return nil, false
	}
	system, defined, err := resolveNavigation(s, spec["FS"])
	if err != nil || !defined || system != nil && system != types.Name("URL") {
		return nil, false
	}
	urlFile := system == types.Name("URL")
	key, fallback := "UF", "F"
	if urlFile {
		key, fallback = fallback, key
	}
	value, defined, err := resolveNavigation(s, spec[key])
	if err != nil || !defined {
		return nil, false
	}
	if value == nil {
		key = fallback
		value, defined, err = resolveNavigation(s, spec[key])
		if err != nil || !defined {
			return nil, false
		}
	}
	if value != nil {
		name, ok := remoteFilename(value)
		if !ok {
			return nil, false
		}
		if name != "" {
			if urlFile {
				return []string{key}, relativeURI(name)
			}
			return []string{key}, relativePDFFile(name)
		}
	}
	if urlFile || s.XRefTable.Version() >= model.V20 {
		return nil, false
	}
	var slots []string
	// The obsolescent slots carry PDF file specification strings for
	// different reader platforms. Any relative candidate is relevant.
	for _, key := range []string{"DOS", "Mac", "Unix"} {
		value, defined, err := resolveNavigation(s, spec[key])
		if err != nil || !defined {
			continue
		}
		if name, ok := remoteFilename(value); ok && relativePDFFile(name) {
			slots = append(slots, key)
		}
	}
	return slots, len(slots) != 0
}

func remoteFilename(value types.Object) (string, bool) {
	switch value.(type) {
	case types.StringLiteral, types.HexLiteral:
	default:
		return "", false
	}
	name, err := textOf(value)
	return name, err == nil
}

func relativePDFFile(name string) bool {
	// ISO 32000-1 §7.11.2 defines the leading solidus as the absolute
	// form of a file specification string, independently of this host.
	return name != "" && !strings.HasPrefix(name, "/")
}

// Calls without selection requests observe the whole source without changing it.
func inspectExternalActions(c context.Context, s *model.Context, origins sourceOrigins) ([]diag.Diagnostic, error) {
	actions, err := newExternalActionObserver(s, origins, false)
	if err != nil {
		return nil, err
	}
	named, err := readDestinations(c, s)
	if err != nil {
		return nil, err
	}
	pages := make(map[types.IndirectRef]bool, len(origins.pages))
	for ref := range origins.pages {
		pages[ref] = true
	}
	err = walkRetained(c, s, origins, nil, func(binding retainedTarget) (bool, error) {
		return localTarget(binding.value, named, pages)
	}, actions.visit, false)
	return actions.findings, err
}
