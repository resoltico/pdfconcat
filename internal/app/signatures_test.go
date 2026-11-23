// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestIncrementalTimestampDiscard(t *testing.T) {
	dir := t.TempDir()
	fixture := "testdata/incremental-timestamp/incremental-timestamp.pdf"
	before, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "source.pdf")
	if err := os.WriteFile(input, before, 0600); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(before))
	for _, operation := range []app.Operation{app.Check, app.Build} {
		target := string(operation) + ".pdf"
		direct := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: operation, WorkingDir: dir, Output: target,
			Input: app.Items{app.SourceItem{Ref: "direct", Path: input}}})
		if direct.Status != "invalid" || len(direct.Diagnostics) != 1 || direct.Diagnostics[0].Code != diag.SourceSigned {
			t.Fatalf("direct %s: %+v", operation, direct)
		}
		if _, err := os.Stat(filepath.Join(dir, target)); !os.IsNotExist(err) {
			t.Fatalf("default refusal created output: %v", err)
		}
		plan := fmt.Appendf(nil, `{"version":%d,"signatures":"discard","items":[{"source":"source.pdf"}]}`, app.PlanVersion)
		result := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: operation, WorkingDir: dir, Output: target, Input: app.PlanBytes{Data: plan}})
		if result.Status != "ok" || len(result.Parts) != 1 || result.Parts[0].SHA256 != digest || result.Parts[0].Pages == nil || *result.Parts[0].Pages != 2 {
			t.Fatalf("opt-in %s: %+v", operation, result)
		}
		warnings := 0
		for _, finding := range result.Diagnostics {
			if finding.Code == diag.SourceSignatureDiscarded {
				warnings++
				if !slices.Equal(finding.Items, []string{"/items/0"}) || finding.PDFLocation == nil || finding.PDFLocation.Object != 8 {
					t.Fatalf("warning owner: %+v", finding)
				}
			}
		}
		if warnings != 1 {
			t.Fatalf("discard warnings: %d", warnings)
		}
		after, err := os.ReadFile(input)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("original bytes changed after %s: %v", operation, err)
		}
	}
	tools := oracle.RequireTools(t)
	output := filepath.Join(dir, "build.pdf")
	checkSignatureOutput(t, tools, output)
	for _, page := range []int{1, 2} {
		original := rasterSignaturePage(t, tools.PDFToPPM, input, page, false)
		actual := rasterSignaturePage(t, tools.PDFToPPM, output, page, false)
		if !sameSignatureRaster(original, actual) {
			t.Fatalf("timestamp removal changed page %d", page)
		}
	}

	// Re-encode the effective document independently as xref and object streams.
	streamed := filepath.Join(dir, "streamed.pdf")
	if raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--object-streams=generate", input, streamed).CombinedOutput(); err != nil {
		t.Fatalf("qpdf xref-stream fixture: %v, %s", err, raw)
	}
	plan := fmt.Appendf(nil, `{"version":%d,"signatures":"discard","items":[{"source":"streamed.pdf","pages":[[1,1]]},{"source":"streamed.pdf","pages":[[1,1]]}]}`, app.PlanVersion)
	result := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Build, WorkingDir: dir, Output: "selected.pdf", Input: app.PlanBytes{Data: plan}})
	if result.Status != "ok" || !result.Output.Published || len(result.Parts) != 2 || result.Parts[0].Pages == nil || *result.Parts[0].Pages != 1 || result.Parts[1].Pages == nil || *result.Parts[1].Pages != 1 {
		t.Fatalf("xref-stream selection: %+v", result)
	}
	warnings := 0
	for _, finding := range result.Diagnostics {
		if finding.Code == diag.SourceSignatureDiscarded {
			warnings++
			if !slices.Equal(finding.Items, []string{"/items/0", "/items/1"}) {
				t.Fatalf("warning owners: %v", finding.Items)
			}
		}
	}
	if warnings != 1 {
		t.Fatalf("discard warnings for repeated source: %d", warnings)
	}
	checkSignatureOutput(t, tools, result.Output.Path)
}

