// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

type fitGroupPresenceCase struct {
	name, context, entry, extra string
	noFitCode                   Code
	reject                      bool
}

const (
	guardGroupPage          = "page"
	guardGroupForm          = "form"
	guardEmptyGroupEntry    = "/Group <<>>"
	guardIndirectGroupEntry = "/Group 6 0 R"
	guardSquareFill         = "0 0 1 1 re f"
)

func groupPresenceCases() []fitGroupPresenceCase {
	return []fitGroupPresenceCase{
		{name: "page-empty", context: guardGroupPage, entry: guardEmptyGroupEntry, reject: true, noFitCode: CodeInvalid},
		{
			name:      "page-indirect-empty",
			context:   guardGroupPage,
			entry:     guardIndirectGroupEntry,
			extra:     "<<>>",
			reject:    true,
			noFitCode: CodeInvalid,
		},
		{name: "page-null", context: guardGroupPage, entry: "/Group null"},
		{name: "page-indirect-null", context: guardGroupPage, entry: guardIndirectGroupEntry, extra: nullPDFObject},
		{name: "page-undefined", context: guardGroupPage, entry: "/Group 999 0 R"},
		{
			name:    "page-optional-null-fields",
			context: guardGroupPage,
			entry:   "/Group << /S /Transparency /Type 6 0 R /I 6 0 R /K 6 0 R /CS 6 0 R >>",
			extra:   nullPDFObject,
		},
		{
			name:      "page-required-null-S",
			context:   guardGroupPage,
			entry:     "/Group << /S 6 0 R >>",
			extra:     nullPDFObject,
			reject:    true,
			noFitCode: CodeInvalid,
		},
		{name: "page-alias", context: guardGroupPage, entry: guardIndirectGroupEntry, extra: "7 0 R", reject: true, noFitCode: CodeInvalid},
		{
			name:      "page-cycle",
			context:   guardGroupPage,
			entry:     guardIndirectGroupEntry,
			extra:     sourceSixReference,
			reject:    true,
			noFitCode: CodeInvalid,
		},
		{name: "form-empty", context: guardGroupForm, entry: guardEmptyGroupEntry, reject: true, noFitCode: CodeInvalid},
		{name: "form-indirect-null", context: guardGroupForm, entry: guardIndirectGroupEntry, extra: nullPDFObject},
		{name: "alpha-missing-group", context: fitMaskAlpha, reject: true},
		{name: "alpha-indirect-null-group", context: fitMaskAlpha, entry: "/Group 7 0 R", extra: nullPDFObject, reject: true},
		{name: "alpha-empty-group", context: fitMaskAlpha, entry: guardEmptyGroupEntry, reject: true, noFitCode: CodeInvalid},
		{name: "alpha-minimal-group", context: fitMaskAlpha, entry: "/Group << /S /Transparency >>"},
		{name: "alpha-optional-null-CS", context: fitMaskAlpha, entry: "/Group << /S /Transparency /CS 7 0 R >>", extra: nullPDFObject},
		{name: "luminosity-missing-CS", context: guardLuminosityMask, entry: "/Group << /S /Transparency >>", reject: true},
		{
			name:    "luminosity-indirect-null-CS",
			context: guardLuminosityMask,
			entry:   "/Group << /S /Transparency /CS 7 0 R >>",
			extra:   nullPDFObject,
			reject:  true,
		},
		{name: "luminosity-valid-CS", context: guardLuminosityMask, entry: "/Group << /S /Transparency /CS /DeviceRGB >>"},
	}
}

func canonicalGroupNameCases() []fitGroupPresenceCase {
	return []fitGroupPresenceCase{
		{name: "page-genuine-escaped-S", context: guardGroupPage, entry: "/Group << /S /Trans#70arency >>"},
		{
			name:      "page-literal-hash-S",
			context:   guardGroupPage,
			entry:     "/Group << /S /Trans#2370arency >>",
			reject:    true,
			noFitCode: CodeInvalid,
		},
		{name: "page-genuine-escaped-Type", context: guardGroupPage, entry: "/Group << /S /Transparency /Type /Gro#75p >>"},
		{
			name:      "page-literal-hash-Type",
			context:   guardGroupPage,
			entry:     "/Group << /S /Transparency /Type /Gro#2375p >>",
			reject:    true,
			noFitCode: CodeInvalid,
		},
	}
}

