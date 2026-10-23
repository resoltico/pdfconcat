// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

// finalFontFacts reads the independent final dictionaries and embedded programs.
func finalFontFacts(t *testing.T, path string) (map[string]int, map[string][]byte) {
	t.Helper()
	tools := oracle.RequireTools(t)
	raw := run(t, qpdfTool, "--json", path)
	var dump any
	if err := json.Unmarshal(raw, &dump); err != nil {
		t.Fatal(err)
	}
	names := map[string]int{}
	programs := map[string][]byte{}
	var walk func(any)
	walk = func(v any) {
		switch value := v.(type) {
		case map[string]any:
			for key, child := range value {
				if key == "/BaseFont" || key == "/FontName" {
					name, ok := child.(string)
					if !ok {
						t.Fatalf("font name is not a string: %v", child)
					}
					names[strings.TrimPrefix(name, "/")]++
				}
				if key == "/FontFile2" {
					reference, ok := child.(string)
					if !ok {
						t.Fatalf("font stream reference is not a string: %v", child)
					}
					ref := strings.Fields(reference)[0]
					data, err := exec.CommandContext(t.Context(), tools.QPDF, "--show-object="+ref, "--filtered-stream-data", path).Output()
					if err != nil {
						t.Fatal(err)
					}
					name, ok := value["/FontName"].(string)
					if !ok {
						t.Fatalf("font descriptor has no name: %v", value)
					}
					programs[strings.TrimPrefix(name, "/")] = data
					tables := fontTablesForDescriptor(t, data)
					post, os2 := tables["post"], tables["OS/2"]
					rawAngle := int64(binary.BigEndian.Uint32(post[4:]))
					if rawAngle >= 1<<31 {
						rawAngle -= 1 << 32
					}
					angle := float64(rawAngle) / 65536
					flags := 4
					if binary.BigEndian.Uint32(post[12:]) != 0 {
						flags |= 1
					}
					selection := binary.BigEndian.Uint16(os2[62:])
					if selection&1 != 0 || binary.BigEndian.Uint16(os2) >= 4 && selection&512 != 0 {
						flags |= 64
					}
					if value["/ItalicAngle"] != angle || value["/Flags"] != float64(flags) {
						t.Fatalf("descriptor disagrees with embedded original traits: %v angle=%v flags=%v", value, angle, flags)
					}

				}
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(dump)
	return names, programs
}

func TestSubsetNamesReserveImportedPrefixesAndDeduplicate(t *testing.T) {
	font := defaultFont(t)
	alias := defaultFont(t)
	other, err := typeset.LoadFont(altered(font))
	if err != nil {
		t.Fatal(err)
	}
	pages := []pdf.Page{textPage(t, font, "Ā ļ ķ"), textPage(t, alias, "Ελληνικά"), textPage(t, other, "Кириллица")}
	prepared, err := typeset.PrepareFonts([]*typeset.Placed{pages[0].Text, pages[1].Text, pages[2].Text})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared) != 2 {
		t.Fatal("font aliases did not share preparation")
	}
	for _, embedded := range prepared {
		if !embedded.Subset {
			t.Fatal("pruned font not marked as subset")
		}
	}
	var firstNames map[string]int
	for _, reserve := range []bool{false, true} {
		dir := t.TempDir()
		source := pdffixture.Plain("source")
		if reserve {
			source.Objs[4] = bytes.ReplaceAll(source.Objs[4], []byte("/Helvetica"), []byte("/AAAAAA+Helvetica"))
			// Descriptor-only prefix reservation, escaped on disk and canonical after reading.
			source.Objs = append(source.Objs, []byte("<< /Type /FontDescriptor /FontName /AAA#41AB+Other >>"))
		}
		path := filepath.Join(dir, "source.pdf")
		if err = source.WriteFile(path); err != nil {
			t.Fatal(err)
		}
		facts, ds := pdf.Inspect(t.Context(), source.Bytes())
		if status, _ := diag.Outcome(ds); status != "ok" {
			t.Fatal(ds)
		}
		if reserve && (!slices.Contains(facts.SubsetPrefixes, "AAAAAA") || !slices.Contains(facts.SubsetPrefixes, "AAAAAB")) {
			t.Fatalf("prefixes: %v", facts.SubsetPrefixes)
		}
		recipe := packet.Job{Parts: []packet.Part{{Source: path, Pointer: "source"}}}
		generated := map[int][]pdf.Page{}
		for i, page := range append(pages, pages[0]) {
			style := packet.Defaults()
			recipe.Parts = append(recipe.Parts, packet.Part{Page: &style, Count: 1, Pointer: fmt.Sprintf("page%d", i)})
			generated[i+1] = []pdf.Page{page}
		}
		layout, ds := packet.Resolve(recipe, map[string]packet.SourceFacts{"source": facts.SourceFacts}, 0)
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		var previous map[string]int
		for repeat := range 2 {
			var out bytes.Buffer
			if _, err = pdf.Assemble(t.Context(), layout, map[string]pdf.SourceFacts{path: facts}, wholeOccurrences(layout, map[string]pdf.SourceFacts{path: facts}), generated, nil, prepared, &out); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(dir, fmt.Sprintf("packet%d.pdf", repeat))
			if err = os.WriteFile(output, out.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			names, programs := finalFontFacts(t, output)
			if len(programs) != 2 {
				t.Fatalf("embedded programs: %d", len(programs))
			}
			generatedNames := map[string]int{}
			for name, count := range names {
				if strings.HasSuffix(name, "+NotoSans-Regular") {
					if !regexp.MustCompile(`^[A-Z]{6}\+NotoSans-Regular$`).MatchString(name) || count != 3 {
						t.Fatalf("names disagree: %v", names)
					}
					if reserve && (strings.HasPrefix(name, "AAAAAA+") || strings.HasPrefix(name, "AAAAAB+")) {
						t.Fatalf("import collision: %s", name)
					}
					generatedNames[name] = count
				}
			}
			if len(generatedNames) != 2 {
				t.Fatalf("distinct fonts: %v", names)
			}
			for _, embedded := range prepared {
				found := false
				for _, program := range programs {
					found = found || bytes.Equal(program, embedded.Program)
				}
				if !found {
					t.Fatal("embedded font program changed")
				}
			}
			if repeat == 1 && fmt.Sprint(previous) != fmt.Sprint(generatedNames) {
				t.Fatal("font allocation changed")
			}
			previous = generatedNames
		}
		if !reserve {
			firstNames = previous
			if firstNames["AAAAAA+NotoSans-Regular"] != 3 {
				t.Fatalf("missing collision control: %v", firstNames)
			}
		}
	}
}

func namedFont(t *testing.T, name string, noSubset bool) *typeset.Font {
	t.Helper()
	data := append([]byte(nil), defaultFont(t).Bytes()...)
	count := int(binary.BigEndian.Uint16(data[4:]))
	for i := range count {
		record := data[12+16*i:]
		offset := int(binary.BigEndian.Uint32(record[8:]))
		length := int(binary.BigEndian.Uint32(record[12:]))
		if noSubset && string(record[:4]) == "OS/2" {
			binary.BigEndian.PutUint16(data[offset+8:], 0x0100)
		}
		if string(record[:4]) != "name" {
			continue
		}
		table := data[offset : offset+length]
		storage := int(binary.BigEndian.Uint16(table[4:]))
		for j := range int(binary.BigEndian.Uint16(table[2:])) {
			entry := table[6+12*j:]
			if binary.BigEndian.Uint16(entry[6:]) != 6 {
				continue
			}
			platform := binary.BigEndian.Uint16(entry)
			replacement := []byte(name)
			if platform == 0 || platform == 3 {
				replacement = nil
				for _, c := range []byte(name) {
					replacement = append(replacement, 0, c)
				}
			}
			start := storage + int(binary.BigEndian.Uint16(entry[10:]))
			oldLength := int(binary.BigEndian.Uint16(entry[8:]))
			if len(replacement) > oldLength {
				t.Fatal("font test name exceeds allocated record")
			}
			replacementLength := len(replacement)
			if replacementLength > math.MaxUint16 {
				t.Fatal("font test name exceeds SFNT name record length")
				return nil
			}
			clear(table[start : start+oldLength])
			copy(table[start:], replacement)
			binary.BigEndian.PutUint16(entry[8:], uint16(replacementLength))
		}
	}
	font, err := typeset.LoadFont(data)
	if err != nil {
		t.Fatal(err)
	}
	if font.PostScriptName() != name {
		t.Fatalf("test font name: %q", font.PostScriptName())
	}
	return font
}

func noSubsetFont(t *testing.T, tagged bool) *typeset.Font {
	t.Helper()
	name := "NotoSans-Regular"
	if tagged {
		name = "ABCDEF+S-Regular"
	}
	return namedFont(t, name, true)
}

func assembleFontPages(t *testing.T, fonts []*typeset.Font) (string, map[typeset.Digest]typeset.EmbeddedFont) {
	t.Helper()
	recipe := packet.Job{}
	pages := map[int][]pdf.Page{}
	var placements []*typeset.Placed
	for i, font := range fonts {
		style := packet.Defaults()
		style.Size = packet.Size{Width: 300, Height: 200}
		recipe.Parts = append(recipe.Parts, packet.Part{Page: &style, Count: 1})
		pages[i] = []pdf.Page{textPage(t, font, "Ā Ελληνικά Кириллица")}
		placements = append(placements, pages[i][0].Text)
	}
	prepared, err := typeset.PrepareFonts(placements)
	if err != nil {
		t.Fatal(err)
	}
	layout, ds := packet.Resolve(recipe, nil, 0)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	var out bytes.Buffer
	if _, err = pdf.Assemble(t.Context(), layout, nil, nil, pages, nil, prepared, &out); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "packet.pdf")
	if err = os.WriteFile(path, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path, prepared
}

func TestGeneratedFullFontPrefixIsReservedBeforeSubsetAllocation(t *testing.T) {
	full := namedFont(t, "AAAAAA+S-Regular", true)
	subset := namedFont(t, "S-Regular", false)
	for _, fonts := range [][]*typeset.Font{{full, subset}, {subset, full}} {
		path, prepared := assembleFontPages(t, fonts)
		names, programs := finalFontFacts(t, path)
		if len(names) != 2 || names["AAAAAA+S-Regular"] != 3 || names["AAAAAB+S-Regular"] != 3 || len(programs) != 2 {
			t.Fatalf("generated font names collide: %v", names)
		}
		if !bytes.Equal(programs["AAAAAA+S-Regular"], full.Bytes()) || !bytes.Equal(programs["AAAAAB+S-Regular"], prepared[subset.Identity()].Program) {
			t.Fatal("font name refers to the wrong embedded program")
		}
		for _, embedded := range prepared {
			found := false
			for _, program := range programs {
				found = found || bytes.Equal(program, embedded.Program)
			}
			if !found {
				t.Fatal("font program changed during merge")
			}
		}
		for _, text := range extractRaw(t, path) {
			if strings.TrimSpace(text) != "Ā Ελληνικά Кириллица" {
				t.Fatalf("font extraction: %q", text)
			}
		}
	}
}

func TestOrdinaryHashFontNameSurvivesWritingAndMerge(t *testing.T) {
	for _, name := range []string{"Hash#Z-Regular", "Hash#4Z-Regular", "Hash#-Regular"} {
		for _, noSubset := range []bool{false, true} {
			font := namedFont(t, name, noSubset)
			path, prepared := assembleFontPages(t, []*typeset.Font{font})
			names, programs := finalFontFacts(t, path)
			expected := name
			if !noSubset {
				expected = "AAAAAA+" + name
			}
			if names[expected] != 3 || len(names) != 1 || len(programs) != 1 || !bytes.Equal(programs[expected], prepared[font.Identity()].Program) {
				t.Fatalf("literal hash font name/program changed: %v", names)
			}
			original, _ := writePDF(t, []pdf.Page{textPage(t, font, "Ā Ελληνικά")})
			names, programs = finalFontFacts(t, original)
			if names[name] != 3 || !bytes.Equal(programs[name], font.Bytes()) {
				t.Fatalf("original literal hash font changed: %v", names)
			}
		}
	}
}

func TestFullEmbeddingPreservesOriginalNameAndProgram(t *testing.T) {
	for _, tc := range []struct {
		font                   *typeset.Font
		tagged, pruningAllowed bool
	}{
		{defaultFont(t), false, true}, {noSubsetFont(t, false), false, false}, {noSubsetFont(t, true), true, false},
	} {
		font, tagged := tc.font, tc.tagged
		page := textPage(t, font, "Ā Ελληνικά Кириллица")
		prepared, err := typeset.PrepareFonts([]*typeset.Placed{page.Text})
		if err != nil {
			t.Fatal(err)
		}
		if prepared[font.Identity()].Subset != tc.pruningAllowed {
			t.Fatal("prepared subset status contradicts embedding permission")
		}
		for _, original := range []bool{false, true} {
			if tc.pruningAllowed && !original {
				continue
			}
			var out bytes.Buffer
			if original {
				_, err = pdf.Write(t.Context(), &out, []pdf.Page{page})
			} else {
				_, err = pdf.WriteRepeated(t.Context(), &out, page, 1, prepared)
			}
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "full.pdf")
			if err = os.WriteFile(path, out.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			names, programs := finalFontFacts(t, path)
			if len(names) != 1 || names[font.PostScriptName()] != 3 || len(programs) != 1 || !bytes.Equal(programs[font.PostScriptName()], font.Bytes()) {
				t.Fatalf("full font changed: %v", names)
			}
			if got := strings.TrimSpace(extractRaw(t, path)[0]); got != "Ā Ελληνικά Кириллица" {
				t.Fatalf("full extraction: %q", got)
			}
			tools := oracle.RequireTools(t)
			listing, err := exec.CommandContext(t.Context(), tools.PDFFonts, path).Output()
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, line := range strings.Split(string(listing), "\n") {
				fields := strings.Fields(line)
				if len(fields) > 7 && fields[0] == font.PostScriptName() {
					found = true
					if fields[4] != "yes" || fields[6] != "yes" || (!tagged && fields[5] != "no") {
						t.Fatalf("full embedding flags: %s", line)
					}
				}
			}
			if !found {
				t.Fatalf("full font missing from pdffonts: %s", listing)
			}
		}
	}
}

func TestHashEscapeFontNamesAreRefusedBeforePublication(t *testing.T) {
	for _, name := range []string{"Hash#41-Regular", "Hash#4a-Regular", "Hash#23-Regular", "Hash#41#Z", "Hash#00-Regular"} {
		font := namedFont(t, name, true)
		if err := pdf.ValidateFontName(font); err == nil {
			t.Fatalf("unsafe canonical name accepted: %s", name)
		}
		var raw bytes.Buffer
		if _, err := pdf.Write(t.Context(), &raw, []pdf.Page{textPage(t, font, "Ā Ελληνικά")}); err == nil || raw.Len() != 0 {
			t.Fatalf("direct writer did not refuse before writing: %v", err)
		}
		dir := t.TempDir()
		fontPath := filepath.Join(dir, "font.ttf")
		if err := os.WriteFile(fontPath, font.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(dir, "packet.pdf")
		sentinel := []byte("existing output")
		if err := os.WriteFile(output, sentinel, 0600); err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{"Ā Ελληνικά", ""} {
			recipe := map[string]any{"version": 3, "output": "packet.pdf", "items": []any{map[string]any{"page": map[string]any{"size": "A4", "text": map[string]any{"value": value, "font": map[string]any{"file": "font.ttf"}}}}}}
			data, err := json.Marshal(recipe)
			if err != nil {
				t.Fatal(err)
			}
			plan := filepath.Join(dir, "plan.json")
			if err = os.WriteFile(plan, data, 0600); err != nil {
				t.Fatal(err)
			}
			for _, command := range []string{"check", "build"} {
				result := core.Execute(t.Context(), core.NewRequest(core.Operation(command), core.FromPlan(core.FilePlan(plan)), core.Options{WorkingDir: dir, Overwrite: true}))
				if result.Status != "invalid" || result.Output.Published || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != diag.FontInvalid || result.Diagnostics[0].File != fontPath || !strings.Contains(result.Diagnostics[0].Message, "different PostScript name") {
					t.Fatalf("%s did not refuse name early: %+v", command, result)
				}
				if data, err = os.ReadFile(output); err != nil || !bytes.Equal(data, sentinel) {
					t.Fatalf("%s changed existing output: %v", command, err)
				}
			}
		}
	}
}

// fontTablesForDescriptor reads serialized sfnt offsets independently of typeset.
func fontTablesForDescriptor(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	tables := map[string][]byte{}
	for n := range int(binary.BigEndian.Uint16(data[4:])) {
		rec := data[12+16*n:]
		offset, length := binary.BigEndian.Uint32(rec[8:]), binary.BigEndian.Uint32(rec[12:])
		tables[string(rec[:4])] = data[offset : offset+length]
	}
	return tables
}

func TestDescriptorsPreserveIndependentOriginalTraitsAcrossEmbeddingModes(t *testing.T) {
	var fonts []*typeset.Font
	for index, tc := range []struct {
		angle, pitch       uint32
		version, selection uint16
	}{
		{0, 0, 4, 0}, {0, 0, 4, 1}, {0xfff38000, 0, 4, 1}, {0, 7, 4, 0}, {0xfff38000, 1, 3, 512}, {0xfff38000, 1, 4, 512},
	} {
		for _, full := range []bool{false, true} {
			font := namedFont(t, fmt.Sprintf("Traits%d%t", index, full), full)
			data := slices.Clone(font.Bytes())
			for n := range int(binary.BigEndian.Uint16(data[4:])) {
				record := data[12+16*n:]
				offset := int(binary.BigEndian.Uint32(record[8:]))
				switch string(record[:4]) {
				case "post":
					binary.BigEndian.PutUint32(data[offset+4:], tc.angle)
					binary.BigEndian.PutUint32(data[offset+12:], tc.pitch)
				case "OS/2":
					binary.BigEndian.PutUint16(data[offset:], tc.version)
					binary.BigEndian.PutUint16(data[offset+62:], tc.selection)
				}
			}
			font, err := typeset.LoadFont(data)
			if err != nil {
				t.Fatal(err)
			}
			fonts = append(fonts, font)
		}
	}
	path, prepared := assembleFontPages(t, fonts)
	_, programs := finalFontFacts(t, path)
	if len(programs) != len(fonts) || len(prepared) != len(fonts) {
		t.Fatalf("mixed resources %d/%d", len(programs), len(prepared))
	}
	text := string(run(t, "pdftotext", path, "-"))
	if strings.Count(text, "Ā Ελληνικά Кириллица") != len(fonts) {
		t.Fatalf("glyph extraction changed: %q", text)
	}

	for _, font := range fonts {
		original := fontTablesForDescriptor(t, font.Bytes())
		embedded := fontTablesForDescriptor(t, prepared[font.Identity()].Program)
		if !bytes.Equal(original["post"][4:32], embedded["post"][4:32]) || !bytes.Equal(original["OS/2"], embedded["OS/2"]) {
			t.Fatal("pruning changed original traits")
		}
	}
	// All variants have identical glyph programs/placement, so changing only traits
	// must preserve actual rendered pixels in full and pruned resources.
	tools := oracle.RequireTools(t)
	dir := t.TempDir()
	render := exec.CommandContext(t.Context(), tools.PDFToPPM, "-scale-to", "500", "-png", path, filepath.Join(dir, "traits"))
	if data, err := render.CombinedOutput(); err != nil {
		t.Fatalf("render: %s %v", data, err)
	}
	images, err := filepath.Glob(filepath.Join(dir, "traits-*.png"))
	if err != nil || len(images) != len(fonts) {
		t.Fatalf("render discovery %v %v", images, err)
	}
	var reference []byte
	for _, image := range images {
		pixels, err := os.ReadFile(image)
		if err != nil {
			t.Fatal(err)
		}
		if reference == nil {
			reference = pixels
		} else if !bytes.Equal(reference, pixels) {
			t.Fatalf("trait metadata changed raster: %s", image)
		}
	}

}
