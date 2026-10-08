// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

type (
	// part is one element of a scenario's order, written in terms of fixture names.
	part struct {
		source       string // fixture name, or "" for generated pages
		start, count int    // source pages (1-based start)
		spec, repeat int    // generated page index and repetitions
	}

	// world is a directory of written fixtures with their inspected facts.
	world struct {
		engine   *pdfengine.Engine
		tools    pdforacle.Tools
		catalog  map[string]fixture
		files    map[string]pdfengine.SourceFile
		resource string
		dir      string
	}

	// compiled is a scenario turned into an engine request and the oracle's expectation.
	compiled struct {
		expectation pdforacle.Expectation
		request     pdfengine.AssembleRequest
	}

	// worldCheck is one scenario run against a shared world.
	worldCheck func(t *testing.T, env *world)
)

func whole(source string) part { return part{source: source, start: 0} }
func pages(source string, start, count int) part {
	return part{source: source, start: start, count: count}
}
func gen(spec, repeat int) part { return part{spec: spec, repeat: repeat} }

func newWorld(t *testing.T) *world {
	t.Helper()

	env := &world{
		engine:  newEngine(t),
		tools:   pdforacle.RequireTools(t),
		catalog: fixtureCatalog(),
		files:   map[string]pdfengine.SourceFile{},
		dir:     t.TempDir(),
	}

	paths, resource := writeCatalog(t, env.dir)
	env.resource = resource

	for name, path := range paths {
		info, err := env.engine.Inspect(context.Background(), path)
		if err != nil {
			t.Fatalf("inspect %s: %v", name, err)
		}

		if want := env.catalog[name].fact.Pages; info.Pages != want {
			t.Fatalf("fixture %s has %d pages, catalog says %d", name, info.Pages, want)
		}

		env.files[name] = pdfengine.SourceFile{Path: path, Info: info}
	}

	return env
}

// compile builds the request and the independent expectation for parts. Sources are indexed in order of
// first use; the resource is attached only when a generated run exists.
func (w *world) compile(destination string, parts []part) compiled {
	var result compiled

	index := map[string]int{}
	occurrences := map[string]int{}
	result.expectation.Sources = map[string]pdforacle.SourceFact{}

	for _, step := range parts {
		if step.source == "" {
			result.request.Resource = &pdfengine.ResourceDocument{Path: w.resource, Pages: resourcePages}
			result.request.Order = append(result.request.Order, pdfengine.GeneratedPages(step.spec, step.repeat))

			geometry := generatedGeometry(step.spec)
			for range step.repeat {
				result.expectation.Pages = append(
					result.expectation.Pages,
					pdforacle.ExpectedPage{Text: generatedLabel(step.spec), SourcePage: step.spec, Geometry: &geometry},
				)
			}

			continue
		}

		entry := w.catalog[step.source]

		position, known := index[step.source]
		if !known {
			position = len(result.request.Sources)
			index[step.source] = position
			result.request.Sources = append(result.request.Sources, w.files[step.source])
		}

		start, count := step.start, step.count
		if start == 0 {
			start, count = 1, entry.fact.Pages
		}

		result.request.Order = append(result.request.Order, pdfengine.SourcePages(position, start, count))
		result.expectation.Sources[step.source] = entry.fact
		occurrences[step.source]++

		for offset := range count {
			result.expectation.Pages = append(result.expectation.Pages, pdforacle.ExpectedPage{
				Text: entry.texts(start + offset), Source: step.source, Occurrence: occurrences[step.source], SourcePage: start + offset,
			})
		}
	}

	result.request.Destination = destination
	result.request.ExpectedPages = len(result.expectation.Pages)

	return result
}

// assemble runs a scenario and loads the output into the oracle.
func (w *world) assemble(t *testing.T, parts []part) (compiled, *pdforacle.Document) {
	t.Helper()

	c := w.compile(filepath.Join(t.TempDir(), outputFilename), parts)

	if err := w.engine.Assemble(context.Background(), &c.request); err != nil {
		t.Fatalf("assemble: %v", err)
	}

	doc, err := pdforacle.Load(w.tools, c.request.Destination)
	if err != nil {
		t.Fatal(err)
	}

	return c, doc
}

// requireClean fails with every finding when the oracle objects to the output.
func requireClean(t *testing.T, doc *pdforacle.Document, exp pdforacle.Expectation) {
	t.Helper()

	for _, finding := range doc.Verify(exp) {
		t.Errorf("oracle: %s: %s", finding.Check, finding.Detail)
	}
}
