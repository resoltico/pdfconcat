// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package scale

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

type (
	// Workload is one scale case: the plan the executable reads, the files it refers to, and everything an
	// independent checker needs to judge the output without trusting the executable.
	Workload struct {
		// Name identifies the case in results.
		Name string
		// Dir is the directory holding the plan's sources; the executable runs with it as working directory.
		Dir string
		// Plan is the plan JSON.
		Plan []byte
		// Expectation is what the oracle must find in the output.
		Expectation pdforacle.Expectation
		// Appearances lists generated-page appearances to verify by rendering a sample page of each.
		Appearances []Appearance
		// VisualSources samples original image-bearing source pages in the assembled output.
		VisualSources []VisualSource
		// FullTexts verifies long text after whitespace normalization of independent extraction.
		FullTexts map[int]string
		files     []fixtureFile
		// Pages is the expected number of output pages.
		Pages int
		// SourceFiles and InputBytes describe the input corpus, filled in by Write.
		SourceFiles int
		InputBytes  int64
	}

	// Appearance is a rendered check of one generated page: the pixel at (X, Y) must be Color, within
	// a small tolerance, on output page Page (1-based).
	Appearance struct {
		Page  int
		X, Y  int
		Color [3]uint8
	}

	// VisualSource binds an original source raster to its output occurrence.
	VisualSource struct {
		Path                   string
		SourcePage, OutputPage int
	}

	// BlankStyles says how the generated pages of a light workload are styled.
	BlankStyles int

	fixtureFile struct {
		doc  *pdffixture.Doc
		path string // relative to Workload.Dir
	}

	// planBuilder accumulates plan items and the matching expectation.
	planBuilder struct {
		workload *Workload
		occur    map[string]int
		items    []any
	}
)

const (
	longTextPages    = 3000
	longTextPairs    = 4995
	longTextFontSize = 6
	// SharedBlankStyle gives every generated page of a light workload the same text and style.
	SharedBlankStyle BlankStyles = 0
	// DistinctBlankStyles gives every generated page of a light workload its own text.
	DistinctBlankStyles BlankStyles = 1

	// fixtureDirectoryPermissions is the mode of directories holding fixtures.
	fixtureDirectoryPermissions = 0o700
	// fileMode is the mode of files the harness writes.
	fileMode = 0o600
	// sourcePDFVersion is the version of every fixture source as the oracle encodes it: 17 is PDF 1.7.
	sourcePDFVersion = 17
	// sourceDir is the plan's base directory for sources, relative to the working directory.
	sourceDir = "src"
	// outputDir is the directory the executable writes into, relative to the working directory.
	outputDir = "out"
	// planFile is the plan's name, relative to the working directory.
	planFile = "job.json"

	// Mixed workload dimensions. Each of mixedRounds rounds visits every group; a group holds
	// mixedPlainPerGroup one-page and mixedLinkedPerGroup two-page linked sources. Blank counts are
	// chosen so the total is exactly MixedPages.
	mixedGroups         = 10
	mixedRounds         = 2
	mixedPlainPerGroup  = 400
	mixedLinkedPerGroup = 100
	mixedLeading        = 3
	mixedSeparator      = 149
	mixedTrailing       = 17
	// MixedPages is the output size of MixedWorkload.
	MixedPages = 15000

	// Heavy workload dimensions: image-rich one-page documents, multi-page documents, and generated pages.
	heavyImageSources    = 200
	heavyImageBytes      = 512 << 10
	heavyMultiSources    = 40
	heavyPagesPerMulti   = 60
	heavyBlanksPerSource = 1
)

// Stage writes the fixtures, the plan as the working directory's job.json and an empty output
// directory, so the executable can run with w.Dir as its working directory.
func (w *Workload) Stage() error {
	err := os.MkdirAll(filepath.Join(w.Dir, outputDir), fixtureDirectoryPermissions)
	if err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	err = w.Write()
	if err != nil {
		return err
	}

	return WriteFile(filepath.Join(w.Dir, planFile), w.Plan)
}