func checkSignatureOutput(t *testing.T, tools oracle.Tools, path string) {
	t.Helper()
	if err := tools.Check(path); err != nil {
		t.Fatal(err)
	}
	graph, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"/Widget"`, `"/Sig"`, `"/DocTimeStamp"`, `"/SigFlags"`, `"/ByteRange"`, `"/Perms"`, `"/DSS"`, `"/VRI"`} {
		if bytes.Contains(graph, []byte(key)) {
			t.Fatalf("signature artifact %s survived", key)
		}
	}
}

func rasterSignaturePage(t *testing.T, renderer, path string, page int, hide bool) image.Image {
	t.Helper()
	num := strconv.Itoa(page)
	args := []string{"-r", "72", "-f", num, "-l", num, "-cropbox", "-singlefile", "-png"}
	if hide {
		args = append(args, "-hide-annotations")
	}
	command := exec.CommandContext(t.Context(), renderer, append(args, path)...)
	data, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func rasterSignaturePrint(t *testing.T, gs, path string, page int) image.Image {
	t.Helper()
	n := strconv.Itoa(page)
	cmd := exec.CommandContext(t.Context(), gs, "-q", "-dBATCH", "-dNOPAUSE", "-dPrinted=true", "-dUseCropBox",
		"-dFirstPage="+n, "-dLastPage="+n, "-sDEVICE=png16m", "-r72", "-sOutputFile=%stdout", path)
	data, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func sameSignatureRaster(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for y := range a.Bounds().Dy() {
		for x := range a.Bounds().Dx() {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb || aa != ba {
				return false
			}
		}
	}
	return true
}

func TestSignatureArtworkScreenAndPrint(t *testing.T) {
	tools := oracle.RequireTools(t)
	dir := t.TempDir()
	source := pdffixture.SignatureArtwork()
	// Reserve the generated resource name in the inherited page resources
	// and paint an unrelated inherited Form through the original binding.
	source.Objs[1] = []byte("<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << /XObject << /PCSignature 9 0 R >> >> >>")
	source.Objs[2] = bytes.Replace(source.Objs[2], []byte("/Resources << >> "), nil, 1)
	appearance := "1 0 0 rg 10 15 50 30 re f 1 g BT /F1 10 Tf 15 25 Td (Signature) Tj ET"
	source.Objs[7] = fmt.Appendf(nil, "<< /Type /XObject /Subtype /Form /BBox [10 15 60 45] /Matrix [1 0 0 2 5 3] /Resources << /Font << /F1 5 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(appearance), appearance)
	pageProgram := "/Artifact << /Type /Layout /PDFConcatSignatureArtwork true >> BDC q /PCSignature Do Q EMC 0 0 1 rg 15 25 20 30 re f 2 0 0 2 11 13 cm 0 0 1 1 re W n"
	source.Objs[3] = fmt.Appendf(nil, "<< /Length %d >>\nstream\n%s\nendstream", len(pageProgram), pageProgram)
	formProgram := "0 1 0 rg 210 210 12 12 re f"
	source.Objs = append(source.Objs, fmt.Appendf(nil,
		"<< /Type /XObject /Subtype /Form /BBox [200 200 250 250] /Resources << >> /Length %d >>\nstream\n%s\nendstream",
		len(formProgram), formProgram))
	input := filepath.Join(dir, "source.pdf")
	if err := source.WriteFile(input); err != nil {
		t.Fatal(err)
	}
	plan := fmt.Appendf(nil, `{"version":%d,"signatures":"discard","items":[{"source":"source.pdf"},{"source":"source.pdf"}]}`, app.PlanVersion)
	result := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Build, WorkingDir: dir, Output: "out.pdf", Input: app.PlanBytes{Data: plan}})
	if result.Status != "ok" || !result.Output.Published {
		t.Fatalf("build artwork: %+v", result.Diagnostics)
	}
	checkSignatureOutput(t, tools, result.Output.Path)
	screen := rasterSignaturePage(t, tools.PDFToPPM, input, 1, false)
	if sameSignatureRaster(screen, rasterSignaturePage(t, tools.PDFToPPM, input, 1, true)) {
		t.Fatal("screen renderer could not observe the fixture's annotation")
	}
	for _, page := range []int{1, 2} {
		if !sameSignatureRaster(screen, rasterSignaturePage(t, tools.PDFToPPM, result.Output.Path, page, false)) {
			t.Fatalf("screen artwork differs on page %d", page)
		}
	}
	t.Run("print", func(t *testing.T) {
		gs, err := exec.LookPath("gs")
		if err != nil {
			t.Skip("Ghostscript is unavailable for print raster comparison")
		}
		printed := func(path string, page int) image.Image {
			return rasterSignaturePrint(t, gs, path, page)
		}
		want := printed(input, 1)
		for _, page := range []int{1, 2} {
			if !sameSignatureRaster(want, printed(result.Output.Path, page)) {
				t.Fatalf("print artwork differs on page %d", page)
			}
		}
		noPrint := filepath.Join(dir, "no-print.pdf")
		source.Objs[5] = bytes.Replace(source.Objs[5], []byte("/F 4"), []byte("/F 0"), 1)
		if err := source.WriteFile(noPrint); err != nil {
			t.Fatal(err)
		}
		if sameSignatureRaster(want, printed(noPrint, 1)) {
			t.Fatal("print renderer did not observe the Print flag control")
		}
	})

	fitted := bytes.Replace(plan, []byte(`"items"`), []byte(`"fitting":{"target":"A4"},"items"`), 1)
	for _, name := range []string{"first-fit.pdf", "repeat-fit.pdf"} {
		built := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Build, WorkingDir: dir, Output: name, Input: app.PlanBytes{Data: fitted}})
		if built.Status != "ok" || !built.Output.Published {
			t.Fatalf("build fitted artwork: %+v", built.Diagnostics)
		}
	}
	firstFit, err := os.ReadFile(filepath.Join(dir, "first-fit.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	repeatFit, err := os.ReadFile(filepath.Join(dir, "repeat-fit.pdf"))
	if err != nil || !bytes.Equal(firstFit, repeatFit) {
		t.Fatalf("repeat changed output bytes: %v", err)
	}
	// An independent aspect-ratio calculation locates the ribbon in the
	// fitted page using the measured screen canvas and original red pixels.
	red := redSignatureBounds(screen)
	for _, page := range []int{1, 2} {
		fit := rasterSignaturePage(t, tools.PDFToPPM, filepath.Join(dir, "first-fit.pdf"), page, false)
		drawn := redSignatureBounds(fit)
		scale := math.Min(float64(fit.Bounds().Dx())/float64(screen.Bounds().Dx()), float64(fit.Bounds().Dy())/float64(screen.Bounds().Dy()))
		centerX := (float64(fit.Bounds().Dx()) - scale*float64(screen.Bounds().Dx())) / 2
		centerY := (float64(fit.Bounds().Dy()) - scale*float64(screen.Bounds().Dy())) / 2
		want := [4]float64{centerX + scale*float64(red.Min.X), centerY + scale*float64(red.Min.Y), centerX + scale*float64(red.Max.X), centerY + scale*float64(red.Max.Y)}
		for i, value := range [4]int{drawn.Min.X, drawn.Min.Y, drawn.Max.X, drawn.Max.Y} {
			if drawn.Empty() || math.Abs(float64(value)-want[i]) > 2 {
				t.Fatalf("fitted page %d red artwork %v, expected bounds near %v", page, drawn, want)
			}
		}
	}

	// Translate the widget without changing its artwork. The raster comparison
	// must distinguish this from the original location.
	source.Objs[5] = []byte(strings.ReplaceAll(strings.ReplaceAll(string(source.Objs[5]), "/F 0", "/F 4"), "75 85 175 145", "85 85 185 145"))
	moved := filepath.Join(dir, "moved.pdf")
	if err := source.WriteFile(moved); err != nil {
		t.Fatal(err)
	}
	if sameSignatureRaster(screen, rasterSignaturePage(t, tools.PDFToPPM, moved, 1, false)) {
		t.Fatal("screen plane did not detect moved artwork")
	}
}

func redSignatureBounds(img image.Image) image.Rectangle {
	var bounds image.Rectangle
	for y := range img.Bounds().Dy() {
		for x := range img.Bounds().Dx() {
			r, g, b, _ := img.At(x, y).RGBA()
			if r > 0xffff/2 && g < 0xffff/4 && b < 0xffff/4 {
				bounds = bounds.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return bounds
}

func TestSignatureDiscardUsesLatestEffectiveRevision(t *testing.T) {
	tools := oracle.RequireTools(t)
	sourceDir, err := filepath.Abs("testdata/incremental-timestamp")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		active bool
	}{
		{"inherited-timestamp.pdf", true},
		{"superseded-timestamp.pdf", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(sourceDir, tc.name)
			if err := tools.Check(input); err != nil {
				t.Fatal(err)
			}
			direct := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Check, WorkingDir: dir,
				Input: app.Items{app.SourceItem{Ref: "direct", Path: input}}})
			refused := slices.ContainsFunc(direct.Diagnostics, func(d diag.Diagnostic) bool { return d.Code == diag.SourceSigned })
			if refused != tc.active || !tc.active && direct.Status != "ok" {
				t.Fatalf("default effective-revision decision: %v", direct.Diagnostics)
			}
			plan := fmt.Appendf(nil, `{"version":%d,"signatures":"discard","items":[{"source":%q}]}`, app.PlanVersion, input)
			result := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Build, WorkingDir: dir, Output: "out.pdf",
				Input: app.PlanBytes{Data: plan}})
			discarded := slices.ContainsFunc(result.Diagnostics, func(d diag.Diagnostic) bool { return d.Code == diag.SourceSignatureDiscarded })
			if result.Status != "ok" || !result.Output.Published || discarded != tc.active {
				t.Fatalf("discard effective-revision decision: %v", result.Diagnostics)
			}
			checkSignatureOutput(t, tools, result.Output.Path)
			before := rasterSignaturePage(t, tools.PDFToPPM, input, 1, false)
			after := rasterSignaturePage(t, tools.PDFToPPM, result.Output.Path, 1, false)
			if !sameSignatureRaster(before, after) {
				t.Fatal("final revision artwork changed")
			}
		})
	}
}

func TestSignatureArtworkDerivativesAreReusable(t *testing.T) {
	tools := oracle.RequireTools(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "signed.pdf")
	if err := pdffixture.SignatureArtwork().WriteFile(source); err != nil {
		t.Fatal(err)
	}
	build := func(output, members string) string {
		t.Helper()
		plan := fmt.Appendf(nil, `{"version":%d,"signatures":"discard",%s}`, app.PlanVersion, members)
		result := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Build, WorkingDir: dir,
			Input: app.PlanBytes{Data: plan}, Output: output})
		if result.Status != "ok" || !result.Output.Published {
			t.Fatalf("build %s: %v", output, result.Diagnostics)
		}
		warnings := 0
		for _, d := range result.Diagnostics {
			if d.Code == diag.SourceSignatureDiscarded {
				warnings++
				want := "/items/0"
				if output == "reused.pdf" || output == "refitted.pdf" {
					want = "/items/2"
				}
				if !slices.Equal(d.Items, []string{want}) {
					t.Fatalf("discard warning belongs to %v, want %s", d.Items, want)
				}
			}
		}
		if warnings != 1 {
			t.Fatalf("got %d discard warnings", warnings)
		}
		checkSignatureOutput(t, tools, result.Output.Path)
		return result.Output.Path
	}
	flat := build("flat.pdf", `"items":[{"source":"signed.pdf"}]`)
	fitted := build("fitted.pdf", `"fitting":{"target":"A4"},"items":[{"source":"signed.pdf"}]`)
	const items = `"items":[{"source":"flat.pdf"},{"source":"fitted.pdf"},{"source":"signed.pdf"}]`
	reused := build("reused.pdf", items)
	refitted := build("refitted.pdf", `"fitting":{"target":"A3"},`+items)
	for i, path := range []string{flat, fitted, source} {
		page := i + 1
		want := rasterSignaturePage(t, tools.PDFToPPM, path, 1, false)
		if !sameSignatureRaster(want, rasterSignaturePage(t, tools.PDFToPPM, reused, page, false)) {
			t.Fatalf("reuse changed artwork on page %d", page)
		}
		if redSignatureBounds(rasterSignaturePage(t, tools.PDFToPPM, refitted, page, false)).Empty() {
			t.Fatalf("refitting lost artwork on page %d", page)
		}
	}
}

func TestSignatureArtworkPreservesDefaultColors(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, tc := range []struct {
		name                  string
		appearance, inherited bool
	}{
		{"inherited appearance color", true, true},
		{"original page color", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "source.pdf")
			fixture := pdffixture.SignatureColorArtwork(tc.appearance, tc.inherited)
			if err := fixture.WriteFile(input); err != nil {
				t.Fatal(err)
			}
			plan := fmt.Appendf(nil, `{"version":%d,"signatures":"discard","items":[{"source":"source.pdf"}]}`, app.PlanVersion)
			r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Build, WorkingDir: dir,
				Input: app.PlanBytes{Data: plan}, Output: "normalized.pdf"})
			if r.Status != "ok" || !r.Output.Published {
				t.Fatalf("build: %v", r.Diagnostics)
			}
			checkSignatureOutput(t, tools, r.Output.Path)
			before := rasterSignaturePage(t, tools.PDFToPPM, input, 1, false)
			if !sameSignatureRaster(before, rasterSignaturePage(t, tools.PDFToPPM, r.Output.Path, 1, false)) {
				t.Fatal("screen color changed")
			}
			// A changed CalRGB transfer function must be observable.
			object := 2
			fixture.Objs[object] = bytes.Replace(fixture.Objs[object], []byte("/Gamma [2 2 2]"), []byte("/Gamma [4 4 4]"), 1)
			changed := filepath.Join(dir, "changed.pdf")
			if err := fixture.WriteFile(changed); err != nil {
				t.Fatal(err)
			}
			if sameSignatureRaster(before, rasterSignaturePage(t, tools.PDFToPPM, changed, 1, false)) {
				t.Fatal("screen renderer did not observe changed color")
			}
			t.Run("print", func(t *testing.T) {
				gs, err := exec.LookPath("gs")
				if err != nil {
					t.Skip("Ghostscript is unavailable for print raster comparison")
				}
				printed := rasterSignaturePrint(t, gs, input, 1)
				if !sameSignatureRaster(printed, rasterSignaturePrint(t, gs, r.Output.Path, 1)) {
					t.Fatal("print color changed")
				}
				if sameSignatureRaster(printed, rasterSignaturePrint(t, gs, changed, 1)) {
					t.Fatal("print renderer did not observe changed color")
				}
			})
		})
	}
}

func TestSignatureDiscardTreatsLiveNullMetadataAsAbsent(t *testing.T) {
	dir := t.TempDir()
	d := pdffixture.EmptySignatureField("unsigned")
	d.Objs = append(d.Objs, []byte("null"))
	const fields = "/Fields [6 0 R]"
	d.Objs[0] = bytes.Replace(d.Objs[0], []byte(fields), []byte(fields+" /SigFlags 7 0 R"), 1)
	d.Objs[0] = bytes.Replace(d.Objs[0], []byte("/AcroForm"), []byte("/Perms 7 0 R /DocTimeStamp 7 0 R /DSS 7 0 R /VRI 7 0 R /AcroForm"), 1)
	if err := d.WriteFile(filepath.Join(dir, "unsigned.pdf")); err != nil {
		t.Fatal(err)
	}
	plan := fmt.Appendf(nil, `{"version":%d,"signatures":"discard","items":[{"source":"unsigned.pdf"}]}`, app.PlanVersion)
	for _, op := range []app.Operation{app.Check, app.Build} {
		r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: op, WorkingDir: dir,
			Input: app.PlanBytes{Data: plan}, Output: "output.pdf"})
		if r.Status != "ok" || slices.ContainsFunc(r.Diagnostics, func(d diag.Diagnostic) bool { return d.Code == diag.SourceSignatureDiscarded }) {
			t.Fatalf("%s changed unsigned semantics: %v", op, r.Diagnostics)
		}
	}
}

func TestSignatureDiscardKeepsSurvivingInteractiveFields(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, tc := range []struct {
		name         string
		signed, text bool
		intro        int
		repeat       int
	}{
		{"signed and unsigned fields", true, false, 0, 1},
		{"mixed fields after generated page", true, true, 1, 2},
		{"flag-only form", false, true, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			d := pdffixture.SignatureValue("packet")
			fields := "6 0 R 7 0 R"
			d.Objs = append(d.Objs, []byte("<< /Type /Annot /Subtype /Widget /FT /Sig /T (awaiting) /Rect [0 0 0 0] /F 4 /P 3 0 R >>"))
			if !tc.signed {
				fields = "7 0 R"
				d.Objs[5] = []byte("null")
			}
			if tc.text {
				fields += " 8 0 R"
				d.Objs = append(d.Objs, []byte("<< /Type /Annot /Subtype /Widget /FT /Tx /T (packet) /Rect [20 20 100 40] /F 4 /P 3 0 R /V (FT-26) >>"))
			}
			d.Objs[0] = bytes.Replace(d.Objs[0], []byte("/Fields [6 0 R]"),
				fmt.Appendf(nil, "/Fields [%s] /SigFlags 3 /DA (/F1 12 Tf 0 g) /DR << /Font << /F1 5 0 R >> >>", fields), 1)
			d.Objs[2] = bytes.Replace(d.Objs[2], []byte("/Annots [6 0 R]"), fmt.Appendf(nil, "/Annots [%s]", fields), 1)
			if err := d.WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
				t.Fatal(err)
			}
			items := make([]string, 0, tc.intro+tc.repeat)
			for range tc.intro {
				items = append(items, `{"page":{"size":"Letter"}}`)
			}
			for range tc.repeat {
				items = append(items, `{"source":"source.pdf"}`)
			}
			raw := fmt.Appendf(nil, `{"version":%d,"signatures":"discard","items":[%s]}`, app.PlanVersion, strings.Join(items, ","))
			r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Build, WorkingDir: dir,
				Output: "packet.pdf", Input: app.PlanBytes{Data: raw}})
			if r.Status != "ok" || !r.Output.Published {
				t.Fatal(r.Diagnostics)
			}
			if err := tools.Check(r.Output.Path); err != nil {
				t.Fatal(err)
			}
			output, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", "--json-key=acroform", r.Output.Path).Output()
			if err != nil {
				t.Fatal(err)
			}
			var form struct {
				AcroForm struct {
					Fields []struct {
						Name       string  `json:"fullname"`
						Kind       string  `json:"fieldtype"`
						Value      *string `json:"value"`
						Page       int     `json:"pageposfrom1"`
						Object     string  `json:"object"`
						Annotation struct {
							Object string `json:"object"`
						} `json:"annotation"`
					} `json:"fields"`
				} `json:"acroform"`
			}
			if err := json.Unmarshal(output, &form); err != nil {
				t.Fatal(err)
			}
			counts := map[int]map[string]int{}
			for _, field := range form.AcroForm.Fields {
				if field.Page <= tc.intro || field.Page > tc.intro+tc.repeat || field.Annotation.Object == "" || field.Annotation.Object != field.Object {
					t.Fatalf("field membership: %+v", field)
				}
				if counts[field.Page] == nil {
					counts[field.Page] = map[string]int{}
				}
				counts[field.Page][field.Kind]++
				switch field.Kind {
				case "/Sig":
					if !strings.HasSuffix(field.Name, "awaiting") || field.Value != nil {
						t.Fatalf("retained signature field: %+v", field)
					}
				case "/Tx":
					if !tc.text || !strings.HasSuffix(field.Name, "packet") || field.Value == nil || *field.Value != "u:FT-26" {
						t.Fatalf("retained text field: %+v", field)
					}
				default:
					t.Fatalf("unexpected field: %+v", field)
				}
			}
			for page := tc.intro + 1; page <= tc.intro+tc.repeat; page++ {
				if counts[page]["/Sig"] != 1 || tc.text && counts[page]["/Tx"] != 1 {
					t.Fatalf("retained fields: %v", counts)
				}
			}
		})
	}
}

func TestDegeneratePrintableSignatureRefusalPreservesOutput(t *testing.T) {
	dir := t.TempDir()
	d := pdffixture.SignatureArtwork()
	d.Objs[5] = bytes.Replace(d.Objs[5], []byte("[75 85 175 145]"), []byte("[75 85 175 85]"), 1)
	if err := d.WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
		t.Fatal(err)
	}
	incumbent := filepath.Join(dir, "packet.pdf")
	if err := os.WriteFile(incumbent, []byte("incumbent"), 0600); err != nil {
		t.Fatal(err)
	}
	plan := fmt.Appendf(nil, `{"version":%d,"signatures":"discard","items":[{"source":"source.pdf"}]}`, app.PlanVersion)
	for _, op := range []app.Operation{app.Check, app.Build} {
		r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: op, WorkingDir: dir, Overwrite: true,
			Output: "packet.pdf", Input: app.PlanBytes{Data: plan}})
		if r.Status != "invalid" || len(r.Diagnostics) != 1 {
			t.Fatal(r.Diagnostics)
		}
		d := r.Diagnostics[0]
		if d.Code != diag.SourceSignatureDiscardUnsupported || d.PDFLocation == nil || d.PDFLocation.Page != 1 || d.PDFLocation.Object != 6 {
			t.Fatalf("unlocated refusal: %+v", d)
		}
	}
	if data, err := os.ReadFile(incumbent); err != nil || string(data) != "incumbent" {
		t.Fatalf("destination = %q, %v", data, err)
	}
}

func TestXFAOnlyRefusesAndHybridFormWarns(t *testing.T) {
	dir := t.TempDir()
	const payload = `<xdp:xdp xmlns:xdp="http://ns.adobe.com/xdp/"><template><subform><field name="site"/></subform></template><datasets><data><site>Riga</site></data></datasets></xdp:xdp>`
	addXFA := func(d *pdffixture.Doc, source string, hybrid bool) {
		t.Helper()
		entry := fmt.Sprintf("/XFA %d 0 R", len(d.Objs)+1)
		if hybrid {
			d.Objs[0] = bytes.Replace(d.Objs[0], []byte("/AcroForm <<"), []byte("/AcroForm << "+entry+" "), 1)
		} else {
			d.Objs[0] = bytes.Replace(d.Objs[0], []byte(">>"), []byte("/AcroForm << /Fields [] "+entry+" >> >>"), 1)
		}
		d.Objs = append(d.Objs, fmt.Appendf(nil, "<< /Length %d >>\nstream\n%s\nendstream", len(payload), payload))
		if err := d.WriteFile(filepath.Join(dir, source)); err != nil {
			t.Fatal(err)
		}
	}
	addXFA(pdffixture.Plain("Open this form to enter the site"), "xfa.pdf", false)
	addXFA(pdffixture.Form("hybrid"), "hybrid.pdf", true)
	for _, policy := range []string{"refuse", "discard"} {
		raw := fmt.Appendf(nil, `{"version":%d,"signatures":%q,"items":[{"source":"xfa.pdf"}]}`, app.PlanVersion, policy)
		for _, op := range []app.Operation{app.Check, app.Build} {
			r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: op, WorkingDir: dir, Output: "xfa-out.pdf", Input: app.PlanBytes{Data: raw}})
			if r.Status != "invalid" || len(r.Diagnostics) != 1 {
				t.Fatal(r.Diagnostics)
			}
			d := r.Diagnostics[0]
			if d.Code != diag.SourceDynamicForm || d.PDFLocation == nil || d.PDFLocation.Object != 1 || d.PDFLocation.Path != "Catalog.AcroForm.XFA" {
				t.Fatalf("XFA refusal: %+v", d)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "xfa-out.pdf")); !os.IsNotExist(err) {
		t.Fatalf("XFA-only published output: %v", err)
	}
	raw := fmt.Appendf(nil, `{"version":%d,"items":[{"source":"hybrid.pdf"}]}`, app.PlanVersion)
	r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: app.Build, WorkingDir: dir, Output: "hybrid-out.pdf", Input: app.PlanBytes{Data: raw}})
	if r.Status != "ok" || !r.Output.Published || !slices.ContainsFunc(r.Diagnostics, func(d diag.Diagnostic) bool { return d.Code == diag.SourceXfaRemoved }) {
		t.Fatalf("hybrid disposition: %v", r.Diagnostics)
	}
	tools := oracle.RequireTools(t)
	out, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", "--json-key=acroform", r.Output.Path).Output()
	if err != nil || !bytes.Contains(out, []byte(`"u:shared"`)) {
		t.Fatalf("hybrid static field lost: %v, %s", err, out)
	}
}

func TestLiveNullXFAIsAbsentInCheckAndBuild(t *testing.T) {
	for _, tc := range []struct {
		name        string
		makeDoc     func() *pdffixture.Doc
		policies    []string
		wantDiscard bool
	}{
		{"ordinary form", func() *pdffixture.Doc { return pdffixture.Form("static fields") }, []string{"refuse", "discard"}, false},
		{"populated signature", pdffixture.SignatureArtwork, []string{"discard"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			d := tc.makeDoc()
			entry := fmt.Appendf(nil, "/AcroForm << /XFA %d 0 R ", len(d.Objs)+1)
			d.Objs[0] = bytes.Replace(d.Objs[0], []byte("/AcroForm << "), entry, 1)
			d.Objs = append(d.Objs, []byte("null"))
			if err := d.WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
				t.Fatal(err)
			}
			for _, policy := range tc.policies {
				data := fmt.Appendf(nil, `{"version":%d,"signatures":%q,"items":[{"source":"source.pdf"}]}`, app.PlanVersion, policy)
				for _, op := range []app.Operation{app.Check, app.Build} {
					r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: op, WorkingDir: dir,
						Output: policy + ".pdf", Input: app.PlanBytes{Data: data}})
					if r.Status != "ok" || op == app.Build && !r.Output.Published {
						t.Fatalf("%s %s: %v", policy, op, r.Diagnostics)
					}
					if slices.ContainsFunc(r.Diagnostics, func(d diag.Diagnostic) bool { return d.Code == diag.SourceXfaRemoved }) {
						t.Fatalf("live null generated an XFA warning: %v", r.Diagnostics)
					}
					discarded := slices.ContainsFunc(r.Diagnostics, func(d diag.Diagnostic) bool { return d.Code == diag.SourceSignatureDiscarded })
					if discarded != tc.wantDiscard {
						t.Fatalf("signature disposition: %v", r.Diagnostics)
					}
				}
			}
		})
	}
}

func TestSignatureDiscardResolvesOptionalSlots(t *testing.T) {
	for _, tc := range []struct {
		name         string
		object       int
		old, replace string
		value        string
		want         diag.Code
	}{
		{"null action", 6, "/AP", "/A 9 0 R /AP", "null", diag.Code{}},
		{"null additional actions", 6, "/AP", "/AA 9 0 R /AP", "null", diag.Code{}},
		{"null regeneration", 1, "/Fields", "/NeedAppearances 9 0 R /Fields", "null", diag.Code{}},
		{"false regeneration", 1, "/Fields", "/NeedAppearances 9 0 R /Fields", "false", diag.Code{}},
		{"null page group", 3, "/Resources", "/Group 9 0 R /Resources", "null", diag.Code{}},
		{"null appearance group", 8, "/Resources", "/Group 9 0 R /Resources", "null", diag.Code{}},
		{"null rotation", 6, "/AP", "/MK << /R 9 0 R >> /AP", "null", diag.Code{}},
		{"zero rotation", 6, "/AP", "/MK << /R 9 0 R >> /AP", "0", diag.Code{}},
		{"root null parent", 6, "/AP", "/Parent 9 0 R /AP", "null", diag.Code{}},
		{"widget null page", 6, "/P 3 0 R", "/P 9 0 R", "null", diag.Code{}},
		{"conflicting parent", 6, "/AP", "/Parent 3 0 R /AP", "null", diag.SourceSignatureDiscardUnsupported},
		{"wrong widget page generation", 6, "/P 3 0 R", "/P 3 1 R", "null", diag.SourceSignatureDiscardUnsupported},
		{"wrong action generation", 6, "/AP", "/A 9 1 R /AP", "null", diag.SourceUnsupported},
		{"wrong page group generation", 3, "/Resources", "/Group 9 1 R /Resources", "null", diag.SourceSignatureDiscardUnsupported},
		{"undefined rotation", 6, "/AP", "/MK << /R 99 0 R >> /AP", "null", diag.SourceSignatureDiscardUnsupported},
		{"active action", 6, "/AP", "/A 9 0 R /AP", "<< /S /GoTo /D [3 0 R /Fit] >>", diag.SourceSignatureDiscardUnsupported},
		{"active group", 8, "/Resources", "/Group 9 0 R /Resources", "<< /S /Transparency >>", diag.SourceSignatureDiscardUnsupported},
		{"active regeneration", 1, "/Fields", "/NeedAppearances 9 0 R /Fields", "true", diag.SourceSignatureDiscardUnsupported},
		{"nonzero rotation", 6, "/AP", "/MK << /R 9 0 R >> /AP", "90", diag.SourceSignatureDiscardUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			d := pdffixture.SignatureArtwork()
			object := &d.Objs[tc.object-1]
			if !bytes.Contains(*object, []byte(tc.old)) {
				t.Fatalf("fixture object %d has no %s slot", tc.object, tc.old)
			}
			*object = bytes.Replace(*object, []byte(tc.old), []byte(tc.replace), 1)
			d.Objs = append(d.Objs, []byte(tc.value))
			if err := d.WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
				t.Fatal(err)
			}
			plan := fmt.Appendf(nil, `{"version":%d,"signatures":"discard","fitting":{"target":"A4"},"items":[{"source":"source.pdf"}]}`, app.PlanVersion)
			for _, op := range []app.Operation{app.Check, app.Build} {
				r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: op, WorkingDir: dir,
					Output: "packet.pdf", Input: app.PlanBytes{Data: plan}})
				if tc.want != (diag.Code{}) {
					if r.Status != "invalid" || !slices.ContainsFunc(r.Diagnostics, func(d diag.Diagnostic) bool {
						return d.Code == tc.want && d.PDFLocation != nil
					}) {
						t.Fatalf("%s failed to preserve the refusal: %v", op, r.Diagnostics)
					}
				} else if r.Status != "ok" || op == app.Build && !r.Output.Published {
					t.Fatalf("%s refused an absent or inactive entry: %v", op, r.Diagnostics)
				}
			}
		})
	}
}
