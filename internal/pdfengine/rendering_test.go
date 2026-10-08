// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

func TestRejectsDynamicFormSourcesAndAllowsStaticFlag(t *testing.T) {
	t.Parallel()
	engine := newEngine(t)
	tools := pdforacle.RequireTools(t)

	for _, flag := range []string{"true", "false"} {
		doc := pdffixture.Plain("STATIC")
		doc.Objs[0] = bytes.Replace(doc.Objs[0], []byte(" >>"), []byte(" /NeedsRendering "+flag+" >>"), 1)

		path := writeDoc(t, t.TempDir(), "needs-rendering", doc)
		if err := tools.Check(path); err != nil {
			t.Fatal(err)
		}

		_, err := engine.Inspect(t.Context(), path)
		if flag == "false" {
			if err != nil {
				t.Fatalf("static rendering flag rejected: %v", err)
			}

			continue
		}

		failure := requireFailure(t, err, pdfengine.CodeUnsupportedRendering)
		if failure.Path != path {
			t.Fatal("dynamic rendering rejection lost source identity")
		}
	}

	xfa := pdffixture.Plain("STATIC")
	packet := "<?xml version=\"1.0\"?><template xmlns=\"http://www.xfa.org/schema/xfa-template/3.3/\"/>"
	xfa.Objs[0] = bytes.Replace(xfa.Objs[0], []byte(" >>"), []byte(" /AcroForm << /Fields [] /XFA 6 0 R >> >>"), 1)
	xfa.Objs = append(xfa.Objs, fmt.Appendf(nil, "<< /Length %d >>\nstream\n%sendstream", len(packet), packet))

	path := writeDoc(t, t.TempDir(), "xfa", xfa)
	if err := tools.Check(path); err != nil {
		t.Fatal(err)
	}

	_, err := engine.Inspect(t.Context(), path)

	failure := requireFailure(t, err, pdfengine.CodeUnsupportedRendering)
	if failure.Path != path {
		t.Fatal("XFA rejection lost source identity")
	}

	plain, info := inspectDoc(t, engine, "static-control", pdffixture.Plain("CONTROL"))
	rejectWithoutWrite(t, engine, path, plain, &info, []pdfengine.Run{pdfengine.SourcePages(0, 1, 1)})
}

func TestMalformedCatalogCannotBecomeASilentDefault(t *testing.T) {
	t.Parallel()
	engine := newEngine(t)

	for _, body := range []string{"null", "(not a catalog)", "<< /Type /Catalog >>"} {
		doc := pdffixture.Plain("CONTROL")
		doc.Objs[0] = []byte(body)
		path := writeDoc(t, t.TempDir(), "malformed-catalog", doc)
		_, err := engine.Inspect(t.Context(), path)

		failure := requireFailure(t, err, pdfengine.CodeInvalid)
		if failure.Path != path {
			t.Fatal("malformed catalog error lost source identity")
		}
	}
}

func optionalLayer(state string) *pdffixture.Doc {
	content := "/OC /Layer BDC\nBT /F1 36 Tf 1 0 0 1 40 400 Tm (HIDDEN SECRET) Tj ET\nEMC\n"

	return &pdffixture.Doc{Version: "1.7", Objs: [][]byte{
		fmt.Appendf(nil, "<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [6 0 R] /D << /BaseState /ON %s >> >> >>", state),
		[]byte("<< /Type /Pages /Kids [3 0 R] /Count 1 >>"),
		[]byte(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 600] /Resources " +
				"<< /Font << /F1 5 0 R >> /Properties << /Layer 6 0 R >> >> /Contents 4 0 R >>",
		),
		fmt.Appendf(nil, "<< /Length %d >>\nstream\n%sendstream", len(content), content),
		[]byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"),
		[]byte("<< /Type /OCG /Name (Layer) >>"),
	}}
}

func TestOptionalLayerSourcesRejectBeforeWritingAndHaveIndependentVisibilityControl(t *testing.T) {
	t.Parallel()
	tools := pdforacle.RequireTools(t)
	engine := newEngine(t)
	dir := t.TempDir()
	off := writeDoc(t, dir, "off", optionalLayer("/OFF [6 0 R]"))
	visible := writeDoc(t, dir, "visible", optionalLayer("/ON [6 0 R]"))

	dark, err := pdforacle.Load(tools, visible)
	if err != nil {
		t.Fatal(err)
	}

	white, err := pdforacle.Load(tools, off)
	if err != nil {
		t.Fatal(err)
	}

	difference, err := dark.AppearanceDifference(1, white, 1, 3)
	if err != nil || difference < 0.001 {
		t.Fatalf("independent OFF/ON visibility control: %g %v", difference, err)
	}

	for _, path := range []string{off, visible} {
		_, inspectionErr := engine.Inspect(context.Background(), path)

		failure := requireFailure(t, inspectionErr, pdfengine.CodeUnsupportedRendering)
		if failure.Path != path {
			t.Fatalf("rejection names wrong source: %s", failure.Path)
		}
	}

	multiple := optionalLayer("/OFF [6 0 R 7 0 R]")
	multiple.Objs[0] = bytes.Replace(multiple.Objs[0], []byte("/OCGs [6 0 R]"), []byte("/OCGs [6 0 R 7 0 R]"), 1)
	multiple.Objs = append(multiple.Objs, []byte("<< /Type /OCG /Name (Layer) >>"))

	multiPath := writeDoc(t, dir, "duplicate-group-names", multiple)
	if checkErr := tools.Check(multiPath); checkErr != nil {
		t.Fatal(checkErr)
	}

	_, multiErr := engine.Inspect(t.Context(), multiPath)

	multiFailure := requireFailure(t, multiErr, pdfengine.CodeUnsupportedRendering)
	if multiFailure.Path != multiPath {
		t.Fatal("multiple group names changed capability rejection identity")
	}

	plain, info := inspectDoc(t, engine, "plain", pdffixture.Plain("PLAIN"))
	for _, order := range [][]pdfengine.Run{
		{pdfengine.SourcePages(0, 1, 1)},
		{pdfengine.SourcePages(1, 1, 1), pdfengine.SourcePages(0, 1, 1)},
		{pdfengine.SourcePages(0, 1, 1), pdfengine.SourcePages(0, 1, 1)},
	} {
		rejectWithoutWrite(t, engine, off, plain, &info, order)
	}
}

func rejectWithoutWrite(t *testing.T, engine *pdfengine.Engine, layer, plain string, info *pdfengine.SourceInfo, order []pdfengine.Run) {
	t.Helper()

	output := filepath.Join(t.TempDir(), outputFilename)
	if writeErr := os.WriteFile(output, []byte("KEEP"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	req := &pdfengine.AssembleRequest{
		Destination:   output,
		Sources:       []pdfengine.SourceFile{{Path: layer, Info: *info}, {Path: plain, Info: *info}},
		Order:         order,
		ExpectedPages: len(order),
	}
	assemblyErr := engine.Assemble(context.Background(), req)

	failure := requireFailure(t, assemblyErr, pdfengine.CodeUnsupportedRendering)
	if failure.Source != 0 || failure.Path != layer {
		t.Fatalf("rejection names wrong source: %+v", failure)
	}

	data, readErr := os.ReadFile(filepath.Clean(output))
	if readErr != nil || string(data) != "KEEP" {
		t.Fatalf("unsupported source replaced destination: %q %v", data, readErr)
	}
}
