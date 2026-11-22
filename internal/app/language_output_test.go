// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"golang.org/x/image/font/sfnt"
)

func TestAuthoredLanguageSelectsSerializedGlyphs(t *testing.T) {
	tools := oracle.RequireTools(t)
	fontData, err := os.ReadFile("../typeset/fontdata/NotoSans-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	font, err := sfnt.Parse(fontData)
	if err != nil {
		t.Fatal(err)
	}
	neutral, err := font.GlyphIndex(nil, 'ş')
	if err != nil {
		t.Fatal(err)
	}
	localized, err := font.GlyphIndex(nil, 'ș')
	if err != nil {
		t.Fatal(err)
	}
	if neutral == localized {
		t.Fatal("font no longer distinguishes glyphs")
	}
	for _, tag := range []string{"", "ro", "RO", "x-private", "zz-Zzzz-ZZ-x-abcdefgh-abcdefgh-abcdefgh-abcdefgh"} {
		t.Run(tag, func(t *testing.T) {
			dir := t.TempDir()
			metadata := ""
			if tag != "" {
				metadata = fmt.Sprintf(`"metadata":{"language":%q},`, tag)
			}
			plan := []byte(`{"version":9,` + metadata + `"page_defaults":{"size":"A4"},"items":[{"page":{"text":{"value":"ş"}}},{"title":"ş","divider":{},"items":[{"page":{}}]},{"contents":{"title":"ş"}}]}`)
			r := app.Execute(t.Context(), app.Request{InspectionWorkers: 2, Operation: app.Build, Input: app.PlanBytes{Data: plan}, WorkingDir: dir, Output: "language.pdf"})
			if r.Status != "ok" {
				t.Fatal(r.Diagnostics)
			}
			path := filepath.Join(dir, "language.pdf")
			if err := tools.Check(path); err != nil {
				t.Fatal(err)
			}
			raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", "--json-stream-data=inline", path).Output()
			if err != nil {
				t.Fatal(err)
			}
			var dump struct {
				Pages []struct {
					Contents []string `json:"contents"`
				} `json:"pages"`
				QPDF []map[string]any `json:"qpdf"`
			}
			if err = json.Unmarshal(raw, &dump); err != nil {
				t.Fatal(err)
			}
			want, unwanted := neutral, localized
			if strings.EqualFold(tag, "ro") {
				want, unwanted = localized, neutral
			}
			// Inspect actual painted character codes, independently parsed by qpdf.
			// The emitted TrueType CIDToGIDMap is Identity, so these are SFNT glyph IDs.
			for _, pageIndex := range []int{0, 1, 3} {
				var content bytes.Buffer
				for _, ref := range dump.Pages[pageIndex].Contents {
					object, ok := dump.QPDF[1]["obj:"+ref].(map[string]any)
					if !ok {
						t.Fatalf("missing content object %s", ref)
					}
					stream, ok := object["stream"].(map[string]any)
					if !ok {
						t.Fatal("missing stream")
					}
					encoded, ok := stream["data"].(string)
					if !ok {
						t.Fatal("missing stream bytes")
					}
					data, err := base64.StdEncoding.DecodeString(encoded)
					if err != nil {
						t.Fatal(err)
					}
					content.Write(data)
				}
				text := strings.ToUpper(content.String())
				if !strings.Contains(text, fmt.Sprintf("<%04X>", want)) || strings.Contains(text, fmt.Sprintf("<%04X>", unwanted)) {
					t.Fatalf("page %d language %q painted wrong glyph: %s", pageIndex+1, tag, text)
				}
			}
			doc, err := oracle.Load(tools, path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(doc.FirstLine(1), "ş") {
				t.Fatal("authored Unicode changed")
			}
		})
	}
}