func groupPresenceFixture(test fitGroupPresenceCase) *pdffixture.Doc {
	extra := test.extra
	if extra == "" {
		extra = nullPDFObject
	}

	objects := []string{
		catalog,
		guardSinglePageTree,
		"",
		groupPresenceStream("", guardSquareFill),
		nullPDFObject,
		extra,
		"<< /S /Transparency >>",
	}
	resources := "<<>>"
	pageEntry := ""

	switch test.context {
	case guardGroupPage:
		pageEntry = test.entry
	case guardGroupForm:
		resources = "<< /XObject << /F 5 0 R >> >>"
		objects[3] = groupPresenceStream("", "/F Do")
		objects[4] = groupPresenceStream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources <<>> "+test.entry, guardSquareFill)
	case fitMaskAlpha, guardLuminosityMask:
		resources = "<< /ExtGState << /GS 5 0 R >> >>"
		objects[3] = groupPresenceStream("", "/GS gs 0 0 1 1 re f")
		objects[4] = "<< /Type /ExtGState /SMask << /S /" + test.context + " /G 6 0 R >> >>"
		objects[5] = groupPresenceStream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources <<>> "+test.entry, guardSquareFill)
		objects[6] = extra
	default:
		panic("unrecognized Group fixture context")
	}

	objects[2] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources " + resources + " /Contents 4 0 R " + pageEntry + " >>"

	return rawDoc(objects...)
}

func groupPresenceStream(dict, program string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(program), program)
}

func TestFitResolvedGroupPresenceAtSerializedInspectAndDefensiveImport(t *testing.T) {
	t.Parallel()

	for _, test := range append(groupPresenceCases(), canonicalGroupNameCases()...) {
		for _, target := range []PageSize{{210 * 72 / 25.4, 297 * 72 / 25.4}, {612, 1008}} {
			t.Run(test.name+"/"+targetName(target), func(t *testing.T) { t.Parallel(); checkSerializedGroupPresence(t, test, target) })
		}
	}
}

func checkSerializedGroupPresence(t *testing.T, test fitGroupPresenceCase, target PageSize) {
	t.Helper()

	dir := t.TempDir()

	path := filepath.Join(dir, "group.pdf")
	if err := groupPresenceFixture(test).WriteFile(path); err != nil {
		t.Fatal(err)
	}

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	_, err = engine.Inspect(t.Context(), path, nil)
	if CodeOf(err) != test.noFitCode {
		t.Fatalf("changed ordinary native Group behavior: want=%s got=%v", test.noFitCode, err)
	}

	_, err = engine.Inspect(t.Context(), path, &target)
	if test.reject {
		if CodeOf(err) != CodeFitUnsupported {
			t.Fatalf("shared Group preflight refused after wrong phase: %v", err)
		}
	} else if err != nil {
		t.Fatalf("valid resolved Group presence refused: %v", err)
	}

	checkDefensiveGroupPresence(t, engine, path, test, target)
}

func checkDefensiveGroupPresence(t *testing.T, engine *Engine, path string, test fitGroupPresenceCase, target PageSize) {
	t.Helper()

	sibling := test
	sibling.entry = "/Group << /S /Transparency /CS /DeviceRGB >>"
	sibling.extra = nullPDFObject

	validPath := filepath.Join(t.TempDir(), "valid.pdf")
	if err := groupPresenceFixture(sibling).WriteFile(validPath); err != nil {
		t.Fatal(err)
	}

	info, err := engine.Inspect(t.Context(), validPath, nil)
	if err != nil {
		t.Fatal(err)
	}

	fit, err := CanvasFit(PageSize{100, 100}, target)
	if err != nil {
		t.Fatal(err)
	}

	info.Fits = []FitRange{{First: 1, Last: 1, Fit: fit}}
	destination := filepath.Join(t.TempDir(), "seed.pdf")

	const seed = "preserved Group failure destination"
	if err = os.WriteFile(destination, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	request := AssembleRequest{
		FitTarget:     &target,
		Sources:       []SourceFile{{Path: path, Info: info}},
		Order:         []Run{SourcePages(0, 1, 1)},
		ExpectedPages: 1,
		Destination:   destination,
	}

	err = engine.Assemble(t.Context(), &request)
	if test.reject {
		if CodeOf(err) != CodeFitUnsupported {
			t.Fatalf("defensive Group import did not reread unsupported source: %v", err)
		}

		output, readErr := os.ReadFile(filepath.Clean(destination))
		if readErr != nil || string(output) != seed {
			t.Fatalf("Group refusal changed seeded target: %q %v", output, readErr)
		}
	} else if err != nil {
		t.Fatalf("valid Group import refused: %v", err)
	}
}

func TestFitPageGroupPresenceRefusalPrecedesMissingContent(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)

	fit, err := CanvasFit(PageSize{100, 100}, PageSize{612, 1008})
	if err != nil {
		t.Fatal(err)
	}

	page := types.Dict{fitGroup: types.Dict{}, keyResources: types.Dict{}}

	err = newFitProgramInspector(pdf).inspectPage(t.Context(), page, types.Dict{}, fit)
	if !errors.Is(err, errFitUnsupported) || !strings.Contains(err.Error(), "source page /Group") {
		t.Fatalf("blank page approved malformed Group before content: %v", err)
	}
}
