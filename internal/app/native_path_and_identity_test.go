// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
	"github.com/resoltico/pdfconcat/internal/report"
)

func TestNativeParentTraversalSelectsCapturedPDF(t *testing.T) {
	dir := workDir(t)
	logical, physical := filepath.Join(dir, "logical"), filepath.Join(dir, "physical")
	for _, path := range []string{logical, filepath.Join(physical, "child")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writePDF(t, logical, "selected.pdf")
	writePDF(t, physical, "native.pdf")
	native := readFile(t, filepath.Join(physical, "native.pdf"))
	writeFile(t, filepath.Join(physical, "selected.pdf"), native)
	if err := nativeDirectoryLink(t, filepath.Join(physical, "child"), filepath.Join(logical, "link")); err != nil {
		t.Fatal(err)
	}
	expression := logical + "/link/../selected.pdf"
	referenceBytes, readErr := os.ReadFile(expression)
	if readErr != nil {
		t.Fatal(readErr)
	}
	native = string(referenceBytes)
	res := execute(t.Context(), t, appOf(newFake(t)), dir, "check", "--report", "proof.json", expression)
	res.requireCode(t, 0, "")
	f, err := os.Open(filepath.Join(dir, "proof.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	saved, err := report.Decode(t.Context(), f.Name(), f)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Sources[0].Digest != fmt.Sprintf("%x", sha256.Sum256([]byte(native))) {
		t.Fatalf("captured lexical file: %+v", saved.Sources)
	}
}

func TestFailedInspectionRetainsCapturedEvidence(t *testing.T) {
	dir := workDir(t)
	data := []byte("deterministic invalid PDF\n")
	if err := os.WriteFile(filepath.Join(dir, "invalid.pdf"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	res := execute(t.Context(), t, appOf(newFake(t)), dir, "check", "--report", "proof.json", "invalid.pdf")
	res.requireCode(t, 1, "")
	f, err := os.Open(filepath.Join(dir, "proof.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	saved, err := report.Decode(t.Context(), f.Name(), f)
	if err != nil {
		t.Fatal(err)
	}
	source := saved.Sources[0]
	if source.Bytes == nil || *source.Bytes != int64(len(data)) || source.Digest != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatalf("lost capture evidence: %+v", source)
	}
	if saved.Parts[0].Pages != nil || saved.Parts[0].Range != nil {
		t.Fatalf("invented inspection: %+v", saved.Parts[0])
	}
}

func TestPhysicalAliasesInspectOnce(t *testing.T) {
	dir := workDir(t)
	writePDF(t, dir, "source.pdf")
	if err := os.Link(filepath.Join(dir, "source.pdf"), filepath.Join(dir, "alias.pdf")); err != nil {
		t.Fatal(err)
	}
	engine := newFake(t)
	var calls atomic.Int64
	engine.inspect = func(ctx context.Context, path string) (pdfengine.SourceInfo, error) {
		calls.Add(1)
		return engine.realInspect(ctx, path)
	}
	res := execute(t.Context(), t, appOf(engine), dir, "check", "source.pdf", "alias.pdf")
	res.requireCode(t, 0, "")
	if calls.Load() != 1 {
		t.Fatalf("inspection calls=%d, want 1", calls.Load())
	}
}

func TestNativeTraversalControlsPlanFontAndArtifactRoles(t *testing.T) {
	dir := workDir(t)
	logical, physical := filepath.Join(dir, "logical"), filepath.Join(dir, "physical")
	for _, path := range []string{logical, filepath.Join(physical, "child")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := nativeDirectoryLink(t, filepath.Join(physical, "child"), filepath.Join(logical, "link")); err != nil {
		t.Fatal(err)
	}
	expression := logical + "/link/../"
	reference, err := os.Stat(expression)
	if err != nil {
		t.Fatal(err)
	}
	reached, decoy := physical, logical
	logicalInfo, err := os.Stat(logical)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(reference, logicalInfo) {
		reached, decoy = logical, physical
	}
	t.Logf("native traversal reaches %s; decoy is %s", reached, decoy)

	writePDF(t, logical, "source.pdf")
	writePDF(t, physical, "native.pdf")
	writeFile(t, filepath.Join(physical, "source.pdf"), readFile(t, filepath.Join(physical, "native.pdf")))
	writeFile(t, filepath.Join(reached, "source.pdf"), readFile(t, filepath.Join(physical, "native.pdf")))
	// Plan selection and relative anchoring must both use the directory reached by native traversal.
	writeFile(t, filepath.Join(decoy, "plan.json"), `{"version":1,"items":["absent.pdf"]}`)
	writeFile(t, filepath.Join(reached, "plan.json"), `{"version":1,"items":["source.pdf"]}`)
	execute(t.Context(), t, appOf(newFake(t)), dir, "build", "--plan", expression+"plan.json", "-o", expression+"output.pdf", "--report", expression+"proof.json").requireCode(t, 0, "")
	for _, name := range []string{"output.pdf", "proof.json"} {
		if _, err := os.Stat(filepath.Join(reached, name)); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(decoy, name)); !os.IsNotExist(err) {
			t.Fatalf("lexical artifact %s exists: %v", name, err)
		}
	}
	execute(t.Context(), t, appOf(newFake(t)), dir, "report", expression+"proof.json").requireCode(t, 0, "")
	tools := pdforacle.RequireTools(t)
	document, err := pdforacle.Load(tools, filepath.Join(reached, "output.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	texts, err := document.RawTexts()
	if err != nil || len(texts) != 1 || !strings.Contains(texts[0], "native") {
		t.Fatalf("wrong assembled marker: %v %v", texts, err)
	}
	// A file-font expression uses the same native selection, rather than the invalid lexical decoy.
	bytes, err := os.ReadFile("../typeset/fontdata/NotoSans-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reached, "font.ttf"), bytes, 0o600); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(decoy, "font.ttf"), "not a font")
	plan := fmt.Sprintf(`{"version":1,"items":[{"blank":{"size":"100x100","text":{"value":"hello","font":{"file":%q}}}}]}`, expression+"font.ttf")
	execute(t.Context(), t, appOf(newFake(t)), dir, "check", "--plan-json", plan).requireCode(t, 0, "")
}

func TestPhysicalAliasOccurrencesKeepIndependentLinksAndForms(t *testing.T) {
	tools := pdforacle.RequireTools(t)
	for _, kind := range []string{"links", "form"} {
		t.Run(kind, func(t *testing.T) {
			dir := workDir(t)
			source := filepath.Join(dir, "source.pdf")
			pages := 2
			document := pdffixture.Form("alias")
			fact := pdforacle.SourceFact{Pages: 2, Version: 17}
			if kind == "links" {
				document = pdffixture.Links("alias")
				pages = 2
				fact.Pages = 2
				fact.Links = map[int][]int{1: {2, 2}, 2: {1, 1}}
			}
			if err := document.WriteFile(source); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(source, filepath.Join(dir, "alias.pdf")); err != nil {
				t.Fatal(err)
			}
			execute(t.Context(), t, appOf(newFake(t)), dir, "build", "-o", "output.pdf", "source.pdf", "alias.pdf").requireCode(t, 0, "")
			actual, err := pdforacle.Load(tools, filepath.Join(dir, "output.pdf"))
			if err != nil {
				t.Fatal(err)
			}
			expectation := pdforacle.Expectation{Sources: map[string]pdforacle.SourceFact{"physical": fact}}
			for occurrence := 1; occurrence <= 2; occurrence++ {
				for page := 1; page <= pages; page++ {
					marker := fmt.Sprintf("alias p%d", page)
					expectation.Pages = append(expectation.Pages, pdforacle.ExpectedPage{Text: marker, Source: "physical", Occurrence: occurrence, SourcePage: page})
				}
			}
			if findings := actual.Verify(expectation); len(findings) != 0 {
				t.Fatalf("alias occurrence oracle: %+v", findings)
			}
		})
	}
}
