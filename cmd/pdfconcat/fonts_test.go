// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"
)

// fontBytes is the font file the tests supply as a file-backed font.
const (
	itemIDFormat        = "/items/%d"
	fontUnreadableCode  = "font_unreadable"
	hebrewText          = "שלום"
	unsupportedTextCode = "text_unsupported"
	squarePageSize      = "400x400"
	textOverflowCode    = "text_overflow"
	locationMember      = "location"
	pointerMember       = "pointer"
	consumersMember     = "consumers"
)

func fontBytes(tb testing.TB) []byte {
	tb.Helper()

	return readFile(tb, fontPath)
}

func TestSuppliedFontFileIsCapturedIdentifiedAndUsed(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")

	data := fontBytes(t)
	writeFile(t, dir, "fonts/Example.ttf", string(data))
	writeFile(t, dir, fileJob, planJSON(t, itemsOf(fileA, obj{keyBlank: obj{keyText: obj{
		keyValue: "Ģimenes ā ļ", keyFont: obj{keyFile: "fonts/Example.ttf"},
	}}})))

	res := run(t, dir, "", commandBuild, flagPlan, fileJob, "-o", fileOut, flagReport, shortReportPath)
	requireExit(t, res, 0)
	verifyPages(t, filepath.Join(dir, fileOut), sourceAMarker, "Ģimenes ā ļ")

	saved := generic(t, string(readFile(t, filepath.Join(dir, shortReportPath))))
	fonts := listAt(t, saved, fontsView)

	if len(fonts) != 1 {
		t.Fatalf("fonts: %v", fonts)
	}

	// The report names the font by its content, which an independent hash confirms.
	sum := sha256.Sum256(data)
	font := objAt(t, fonts[0])

	if font["digest"] != hex.EncodeToString(sum[:]) || font[keyFile] != filepath.Join(dir, fontsView, "Example.ttf") {
		t.Errorf("font identity: %v", font)
	}
}

func TestFontFilesResolveAgainstTheirDeclaringScope(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	data := string(fontBytes(t))

	writePDFs(t, dir, 1, "chapters/c1/a")
	writeFile(t, dir, "fonts/Root.ttf", data)
	writeFile(t, dir, "chapters/c1/fonts/Group.ttf", data)

	plan := planJSON(t, obj{
		keyVersion: 1,
		keyDir:     "chapters",
		keyBlank:   obj{keyText: obj{keyValue: "root default", keyFont: obj{keyFile: "fonts/Root.ttf"}}},
		keyItems: []any{
			obj{keyDir: "c1", keyItems: []any{
				fileA,
				obj{keyBlank: obj{}},
				obj{keyBlank: obj{keyText: obj{keyValue: "group font", keyFont: obj{keyFile: "fonts/Group.ttf"}}}},
			}},
		},
	})

	res := run(t, dir, "", commandCheck, inlinePlanFlag, plan, flagDetails)
	requireExit(t, res, 0)

	got := map[string]bool{}

	for _, raw := range listAt(t, generic(t, res.stdout), fontsView) {
		got[textAt(t, raw, keyFile)] = true
	}

	rootFound := got[filepath.Join(dir, fontsView, "Root.ttf")]

	groupFound := got[filepath.Join(dir, "chapters", "c1", fontsView, "Group.ttf")]
	if !rootFound || !groupFound || len(got) != 2 {
		t.Errorf("fonts resolved to %v", got)
	}
}

