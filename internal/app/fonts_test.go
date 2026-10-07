// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"os"
	"path/filepath"
	"testing"
)

// maxFontFileBytes is the largest font file the program reads: 64 MiB.
const maxFontFileBytes = 64 << 20

// writeSparseFile creates a file of size bytes that is all zeros.
func writeSparseFile(tb testing.TB, path string, size int64) {
	tb.Helper()

	file, err := os.Create(filepath.Clean(path))
	if err != nil {
		tb.Fatal(err)
	}

	err = file.Truncate(size)
	if err != nil {
		tb.Fatal(err)
	}

	err = file.Close()
	if err != nil {
		tb.Fatal(err)
	}
}

// blankWithFont is a plan item for a generated page whose text uses the font file.
func blankWithFont(file string) map[string]any {
	return map[string]any{keyBlank: map[string]any{keyText: map[string]any{keyValue: "x", keyFont: map[string]any{keyFile: file}}}}
}

func TestEveryFontProblemIsReportedInOrderBeforeAnySourceIsRead(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writeFile(t, filepath.Join(dir, "garbage.pdf"), notAPDF)
	writeFile(t, filepath.Join(dir, invalidFontPath), "not a font")
	writeSparseFile(t, filepath.Join(dir, "limit.ttf"), maxFontFileBytes)
	writeSparseFile(t, filepath.Join(dir, oversizedFontPath), maxFontFileBytes+1)

	runner := appOf(newFake(t))

	// A font of exactly the limit is read, and is then found not to be a font; one byte more is too large.
	cases := []struct {
		font string
		code string
		exit int
	}{
		{"missing.ttf", fontUnreadableCode, 1},
		{invalidFontPath, codeInvalid, 2},
		{"limit.ttf", codeInvalid, 2},
		{oversizedFontPath, "font_too_large", 2},
	}

	for _, test := range cases {
		plan := planJSON(t, map[string]any{keyItems: []any{"garbage.pdf", blankWithFont(test.font)}})
		res := execute(t.Context(), t, runner, dir, commandCheck, inlinePlanFlag, plan)
		parsed := res.requireCode(t, test.exit, test.code)

		wrongPath := parsed.Diagnostics[0].Path != filepath.Join(dir, test.font)
		if parsed.DiagnosticCount != 1 || parsed.Diagnostics[0].Code != test.code || wrongPath {
			t.Errorf(namedFailureFormat, test.font, parsed.Diagnostics)
		}

		// The garbage source was never opened.
		if parsed.Phases["instructions"] != phaseIncomplete || parsed.Phases[phaseInspected] != stateNotRun {
			t.Errorf("%s: phases %v", test.font, parsed.Phases)
		}
	}

	// Several bad fonts are all reported, in the order the plan uses them, and the first decides the status.
	plan := planJSON(
		t,
		map[string]any{keyItems: []any{blankWithFont("missing.ttf"), blankWithFont(invalidFontPath), blankWithFont(oversizedFontPath)}},
	)
	res := execute(t.Context(), t, runner, dir, commandCheck, inlinePlanFlag, plan)
	parsed := res.requireCode(t, 1, fontUnreadableCode)

	codes := make([]string, 0, len(parsed.Diagnostics))
	for _, found := range parsed.Diagnostics {
		codes = append(codes, found.Code)
	}

	if len(codes) != 3 || codes[0] != fontUnreadableCode || codes[1] != codeInvalid || codes[2] != "font_too_large" {
		t.Errorf("codes %v", codes)
	}
}