// LongTextWorkload is 3,000 distinct 10,000-character pages, a shaped-text lifetime stress case.
func LongTextWorkload(dir string) (*Workload, error) {
	builder := newPlanBuilder("long-text-3000", dir)
	builder.workload.FullTexts = make(map[int]string, longTextPages)

	for index := range longTextPages {
		marker := fmt.Sprintf("LONG %04d", index)
		text := marker + "\n" + strings.Repeat("a ", longTextPairs)
		builder.items = append(
			builder.items,
			map[string]any{"blank": map[string]any{"size": "612x792", "text": map[string]any{"value": text, "size": longTextFontSize}}},
		)
		geometry := letter()
		builder.workload.Expectation.Pages = append(
			builder.workload.Expectation.Pages,
			pdforacle.ExpectedPage{Text: marker, Geometry: &geometry},
		)
		builder.workload.Pages++
		builder.workload.FullTexts[index+1] = text
	}

	return builder.finish("")
}

// Write creates the fixture files under w.Dir and fills in SourceFiles and InputBytes.
func (w *Workload) Write() error {
	for _, file := range w.files {
		path := filepath.Join(w.Dir, file.path)

		err := os.MkdirAll(filepath.Dir(path), fixtureDirectoryPermissions)
		if err != nil {
			return fmt.Errorf("create fixture directory: %w", err)
		}

		err = file.doc.WriteFile(path)
		if err != nil {
			return fmt.Errorf("write fixture: %w", err)
		}

		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat fixture: %w", err)
		}

		w.InputBytes += info.Size()
	}

	w.SourceFiles = len(w.files)

	return nil
}

// FixtureSignature is a digest-free fingerprint of the fixture set for determinism tests: the sorted
// relative paths with byte sizes.
func (w *Workload) FixtureSignature() []string {
	signature := make([]string, len(w.files))
	for index, file := range w.files {
		signature[index] = fmt.Sprintf("%s:%d", file.path, len(file.doc.Bytes()))
	}

	return signature
}

// letter is the geometry of a US Letter page.
func letter() pdforacle.Geometry {
	return pdforacle.Geometry{MediaBox: []float64{0, 0, 612, 792}}
}

func plainFact() pdforacle.SourceFact {
	return pdforacle.SourceFact{Pages: 1, Version: sourcePDFVersion, Geometry: map[int]pdforacle.Geometry{1: letter()}}
}

func linkedFact() pdforacle.SourceFact {
	return pdforacle.SourceFact{
		Pages: 2, Version: sourcePDFVersion, Links: map[int][]int{1: {2}, 2: {1}},
		Geometry: map[int]pdforacle.Geometry{1: letter(), 2: letter()},
	}
}

func newPlanBuilder(name, dir string) *planBuilder {
	expectation := pdforacle.Expectation{Sources: map[string]pdforacle.SourceFact{}}

	return &planBuilder{workload: &Workload{Name: name, Dir: dir, Expectation: expectation}, occur: map[string]int{}}
}

// addFile registers a fixture source at path, relative to the workload's directory.
func (b *planBuilder) addFile(path string, doc *pdffixture.Doc) {
	b.workload.files = append(b.workload.files, fixtureFile{doc: doc, path: path})
}

// sourcePages records the expected pages of one use of a source and returns nothing: the caller adds
// the plan item. fact describes the source; text names the marker of each page.
func (b *planBuilder) sourcePages(name string, fact pdforacle.SourceFact, text func(page int) string) {
	b.workload.Expectation.Sources[name] = fact
	b.occur[name]++

	for page := 1; page <= fact.Pages; page++ {
		b.workload.Expectation.Pages = append(b.workload.Expectation.Pages, pdforacle.ExpectedPage{
			Text: text(page), Source: name, Occurrence: b.occur[name], SourcePage: page,
		})
	}

	b.workload.Pages += fact.Pages
}

