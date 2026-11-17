// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"fmt"
	"strconv"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// sourceSite is an immutable address in the original source, never an output
// object identity. Generation stays private; projection uses the existing wire.
type sourceSite struct {
	ref  types.IndirectRef
	page int
	path string
}
type sourceOrigins struct {
	pages        map[types.IndirectRef]int
	annotations  map[types.IndirectRef]int
	destinations map[string]destination
	catalog      map[string]sourceSite
}

func sourceOriginsFor(c context.Context, s *model.Context) (sourceOrigins, error) {
	result := sourceOrigins{pages: map[types.IndirectRef]int{}, annotations: map[types.IndirectRef]int{}}
	pages, e := rawPages(c, s)
	if e != nil {
		return result, e
	}
	for i, p := range pages {
		result.pages[p.ref] = i + 1
		annots, e := s.DereferenceArray(p.dict["Annots"])
		if e != nil {
			return result, e
		}
		for _, o := range annots {
			if ref, ok := o.(types.IndirectRef); ok {
				previous, seen := result.annotations[ref]
				if !seen {
					result.annotations[ref] = i + 1
				} else if previous != i+1 {
					result.annotations[ref] = 0
				}
			}
		}
	}
	return result, nil
}
func (s sourceSite) key(key string) sourceSite {
	simple := key != ""
	for _, b := range []byte(key) {
		if (b < 'a' || b > 'z') && (b < 'A' || b > 'Z') && (b < '0' || b > '9') && b != '_' {
			simple = false
			break
		}
	}
	if simple {
		if s.path != "" {
			s.path += "."
		}
		s.path += key
	} else {
		s.path += "[" + strconv.QuoteToASCII(key) + "]"
	}
	return s
}
func (s sourceSite) name(name string) sourceSite {
	s.path += "[" + strconv.QuoteToASCII(name) + "]"
	return s
}
func (s sourceSite) index(i int) sourceSite { s.path += fmt.Sprintf("[%d]", i); return s }
func (s sourceSite) anchor(o types.Object, origins sourceOrigins) sourceSite {
	if ref, ok := o.(types.IndirectRef); ok {
		s.ref = ref
		s.path = ""
		if page := origins.pages[ref]; page > 0 {
			s.page = page
		} else if page, known := origins.annotations[ref]; known {
			s.page = page
		}
	}
	return s
}
func (s sourceSite) location() *diag.PDFLocation {
	loc := diag.PDFLocation{Object: s.ref.ObjectNumber.Value(), Page: s.page, Path: s.path}
	if loc.Object <= 0 && loc.Page <= 0 && loc.Path == "" {
		return nil
	}
	return &loc
}
func (s sourceSite) describe() string {
	return fmt.Sprintf("source object %d %d %s", s.ref.ObjectNumber.Value(), s.ref.GenerationNumber.Value(), s.path)
}
func catalogSite(s *model.Context) sourceSite { return sourceSite{ref: *s.Root, path: "Catalog"} }
func sourceWarning(code diag.Code, message, detail diag.Explanation, site sourceSite) diag.Diagnostic {
	d := diag.Report(code, message)
	diag.AttachEvidence(&d, detail)
	d.Identity.Phase = "source_observation"
	d.PDFLocation = site.location()
	return d
}

type navigationChange struct {
	site       sourceSite
	target     sourceSite
	targetPage int
	name       string
	action     bool
	outline    bool
}

func (n navigationChange) diagnostic() diag.Diagnostic {
	return sourceWarning(diag.SourceNavigationRemoved, diag.NavigationTargetsOmittedFromThisSourceOccurrenceWereRemoved(), diag.NavigationEvidence(n.name, int64(n.target.ref.ObjectNumber.Value()), int64(n.target.ref.GenerationNumber.Value()), n.target.path, int64(n.targetPage), n.action, n.outline), n.site)
}
func (n navigationChange) key() string {
	return n.site.describe() + fmt.Sprint("/", n.site.page, "/", n.targetPage, "/", n.name, "/", n.action, "/", n.outline)
}
func navigationDiagnostics(changes []navigationChange) []diag.Diagnostic {
	result := []diag.Diagnostic{}
	seen := map[string]bool{}
	for _, change := range changes {
		key := change.key()
		if !seen[key] {
			seen[key] = true
			result = append(result, change.diagnostic())
		}
	}
	return result
}

func captureCatalogSites(s *model.Context, origins sourceOrigins) (map[string]sourceSite, error) {
	cat, e := s.Catalog()
	if e != nil {
		return nil, e
	}
	base := catalogSite(s)
	result := map[string]sourceSite{}
	for _, key := range []string{"PageLabels", "OutputIntents", "OpenAction", "AA", "AF"} {
		result[key] = base.key(key)
	}
	for _, item := range []struct {
		parent   string
		children []string
	}{{"Names", []string{"JavaScript", "EmbeddedFiles"}}, {"AcroForm", []string{"XFA", "NeedAppearances", "Fields"}}, {"URI", []string{"Base"}}} {
		site := base.key(item.parent).anchor(cat[item.parent], origins)
		for _, key := range item.children {
			result[item.parent+"."+key] = site.key(key)
		}
	}
	return result, nil
}
func (o sourceOrigins) catalogEntry(key string, fallback sourceSite) sourceSite {
	if site, ok := o.catalog[key]; ok {
		return site
	}
	return fallback
}

func (s sourceSite) wrap(err error) error {
	if err == nil {
		return nil
	}
	if loc := s.location(); loc != nil {
		return structureAt(err, *loc)
	}
	return err
}
