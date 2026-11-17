// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package files_test

import (
	"path/filepath"
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/files"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestTypedFontDeclarationScopes(t *testing.T) {
	base := t.TempDir()
	font := packet.Setting[packet.Font]{Present: true, Value: packet.Font{Path: "same.ttf"}}
	d, ds := packet.ValidateDeclaration(packet.Declaration{Dir: "inputs", PageDefaults: packet.PageStyle{Text: packet.TextStyle{Font: font}}, Items: []packet.Item{&packet.GroupItem{Title: "Section", Dir: "nested", Divider: &packet.PageStyle{Text: packet.TextStyle{Font: font}}, Items: []packet.Item{&packet.PageItem{Count: 1}, &packet.PageItem{Count: 1, Style: packet.PageStyle{Text: packet.TextStyle{Font: font}}}}}}})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	d, ds = files.ResolveDeclaration(d, base)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	leaves := pdffixture.Plan(t, d).Leaves()
	want := []string{filepath.Join(base, "inputs/same.ttf"), filepath.Join(base, "same.ttf"), filepath.Join(base, "inputs/nested/same.ttf")}
	for index, leaf := range leaves {
		if leaf.Page.Text.Font.Value.Path != want[index] {
			t.Fatalf("font declaration scope %d: %s != %s", index, leaf.Page.Text.Font.Value.Path, want[index])
		}
	}
}