func TestFontProblemsAreFoundBeforeAnySourceIsRead(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writeFile(t, dir, invalidPDFPath, "not a pdf")
	writeFile(t, dir, "notafont.ttf", "not a font")

	writeSparseFile(t, filepath.Join(dir, "huge.ttf"), 65<<20)

	cases := []struct {
		name string
		font string
		code string
		exit int
	}{
		{"missing", "missing.ttf", fontUnreadableCode, 1},
		{"malformed", "notafont.ttf", "font_invalid", 2},
		{"too large", "huge.ttf", "font_too_large", 2},
	}

	for _, test := range cases {
		plan := planJSON(t, itemsOf(invalidPDFPath, obj{keyBlank: obj{keyText: obj{keyValue: "x", keyFont: obj{keyFile: test.font}}}}))
		res := run(t, dir, "", commandCheck, inlinePlanFlag, plan)
		requireExit(t, res, test.exit)

		parsed := summaryOf(t, res)
		if parsed.DiagnosticCount != 1 || parsed.Diagnostics[0].Code != test.code {
			t.Errorf(namedDiagnosticFormat, test.name, parsed.Diagnostics)
		}

		// The garbage source was never opened, so the instruction phase is what failed.
		if parsed.Phases[phaseInstructions] != phaseIncomplete || parsed.Phases["input_inspection"] != "not_run" {
			t.Errorf("%s: phases %v", test.name, parsed.Phases)
		}

		if got := parsed.Diagnostics[0].Location; got == nil || got.Pointer != "/items/1/blank/text/font" {
			t.Errorf("%s: located at %+v", test.name, got)
		}
	}
}

func TestUnsupportedTextIsRejectedWithItsLocation(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")

	for _, text := range []string{hebrewText, "tab\there", "\U0001F600"} {
		plan := planJSON(t, itemsOf(fileA, obj{keyBlank: obj{keyText: obj{keyValue: text}}}))
		res := run(t, dir, "", commandCheck, inlinePlanFlag, plan)
		requireExit(t, res, 2)

		parsed := summaryOf(t, res)
		if parsed.Diagnostics[0].Code != unsupportedTextCode || parsed.Diagnostics[0].Location.Pointer != "/items/1/blank/text/value" {
			t.Errorf("%q: %+v", text, parsed.Diagnostics[0])
		}
	}
}

func TestInheritedUnsupportedTextRejectsBeforeSourcesAndLocatesDeclaration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	for _, override := range []bool{false, true} {
		text := obj{keyValue: hebrewText}
		item := obj{keyBlank: obj{}}
		plan := obj{keyVersion: 1, keyBlank: obj{keyText: text}, keyItems: []any{fileMissing, item}}
		pointer := "/blank/text/value"

		if override {
			plan[keyBlank] = obj{keyText: obj{keyValue: "good"}}
			item[keyBlank] = obj{keyText: text}
			pointer = "/items/1/blank/text/value"
		}

		res := run(t, dir, "", commandCheck, inlinePlanFlag, planJSON(t, plan))
		requireExit(t, res, 2)

		parsed := summaryOf(t, res)

		declarationRejected := len(parsed.Diagnostics) == 1 && parsed.Diagnostics[0].Code == unsupportedTextCode &&
			parsed.Diagnostics[0].Location.Pointer == pointer
		if !declarationRejected {
			t.Fatalf("declaration must fail before missing source: %+v", parsed.Diagnostics)
		}
	}
}

func TestDeliberateOverflowIsVisibleInTheReport(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")

	plan := planJSON(t, itemsOf(fileA, obj{keyBlank: obj{
		keySize: "100x100", keyText: obj{keyValue: unbreakableText, "overflow": "allow", keySize: 20},
	}}))

	res := run(t, dir, "", commandBuild, inlinePlanFlag, plan, "-o", fileOut, flagReport, shortReportPath)
	requireExit(t, res, 0)

	part := generic(t, run(t, dir, "", commandReport, shortReportPath, partFlag, "/items/1", flagDetails).stdout)
	text := objAt(t, part, partField, keyStyle, keyText)

	if text["overflow"] != "allow" || numberAt(t, text, "bounds", keyWidth) <= 100 {
		t.Errorf("the allowed overflow is not visible: %v", text)
	}
}

