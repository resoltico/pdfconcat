// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

func TestAssemblePolicyDropsDocumentLevelMetadata(t *testing.T) {
	t.Parallel()

	env := newWorld(t)

	_, doc := env.assemble(t, []part{whole(fixtureTitled), whole(fixtureOutline), whole(fixtureTagged), whole(fixtureAttached)})

	// pdfcpu writes its own /Info with a producer and dates; nothing of the sources' own survives.
	for _, key := range doc.InfoKeys() {
		if key != "/Producer" && key != "/CreationDate" && key != "/ModDate" {
			t.Errorf("the trailer /Info keeps %s from a source", key)
		}
	}

	if doc.Attachments() != 0 {
		t.Error("the output keeps attachments")
	}
}

func TestAssembleRejectsSourcesThatNeedWholeUse(t *testing.T) {
	t.Parallel()

	env := newWorld(t)

	for _, name := range []string{fixtureLinks, fixtureNamed, fixtureForm, fixtureActions, fixtureLegacy} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := env.compile(filepath.Join(t.TempDir(), outputFilename), []part{whole(fixturePlainA), pages(name, 2, 1)})

			err := env.engine.Assemble(context.Background(), &c.request)
			failure := requireFailure(t, err, pdfengine.CodePartialRange)

			if failure.Source != 1 || failure.Path != env.files[name].Path {
				t.Errorf("error names source %d %q, want 1 %q", failure.Source, failure.Path, env.files[name].Path)
			}

			if _, statErr := os.Stat(c.request.Destination); statErr == nil {
				t.Error("a rejected request still wrote the destination")
			}
		})
	}
}

func TestAssembleRejectsRepeatedLegacyDests(t *testing.T) {
	t.Parallel()

	env := newWorld(t)

	for name, parts := range map[string][]part{
		"same source twice":    {whole(fixtureLegacy), whole(fixturePlainA), whole(fixtureLegacy)},
		"two legacy documents": {whole(fixtureLegacy), whole("legacy2")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := env.compile(filepath.Join(t.TempDir(), outputFilename), parts)

			err := env.engine.Assemble(context.Background(), &c.request)
			requireCode(t, err, pdfengine.CodeLegacyDestsRepeated)
		})
	}
}

func TestAssembleRejectsInconsistentRequests(t *testing.T) {
	t.Parallel()

	env := newWorld(t)

	valid := func(t *testing.T) pdfengine.AssembleRequest {
		t.Helper()

		return env.compile(filepath.Join(t.TempDir(), outputFilename), []part{whole(fixturePlainA), gen(0, 2)}).request
	}

	cases := map[string]func(r *pdfengine.AssembleRequest){
		"no destination":             func(r *pdfengine.AssembleRequest) { r.Destination = "" },
		"empty order":                func(r *pdfengine.AssembleRequest) { r.Order = nil; r.ExpectedPages = 0 },
		"zero count":                 func(r *pdfengine.AssembleRequest) { r.Order[0].Count = 0 },
		"negative count":             func(r *pdfengine.AssembleRequest) { r.Order[1].Count = -1 },
		"generated without resource": func(r *pdfengine.AssembleRequest) { r.Resource = nil },
		"resource page out of range": func(r *pdfengine.AssembleRequest) { r.Order[1].Index = resourcePages },
		"negative resource page":     func(r *pdfengine.AssembleRequest) { r.Order[1].Index = -1 },
		"source out of range":        func(r *pdfengine.AssembleRequest) { r.Order[0].Index = 1 },
		"negative source":            func(r *pdfengine.AssembleRequest) { r.Order[0].Index = -1 },
		"start before first page":    func(r *pdfengine.AssembleRequest) { r.Order[0].Start = 0 },
		"start after last page":      func(r *pdfengine.AssembleRequest) { r.Order[0].Start = 2 },
		"range past the end":         func(r *pdfengine.AssembleRequest) { r.Order[0].Count = 2; r.ExpectedPages = 4 },
		"expected total mismatch":    func(r *pdfengine.AssembleRequest) { r.ExpectedPages++ },
		"total exceeds the limit": func(r *pdfengine.AssembleRequest) {
			r.Order = []pdfengine.Run{pdfengine.GeneratedPages(0, pdfengine.MaxOutputPages), pdfengine.GeneratedPages(0, 1)}
			r.ExpectedPages = pdfengine.MaxOutputPages + 1
		},
		"total would overflow an int": func(r *pdfengine.AssembleRequest) {
			r.Order = []pdfengine.Run{pdfengine.GeneratedPages(0, math.MaxInt), pdfengine.GeneratedPages(0, math.MaxInt)}
			r.ExpectedPages = -2
		},
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			request := valid(t)
			mutate(&request)

			// A rejected request fails before any file is read or any context check, so a request that is
			// wrongly accepted meets the canceled context at once instead of assembling millions of pages.
			err := env.engine.Assemble(canceledContext(), &request)
			requireCode(t, err, pdfengine.CodeRequestInvalid)

			if request.Destination != "" {
				if _, statErr := os.Stat(request.Destination); statErr == nil {
					t.Error("a rejected request wrote the destination")
				}
			}
		})
	}
}