// blank adds a generated blank item of count pages with the given text and optional background.
func (b *planBuilder) blank(items *[]any, value, background string, count int) {
	spec := map[string]any{"text": map[string]any{"value": value}}
	if background != "" {
		spec["background"] = background
	}

	item := map[string]any{"blank": spec}
	if count != 1 {
		item["count"] = count
	}

	*items = append(*items, item)

	for range count {
		geometry := letter()
		b.workload.Expectation.Pages = append(
			b.workload.Expectation.Pages,
			pdforacle.ExpectedPage{Text: value, SourcePage: 0, Geometry: &geometry},
		)
	}

	b.workload.Pages += count
}

func (b *planBuilder) finish(dir string) (*Workload, error) {
	plan := map[string]any{"version": 1, "items": b.items}
	if dir != "" {
		plan["dir"] = dir
	}

	encoded, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("encode plan: %w", err)
	}

	b.workload.Plan = encoded

	return b.workload, nil
}

// LightWorkload is count distinct one-page sources interleaved with count generated pages. With
// DistinctBlankStyles every generated page has its own text; otherwise all share one text and style.
func LightWorkload(dir string, count int, styles BlankStyles) (*Workload, error) {
	style := "repeated-style"
	if styles == DistinctBlankStyles {
		style = "distinct-style"
	}

	plan := newPlanBuilder(fmt.Sprintf("light-%d-%s", count, style), dir)

	for index := range count {
		name := fmt.Sprintf("s%05d", index)
		marker := fmt.Sprintf("S%05d", index)

		plan.addFile(sourceDir+"/"+name+".pdf", pdffixture.Plain(marker))
		plan.items = append(plan.items, name+".pdf")
		plan.sourcePages(name, plainFact(), func(int) string { return marker })

		text := "BLANK"
		if styles == DistinctBlankStyles {
			text = fmt.Sprintf("BLANK %05d", index)
		}

		plan.blank(&plan.items, text, "", 1)
	}

	return plan.finish(sourceDir)
}

// MixedWorkload is the 15,000-page case: repeated source occurrences (every source is used once per
// round, rounds visit the groups in opposite directions), leading and trailing blanks, grouped
// directories, compact blank counts with distinct text and backgrounds, and linked sources whose
// links must stay inside their own occurrence.
func MixedWorkload(dir string) (*Workload, error) {
	plan := newPlanBuilder("mixed-15000", dir)

	for group := range mixedGroups {
		for index := range mixedPlainPerGroup {
			marker := fmt.Sprintf("P%d-%03d", group, index)
			plan.addFile(fmt.Sprintf("%s/g%02d/p%03d.pdf", sourceDir, group, index), pdffixture.Plain(marker))
		}

		for index := range mixedLinkedPerGroup {
			marker := fmt.Sprintf("K%d-%03d", group, index)
			plan.addFile(fmt.Sprintf("%s/g%02d/k%03d.pdf", sourceDir, group, index), pdffixture.Links(marker))
		}
	}

	plan.blank(&plan.items, "LEAD", "#FFF2CC", mixedLeading)
	plan.appearance(len(plan.workload.Expectation.Pages), [3]uint8{0xFF, 0xF2, 0xCC})

	for round := range mixedRounds {
		for step := range mixedGroups {
			group := step
			if round%2 == 1 {
				group = mixedGroups - 1 - step
			}

			plan.addMixedGroup(round, step, group)
		}
	}

	plan.blank(&plan.items, "TRAILING", "", mixedTrailing)

	return plan.finish(sourceDir)
}

