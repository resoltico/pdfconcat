// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/report"
)

func TestFontFailureConsumersSurvivePrecedingPDFAndIdenticalOverride(t *testing.T) {
	t.Parallel()
	dir := workDir(t)
	writePDF(t, dir, sourceA)
	writeFile(t, filepath.Join(dir, invalidFontPath), "not a font")
	plan := planJSON(t, map[string]any{
		keyBlank: map[string]any{keyText: map[string]any{keyValue: textHello, keyFont: map[string]any{keyFile: invalidFontPath}}},
		keyItems: []any{sourceA, blank(map[string]any{}), blankWithFont(invalidFontPath)},
	})

	res := execute(t.Context(), t, nativeInventoryApp(), dir, commandCheck, inlinePlanFlag, plan, reportFlag, reportFile)
	res.requireCode(t, 2, codeInvalid)
	saved := decodedInventoryReport(t, dir)

	if len(saved.Diagnostics) != 2 {
		t.Fatalf("distinct root/item font declarations lost: %+v", saved.Diagnostics)
	}

	for index, want := range []struct {
		pointer string
		part    string
	}{
		{"/blank/text/font", "/items/1"},
		{"/items/2/blank/text/font", "/items/2"},
	} {
		got := saved.Diagnostics[index]
		if got.Location == nil || got.Location.Pointer != want.pointer || !slices.Equal(got.Consumers, []string{want.part}) {
			t.Fatalf("font declaration/affected consumers: %+v want %s/%s", got, want.pointer, want.part)
		}
	}
}

func TestRealInspectionFailuresRetainFirstUseOrderAcrossBlankAndRepeatedPDF(t *testing.T) {
	t.Parallel()
	dir := workDir(t)
	writePDF(t, dir, sourceA)

	for _, name := range []string{sourceB, sourceCPath} {
		writeFile(t, filepath.Join(dir, name), notAPDF)
	}

	plan := planJSON(t, map[string]any{keyItems: []any{
		blank(map[string]any{keySize: "100x100", keyText: map[string]any{keyValue: textHello}}),
		sourceA, sourceB, sourceCPath, sourceB,
	}})
	res := execute(t.Context(), t, nativeInventoryApp(), dir, commandCheck, jobsFlag, "3", inlinePlanFlag, plan,
		reportFlag, reportFile)
	res.requireCode(t, 1, codePDFInvalid)
	saved := decodedInventoryReport(t, dir)

	if len(saved.Diagnostics) != 2 {
		t.Fatalf("inspection failures lost: %+v", saved.Diagnostics)
	}

	for index, want := range []struct{ path, pointer string }{{sourceB, "/items/2"}, {sourceCPath, "/items/3"}} {
		got := saved.Diagnostics[index]
		if filepath.Base(got.Path) != want.path || got.Location == nil || got.Location.Pointer != want.pointer {
			t.Fatalf("diagnostic order/location: %+v want %+v", got, want)
		}
	}
}

func nativeInventoryApp() *app.App {
	return app.New(func() (app.Engine, error) { return pdfengine.New() })
}

func decodedInventoryReport(t *testing.T, dir string) *report.Report {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), app.OperationTestTimeout)
	defer cancel()

	saved, decodeErr := report.Decode(ctx, reportFile, strings.NewReader(readFile(t, filepath.Join(dir, reportFile))))
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}

	return saved
}