// TestAssembleAcceptsTheLimitItself checks the bound is inclusive: a request of exactly MaxOutputPages
// fails only on the expected-total comparison, not on the limit.
func TestAssembleAcceptsTheLimitItself(t *testing.T) {
	t.Parallel()

	env := newWorld(t)
	request := env.compile(filepath.Join(t.TempDir(), outputFilename), []part{gen(0, 1)}).request
	request.Order = []pdfengine.Run{pdfengine.GeneratedPages(0, pdfengine.MaxOutputPages)}
	request.ExpectedPages = pdfengine.MaxOutputPages - 1

	failure := requireFailure(t, env.engine.Assemble(canceledContext(), &request), pdfengine.CodeRequestInvalid)
	if !strings.Contains(failure.Error(), "expected") {
		t.Fatalf("rejected for the wrong reason: %v", failure)
	}
}

// runWorldChecks runs each check as a parallel subtest over one shared world.
func runWorldChecks(t *testing.T, env *world, checks map[string]worldCheck) {
	t.Helper()

	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			check(t, env)
		})
	}
}

func TestAssembleReportsSourceFailures(t *testing.T) {
	t.Parallel()

	runWorldChecks(t, newWorld(t), map[string]worldCheck{
		"source missing":                    checkSourceMissing,
		"source no longer a PDF":            checkSourceNoLongerPDF,
		"source encrypted after inspection": checkSourceEncryptedLater,
		"source changed page count":         checkSourceChangedPageCount,
		"resource unreadable":               checkResourceUnreadable,
		"resource page count differs":       checkResourcePageCountDiffers,
		"destination cannot be created":     checkDestinationNotCreatable,
	})
}

func checkSourceMissing(t *testing.T, env *world) {
	t.Helper()

	c := env.compile(filepath.Join(t.TempDir(), outputFilename), []part{whole(fixturePlainA), whole(fixturePlainB)})
	c.request.Sources[1].Path = filepath.Join(t.TempDir(), absentFilename)

	failure := requireFailure(t, env.engine.Assemble(context.Background(), &c.request), pdfengine.CodeUnreadable)
	if failure.Source != 1 {
		t.Errorf("source %d, want 1", failure.Source)
	}
}

func checkSourceNoLongerPDF(t *testing.T, env *world) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "broken.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.7 garbage"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := env.compile(filepath.Join(t.TempDir(), outputFilename), []part{whole(fixturePlainA), whole(fixturePlainB)})
	c.request.Sources[0].Path = path

	failure := requireFailure(t, env.engine.Assemble(context.Background(), &c.request), pdfengine.CodeInvalid)
	if failure.Source != 0 || failure.Path != path {
		t.Errorf("error names source %d %q", failure.Source, failure.Path)
	}
}

