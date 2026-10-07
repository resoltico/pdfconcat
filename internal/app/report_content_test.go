// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdforacle"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

type (
	// savedBounds is the text block of a style in a saved report.
	savedBounds struct {
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}

	// savedText is the text of a style in a saved report.
	savedText struct {
		Value  string      `json:"value"`
		Color  string      `json:"color"`
		Bounds savedBounds `json:"bounds"`
	}

	// savedSize is how a generated page got its size.
	savedSize struct {
		Origin string  `json:"origin"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}

	// savedStyle is a style of a saved report, with only the members the tests read.
	savedStyle struct {
		Text       *savedText `json:"text"`
		Background string     `json:"background"`
		Size       savedSize  `json:"size"`
	}

	// savedSource is a source of a saved report.
	savedSource struct {
		Bytes  *int64 `json:"bytes"`
		Path   string `json:"path"`
		Digest string `json:"digest"`
	}

	// savedFont is a font of a saved report.
	savedFont struct {
		Digest string `json:"digest"`
		Name   string `json:"name"`
		File   string `json:"file"`
	}

	// savedReport is the part of a saved report the tests read.
	savedReport struct {
		Status  string        `json:"status"`
		Sources []savedSource `json:"sources"`
		Fonts   []savedFont   `json:"fonts"`
		Styles  []savedStyle  `json:"styles"`
	}
)

// loadReport reads the saved report at path.
func loadReport(tb testing.TB, path string) savedReport {
	tb.Helper()

	var saved savedReport

	err := json.Unmarshal([]byte(readFile(tb, path)), &saved)
	if err != nil {
		tb.Fatal(err)
	}

	return saved
}

// blank is a plan item for a generated page with the given members.
func blank(members map[string]any) map[string]any {
	return map[string]any{"blank": members}
}

// requirePixel checks the color of one pixel of a page, as an independent renderer draws it.
func requirePixel(tb testing.TB, path string, page int, want [3]uint8) {
	tb.Helper()

	doc, err := pdforacle.Load(pdforacle.RequireTools(tb), path)
	if err != nil {
		tb.Fatal(err)
	}

	const probe = 5

	got, err := doc.PixelColor(page, probe, probe)
	if err != nil {
		tb.Fatal(err)
	}

	if got != want {
		tb.Errorf("page %d has the color %v near its corner, want %v", page, got, want)
	}
}

// buildDescribedJob builds a job of two pages with text, a source and a page without text, and returns the directory
// it ran in and the report it saved.
func buildDescribedJob(tb testing.TB) (string, savedReport) {
	tb.Helper()

	dir := workDir(tb)
	writePDF(tb, dir, sourceA)

	lead := blank(map[string]any{keyText: map[string]any{keyValue: "Lead"}})
	painted := blank(map[string]any{
		keySize: "300x200", "background": "#FFEECC", keyText: map[string]any{keyValue: textHello, "color": "#AA0011"},
	})
	plan := planJSON(tb, map[string]any{keyItems: []any{lead, sourceA, painted, blank(map[string]any{})}})

	res := execute(
		tb.Context(),
		tb,
		appOf(newFake(tb)),
		dir,
		commandBuild,
		inlinePlanFlag,
		plan,
		outputFlag,
		outputFile,
		reportFlag,
		reportFile,
	)
	res.requireCode(tb, 0, "")

	return dir, loadReport(tb, filepath.Join(dir, reportFile))
}

func TestGeneratedPagesAreDrawnWithTheirTextAndBackground(t *testing.T) {
	t.Parallel()

	dir, _ := buildDescribedJob(t)
	out := filepath.Join(dir, outputFile)

	requirePages(t, out, "Lead", sourceAMarker, textHello, "")

	// A page with a background is painted with it; pages without one are white.
	requirePixel(t, out, 1, [3]uint8{255, 255, 255})
	requirePixel(t, out, 3, [3]uint8{0xFF, 0xEE, 0xCC})
	requirePixel(t, out, 4, [3]uint8{255, 255, 255})
}

func TestTheReportRecordsHowEveryGeneratedPageGotItsSize(t *testing.T) {
	t.Parallel()

	_, saved := buildDescribedJob(t)
	if saved.Status != statusOK || len(saved.Styles) != 3 {
		t.Fatalf("report: %+v", saved)
	}

	// From the source after the page, set explicitly, or from the source before the page.
	origins := []string{"following_source", "explicit", "preceding_source"}
	for index, want := range origins {
		if saved.Styles[index].Size.Origin != want {
			t.Errorf("style %d took its size %s, want %s", index, saved.Styles[index].Size.Origin, want)
		}
	}

	hello := saved.Styles[1]
	if hello.Size.Width != 300 || hello.Size.Height != 200 {
		t.Errorf("the explicit size: %+v", hello.Size)
	}
}

// requireAreaInsidePage fails unless a text block has an area and starts on the page.
func requireAreaInsidePage(tb testing.TB, bounds savedBounds) {
	tb.Helper()

	if bounds.Width <= 0 || bounds.Height <= 0 || bounds.X < 0 || bounds.Y < 0 {
		tb.Errorf("text block %+v", bounds)
	}
}

func TestTheReportRecordsTheAppearanceOfEveryGeneratedPage(t *testing.T) {
	t.Parallel()

	_, saved := buildDescribedJob(t)
	if len(saved.Styles) != 3 {
		t.Fatalf("styles: %+v", saved.Styles)
	}

	hello := saved.Styles[1]
	if hello.Background != "#ffeecc" || hello.Text == nil || hello.Text.Value != textHello || hello.Text.Color != "#aa0011" {
		t.Errorf("the explicit style: %+v", hello)
	}

	// The computed text block is inside the page and has an area.
	for index := range 2 {
		requireAreaInsidePage(t, saved.Styles[index].Text.Bounds)
	}

	if saved.Styles[2].Text != nil || saved.Styles[2].Background != "none" {
		t.Errorf("a page without text: %+v", saved.Styles[2])
	}
}

func TestTheReportNamesTheBuiltInFontAndIdentifiesSourcesByContent(t *testing.T) {
	t.Parallel()

	dir, saved := buildDescribedJob(t)

	if len(saved.Fonts) != 1 || saved.Fonts[0].Name != typeset.DefaultFontName || saved.Fonts[0].File != "" {
		t.Errorf("fonts: %+v", saved.Fonts)
	}

	content := readFile(t, filepath.Join(dir, sourceA))
	sum := sha256.Sum256([]byte(content))

	if len(saved.Sources) != 1 || saved.Sources[0].Digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("sources: %+v", saved.Sources)
	}

	if size := saved.Sources[0].Bytes; size == nil || *size != int64(len(content)) {
		t.Errorf("the size of the source: %v", size)
	}
}

func TestAFontFileIsNamedByTheFontItself(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	font, err := filepath.Abs("../typeset/fontdata/NotoSans-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}

	plan := planJSON(t, map[string]any{keyItems: []any{sourceA, blankWithFont(font)}})
	res := execute(t.Context(), t, appOf(newFake(t)), dir, commandCheck, inlinePlanFlag, plan, reportFlag, reportFile)
	res.requireCode(t, 0, "")

	saved := loadReport(t, filepath.Join(dir, reportFile))
	if len(saved.Fonts) != 1 || saved.Fonts[0].Name != "NotoSans-Regular" || saved.Fonts[0].File != font {
		t.Errorf("fonts: %+v", saved.Fonts)
	}
}

func TestTheReportOfAFailedLayoutStillDescribesTheTextThatDidNotFit(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	// The words are wider than a page of 100 points allows.
	plan := planJSON(t, map[string]any{keyItems: []any{
		sourceA, blank(map[string]any{keySize: "100x100", keyText: map[string]any{keyValue: textHello}}),
	}})

	res := execute(t.Context(), t, appOf(newFake(t)), dir, commandCheck, inlinePlanFlag, plan, reportFlag, reportFile)
	res.requireCode(t, 2, "text_overflow")

	saved := loadReport(t, filepath.Join(dir, reportFile))
	if len(saved.Styles) != 1 || saved.Styles[0].Text == nil || saved.Styles[0].Text.Value != textHello {
		t.Fatalf("styles: %+v", saved.Styles)
	}

	// Rejected overflow retains the same computed text block as explicit allow.
	if saved.Styles[0].Text.Bounds.Width <= 0 || saved.Styles[0].Text.Bounds.Height <= 0 {
		t.Errorf("the text block of text that was not placed: %+v", saved.Styles[0].Text.Bounds)
	}
}
