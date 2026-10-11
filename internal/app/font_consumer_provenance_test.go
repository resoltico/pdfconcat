// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestBuiltinConsumersStaySeparateFromExternalFontDeclarations(t *testing.T) {
	t.Parallel()
	for _, externalRoot := range []bool{false, true} {
		name := "builtin root"
		if externalRoot {
			name = "external root"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := workDir(t)
			for _, file := range []string{"a.ttf", "b.ttf"} {
				writeFile(t, filepath.Join(dir, file), "invalid font bytes")
			}
			var rootFont any = "default"
			if externalRoot {
				rootFont = map[string]any{keyFile: "a.ttf"}
			}
			builtin := func() map[string]any { return blank(map[string]any{keyText: map[string]any{keyFont: "default"}}) }
			data := planJSON(t, map[string]any{
				keyBlank: map[string]any{keySize: "100x100", keyText: map[string]any{keyValue: textHello, keyFont: rootFont}},
				keyItems: []any{blank(map[string]any{}), builtin(), map[string]any{"dir": ".", keyItems: []any{blank(map[string]any{}), blankWithFont("b.ttf"), builtin(), blankWithFont("a.ttf")}}, blank(map[string]any{})},
			})
			res := execute(t.Context(), t, nativeInventoryApp(), dir, commandCheck, inlinePlanFlag, data, reportFlag, reportFile)
			res.requireCode(t, 2, codeInvalid)
			saved := decodedInventoryReport(t, dir)
			expected := map[string][]string{
				"/items/2/items/1/blank/text/font": {"/items/2/items/1"},
				"/items/2/items/3/blank/text/font": {"/items/2/items/3"},
			}
			if externalRoot {
				expected["/blank/text/font"] = []string{"/items/0", "/items/2/items/0", "/items/3"}
			}
			if len(saved.Diagnostics) != len(expected) {
				t.Fatalf("incomplete external font provenance: %+v", saved.Diagnostics)
			}
			for _, found := range saved.Diagnostics {
				if found.Location == nil {
					t.Fatalf("font declaration lost location: %+v", found)
				}
				consumers, known := expected[found.Location.Pointer]
				if !known || string(found.Code) != codeInvalid || !slices.Equal(found.Consumers, consumers) {
					t.Fatalf("builtin/external consumers misattributed: %+v", found)
				}
				delete(expected, found.Location.Pointer)
			}
			if len(expected) != 0 {
				t.Fatalf("external declarations missing: %+v", expected)
			}
		})
	}
}