func TestInkOverflowBuildKeepsLocatedFailureAndComputedReport(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	plan := planJSON(t, itemsOf(obj{keyBlank: obj{
		keySize: squarePageSize, keyText: obj{keyValue: "j", keySize: 100, "anchor": "top-left", "align": "left", "x": 0, "y": -20},
	}}))
	res := run(t, dir, "", commandBuild, inlinePlanFlag, plan, "-o", fileOut, flagReport, shortReportPath)
	requireExit(t, res, 2)

	parsed := summaryOf(t, res)
	if len(parsed.Diagnostics) != 1 {
		t.Fatalf("streamed overflow diagnostics: %+v", parsed.Diagnostics)
	}

	if parsed.Diagnostics[0].Code != textOverflowCode {
		t.Fatalf("streamed overflow code: %+v", parsed.Diagnostics)
	}

	if parsed.Diagnostics[0].Location.Pointer != "/items/0/blank/text/x" {
		t.Fatalf("streamed overflow lost its located failure: %+v", parsed.Diagnostics)
	}

	part := generic(t, run(t, dir, "", commandReport, shortReportPath, partFlag, "/items/0", flagDetails).stdout)

	text := objAt(t, part, partField, keyStyle, keyText)
	if numberAt(t, text, "ink_bounds", "x") >= 0 || text["bounds"] == nil || text["findings"] == nil {
		t.Fatalf("rejected geometry/findings lost: %v", text)
	}
}

func TestIdenticalDefaultAndOverrideFaultsKeepEachRepairableDeclaration(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		appearance   obj
		member, code string
		exit         int
	}{
		{appearance: obj{keyValue: hebrewText}, member: "value", code: unsupportedTextCode, exit: 2},
		{
			appearance: obj{keyValue: "okay", "font": obj{"file": "missing.ttf"}},
			member:     "font", code: fontUnreadableCode, exit: 1,
		},
		{
			appearance: obj{keyValue: "j", "anchor": "top-left", "align": "left", "x": 1000, "y": -20},
			member:     "x", code: textOverflowCode, exit: 2,
		},
	} {
		plan := obj{
			keyVersion: 1,
			keyBlank:   obj{keySize: squarePageSize, keyText: row.appearance},
			keyItems:   []any{obj{keyBlank: obj{}}, obj{keyBlank: obj{keyText: row.appearance}}},
		}
		res := run(t, tempDir(t), "", commandCheck, inlinePlanFlag, planJSON(t, plan), flagDetails)
		requireExit(t, res, row.exit)

		diagnostics := listAt(t, generic(t, res.stdout), diagnosticsView)
		if len(diagnostics) != 2 {
			t.Fatalf("%s lost a distinct declaration: %v", row.member, diagnostics)
		}

		for index, prefix := range []string{"/blank/text/", "/items/1/blank/text/"} {
			actualPointer := textAt(t, diagnostics[index], locationMember, pointerMember)

			actualCode := textAt(t, diagnostics[index], "code")
			if actualPointer != prefix+row.member || actualCode != row.code {
				t.Fatalf("%s declaration mismatch: %v", row.member, diagnostics[index])
			}

			consumers := listAt(t, diagnostics[index], consumersMember)
			if len(consumers) != 1 || consumers[0] != fmt.Sprintf(itemIDFormat, index) {
				t.Fatalf("%s affected consumers: %v", row.member, consumers)
			}
		}
	}
}

func TestInheritedStaticFaultListsOnlyAffectedConsumers(t *testing.T) {
	t.Parallel()

	plan := obj{keyVersion: 1, keyBlank: obj{keySize: squarePageSize, keyText: obj{keyValue: hebrewText}}, keyItems: []any{
		obj{keyBlank: obj{}}, obj{keyBlank: obj{keyText: obj{keyValue: "okay"}}}, obj{keyBlank: obj{}},
	}}
	res := run(t, tempDir(t), "", commandCheck, inlinePlanFlag, planJSON(t, plan), flagDetails)
	requireExit(t, res, 2)

	diagnostics := listAt(t, generic(t, res.stdout), diagnosticsView)
	if len(diagnostics) != 1 {
		t.Fatalf("root declaration should aggregate: %v", diagnostics)
	}

	consumers := listAt(t, diagnostics[0], consumersMember)
	if len(consumers) != 2 || consumers[0] != "/items/0" || consumers[1] != "/items/2" {
		t.Fatalf("unaffected override included: %v", consumers)
	}
}