// addMixedGroup adds one visit to a group: its sources, then a separator of generated pages.
func (b *planBuilder) addMixedGroup(round, step, group int) {
	var groupItems []any

	for index := range mixedPlainPerGroup {
		name := fmt.Sprintf("g%02d/p%03d", group, index)
		marker := fmt.Sprintf("P%d-%03d", group, index)

		groupItems = append(groupItems, fmt.Sprintf("p%03d.pdf", index))

		b.sourcePages(name, plainFact(), func(int) string { return marker })

		if index < mixedLinkedPerGroup {
			linked := fmt.Sprintf("g%02d/k%03d", group, index)
			linkedMarker := fmt.Sprintf("K%d-%03d", group, index)

			groupItems = append(groupItems, fmt.Sprintf("k%03d.pdf", index))

			b.sourcePages(linked, linkedFact(), func(page int) string { return fmt.Sprintf("%s p%d", linkedMarker, page) })
		}
	}

	b.items = append(b.items, map[string]any{"dir": fmt.Sprintf("g%02d", group), "items": groupItems})

	background := "#DDEEFF"
	color := [3]uint8{0xDD, 0xEE, 0xFF}

	if step%2 == 1 {
		background, color = "#E2F0D9", [3]uint8{0xE2, 0xF0, 0xD9}
	}

	b.blank(&b.items, fmt.Sprintf("SEPARATOR round %d group %d", round+1, group), background, mixedSeparator)
	b.appearance(len(b.workload.Expectation.Pages), color)
}

// appearance records a rendered check of the pixel near the top-left corner of the last page so far.
func (b *planBuilder) appearance(lastPage int, color [3]uint8) {
	b.workload.Appearances = append(b.workload.Appearances, Appearance{Page: lastPage, X: 2, Y: 2, Color: color})
}

// HeavyWorkload is a resource-heavy corpus at lower counts: image-rich pages of incompressible data and
// multi-page documents, interleaved with generated pages.
func HeavyWorkload(dir string) (*Workload, error) {
	plan := newPlanBuilder("resource-heavy", dir)

	for index := range max(heavyImageSources, heavyMultiSources) {
		if index < heavyImageSources {
			plan.addImageSource(index)
		}

		if index < heavyMultiSources {
			plan.addMultiPageSource(index)
		}
	}

	return plan.finish(sourceDir)
}

// addImageSource adds one image-rich one-page source followed by its generated page.
func (b *planBuilder) addImageSource(index int) {
	name := fmt.Sprintf("img%04d", index)
	marker := fmt.Sprintf("H%04d", index)

	b.addFile(sourceDir+"/"+name+".pdf", pdffixture.ImageRich(marker, heavyImageBytes))
	b.items = append(b.items, name+".pdf")
	b.sourcePages(name, plainFact(), func(int) string { return marker })
	b.blank(&b.items, fmt.Sprintf("HEAVY BLANK %04d", index), "", heavyBlanksPerSource)

	if index == 0 || index == heavyImageSources/2 || index == heavyImageSources-1 {
		b.workload.VisualSources = append(b.workload.VisualSources, VisualSource{
			Path: filepath.Join(b.workload.Dir, sourceDir, name+".pdf"), SourcePage: 1,
			OutputPage: b.workload.Pages - heavyBlanksPerSource,
		})
	}
}

// addMultiPageSource adds one multi-page source.
func (b *planBuilder) addMultiPageSource(index int) {
	name := fmt.Sprintf("multi%03d", index)
	tag := fmt.Sprintf("M%03d", index)
	fact := pdforacle.SourceFact{Pages: heavyPagesPerMulti, Version: sourcePDFVersion, Geometry: map[int]pdforacle.Geometry{}}

	for page := 1; page <= heavyPagesPerMulti; page++ {
		fact.Geometry[page] = letter()
	}

	b.addFile(sourceDir+"/"+name+".pdf", pdffixture.Pages(tag, heavyPagesPerMulti))
	b.items = append(b.items, name+".pdf")
	b.sourcePages(name, fact, func(page int) string { return fmt.Sprintf("%s p%d", tag, page) })
}