func checkSourceEncryptedLater(t *testing.T, env *world) {
	t.Helper()

	path := writeDoc(t, t.TempDir(), "later-encrypted", pdffixture.Plain("LATER"))

	info, err := env.engine.Inspect(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	encrypt(t, env.tools, path, "")

	request := pdfengine.AssembleRequest{
		Sources: []pdfengine.SourceFile{{Path: path, Info: info}}, Order: []pdfengine.Run{pdfengine.SourcePages(0, 1, 1)},
		Destination: filepath.Join(t.TempDir(), outputFilename), ExpectedPages: 1,
	}

	requireCode(t, env.engine.Assemble(context.Background(), &request), pdfengine.CodeEncrypted)
}

func checkSourceChangedPageCount(t *testing.T, env *world) {
	t.Helper()

	c := env.compile(filepath.Join(t.TempDir(), outputFilename), []part{whole(fixturePlainA)})
	c.request.Sources[0].Info.Pages = 3
	c.request.Order = []pdfengine.Run{pdfengine.SourcePages(0, 1, 3)}
	c.request.ExpectedPages = 3

	failure := requireFailure(t, env.engine.Assemble(context.Background(), &c.request), pdfengine.CodeInvalid)
	if failure.Source != 0 {
		t.Errorf("source %d, want 0", failure.Source)
	}
}

func checkResourceUnreadable(t *testing.T, env *world) {
	t.Helper()

	c := env.compile(filepath.Join(t.TempDir(), outputFilename), []part{whole(fixturePlainA), gen(0, 1)})
	c.request.Resource.Path = filepath.Join(t.TempDir(), absentFilename)

	failure := requireFailure(t, env.engine.Assemble(context.Background(), &c.request), pdfengine.CodeUnreadable)
	if failure.Source != pdfengine.NoSource || failure.Path != c.request.Resource.Path {
		t.Errorf("error names source %d %q", failure.Source, failure.Path)
	}
}

func checkResourcePageCountDiffers(t *testing.T, env *world) {
	t.Helper()

	c := env.compile(filepath.Join(t.TempDir(), outputFilename), []part{gen(0, 1)})
	c.request.Resource.Pages = resourcePages + 1

	requireCode(t, env.engine.Assemble(context.Background(), &c.request), pdfengine.CodeInvalid)
}

func checkDestinationNotCreatable(t *testing.T, env *world) {
	t.Helper()

	c := env.compile(filepath.Join(t.TempDir(), "missing-directory", outputFilename), []part{whole(fixturePlainA)})

	requireCode(t, env.engine.Assemble(context.Background(), &c.request), pdfengine.CodeAssemblyFailed)
}

// TestAssembleOverwritesStaleDestination checks that the staged file is created or truncated: a longer
// stale file must not leave trailing bytes.
func TestAssembleOverwritesStaleDestination(t *testing.T) {
	t.Parallel()

	env := newWorld(t)
	destination := filepath.Join(t.TempDir(), outputFilename)

	if err := os.WriteFile(destination, make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}

	c := env.compile(destination, []part{whole(fixturePlainA)})
	if err := env.engine.Assemble(context.Background(), &c.request); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Stat(destination); err != nil || info.Size() >= 1<<20 {
		t.Fatalf("destination not truncated: %v %v", info, err)
	}
}

// TestNegativeControlsAreDetected proves the oracle is not vacuous: each deliberately wrong expectation
// or deliberately corrupted output must be reported by the matching check.
func TestNegativeControlsAreDetected(t *testing.T) {
	t.Parallel()

	runWorldChecks(t, newWorld(t), map[string]worldCheck{
		"swapped pages":                                detectSwappedPages,
		"removed source":                               detectRemovedSource,
		"wrong geometry":                               detectWrongGeometry,
		"missing crop is not accepted":                 detectMissingCrop,
		"wrong occurrence":                             detectWrongOccurrence,
		"wrong link target page":                       detectWrongLinkPage,
		"version requirement above output":             detectVersionAboveOutput,
		"page collection of repeated links is corrupt": detectCollectedLinksCorrupt,
	})
}

func detectSwappedPages(t *testing.T, env *world) {
	t.Helper()

	c, doc := env.assemble(t, []part{whole(fixturePlainA), whole(fixturePlainB), gen(0, 1)})
	c.expectation.Pages[0], c.expectation.Pages[1] = c.expectation.Pages[1], c.expectation.Pages[0]

	requireFailed(t, doc.Verify(c.expectation), pdforacle.CheckPageText)
}

func detectRemovedSource(t *testing.T, env *world) {
	t.Helper()

	c, doc := env.assemble(t, []part{whole(fixturePlainA), whole(fixturePlainB), gen(0, 1)})
	c.expectation.Pages = append(c.expectation.Pages[:1], c.expectation.Pages[2:]...)

	failed := pdforacle.Failed(doc.Verify(c.expectation))
	requireContains(t, failed, pdforacle.CheckPageCount)
	requireContains(t, failed, pdforacle.CheckPageText)
}

func detectWrongGeometry(t *testing.T, env *world) {
	t.Helper()

	c, doc := env.assemble(t, []part{whole(fixtureCrop)})
	fact := c.expectation.Sources[fixtureCrop]
	fact.Geometry = uniformGeometry(2, letterGeometry())
	c.expectation.Sources[fixtureCrop] = fact

	requireFailed(t, doc.Verify(c.expectation), pdforacle.CheckGeometry)
}

func detectMissingCrop(t *testing.T, env *world) {
	t.Helper()

	c, doc := env.assemble(t, []part{whole(fixturePlainA)})
	fact := c.expectation.Sources[fixturePlainA]
	fact.Geometry = map[int]pdforacle.Geometry{1: {MediaBox: []float64{0, 0, 612, 792}, CropBox: []float64{1, 1, 5, 5}}}
	c.expectation.Sources[fixturePlainA] = fact

	requireFailed(t, doc.Verify(c.expectation), pdforacle.CheckGeometry)
}

func detectWrongOccurrence(t *testing.T, env *world) {
	t.Helper()

	c, doc := env.assemble(t, []part{whole(fixtureLinks), whole(fixtureLinks)})
	c.expectation.Pages[2].Occurrence = 1

	requireFailed(t, doc.Verify(c.expectation), pdforacle.CheckLinkOccurrence)
}

func detectWrongLinkPage(t *testing.T, env *world) {
	t.Helper()

	c, doc := env.assemble(t, []part{whole(fixtureLinks)})
	fact := c.expectation.Sources[fixtureLinks]
	fact.Links = map[int][]int{1: {1}, 2: {2}}
	c.expectation.Sources[fixtureLinks] = fact

	requireFailed(t, doc.Verify(c.expectation), pdforacle.CheckLinkPage)
}

func detectVersionAboveOutput(t *testing.T, env *world) {
	t.Helper()

	c, doc := env.assemble(t, []part{whole(fixturePlainA)})
	fact := c.expectation.Sources[fixturePlainA]
	fact.Version = 20
	c.expectation.Sources[fixturePlainA] = fact

	requireFailed(t, doc.Verify(c.expectation), pdforacle.CheckVersion)
}

// detectCollectedLinksCorrupt: pdfcpu's own page-collection primitive was rejected for this design because
// it breaks repeated linked pages. Its output must therefore fail the oracle, which is what makes the
// matrix a real adoption test and not a rubber stamp.
func detectCollectedLinksCorrupt(t *testing.T, env *world) {
	t.Helper()

	collected := filepath.Join(t.TempDir(), "collected.pdf")

	conf, err := api.LoadConfiguration(api.ConfigurationOptions{Mode: api.ConfigurationModeStateless})
	if err != nil {
		t.Fatal(err)
	}

	conf.Offline = true

	err = api.CollectFile(
		context.Background(),
		env.files[fixtureLinks].Path,
		collected,
		[]string{"1", "2", "1", "2"},
		conf,
	)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	doc, err := pdforacle.Load(env.tools, collected)
	if err != nil {
		t.Fatal(err)
	}

	fact := fixtureCatalog()[fixtureLinks].fact
	exp := pdforacle.Expectation{Sources: map[string]pdforacle.SourceFact{fixtureLinks: fact}}

	for occurrence := 1; occurrence <= 2; occurrence++ {
		for page := 1; page <= 2; page++ {
			exp.Pages = append(exp.Pages, pdforacle.ExpectedPage{
				Text: numbered(linkedPageMarker)(page), Source: fixtureLinks, Occurrence: occurrence, SourcePage: page,
			})
		}
	}

	if findings := doc.Verify(exp); len(findings) == 0 {
		t.Fatal("the oracle accepted a page collection that corrupts repeated links")
	}
}

func requireFailed(t *testing.T, findings []pdforacle.Finding, check string) {
	t.Helper()

	requireContains(t, pdforacle.Failed(findings), check)
}

func requireContains(t *testing.T, checks []string, check string) {
	t.Helper()

	if slices.Contains(checks, check) {
		return
	}

	t.Fatalf("check %q did not fail; failed checks: %v", check, checks)
}

// TestFixturesCarryWhatThePolicyDrops keeps the "dropped" assertions honest: the inputs really have
// the outlines, structure tree, attachments and information that the output must lack.
func TestFixturesCarryWhatThePolicyDrops(t *testing.T) {
	t.Parallel()

	env := newWorld(t)

	load := func(name string) *pdforacle.Document {
		doc, err := pdforacle.Load(env.tools, env.files[name].Path)
		if err != nil {
			t.Fatal(err)
		}

		return doc
	}

	if keys := load(fixtureTitled).InfoKeys(); !slices.Contains(keys, "/Title") {
		t.Errorf("titled fixture /Info keys %v", keys)
	}

	if keys := load(fixtureOutline).CatalogKeys(); !slices.Contains(keys, "/Outlines") {
		t.Errorf("outline fixture catalog keys %v", keys)
	}

	if keys := load(fixtureTagged).CatalogKeys(); !slices.Contains(keys, "/StructTreeRoot") {
		t.Errorf("tagged fixture catalog keys %v", keys)
	}

	if load(fixtureAttached).Attachments() != 1 {
		t.Error("attach fixture has no attachment")
	}
}
