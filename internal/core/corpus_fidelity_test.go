// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/oracle"
)

// These observations read final objects/streams through qpdf, never a backend
// helper or a result DTO. Object numbers may change; programs and resource facts
// must not. The compared original is the isolated staged producer input.
func corpusCFFPrograms(t *testing.T, tools oracle.Tools, path string) []string {
	t.Helper()
	raw, err := oracle.QPDFJSON(tools, path)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	var digests []string
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if ref, ok := v["/FontFile3"].(string); ok {
				parts := strings.Fields(ref)
				if len(parts) != 3 {
					t.Fatalf("invalid font program reference %q", ref)
				}
				program, err := exec.CommandContext(t.Context(), tools.QPDF, "--show-object="+parts[0]+","+parts[1], "--filtered-stream-data", path).Output()
				if err != nil {
					t.Fatal(err)
				}
				digests = append(digests, fmt.Sprintf("%x", sha256.Sum256(program)))
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(document)
	slices.Sort(digests)
	return digests
}

type corpusFormFact struct {
	Name       string `json:"fullname"`
	Type       string `json:"fieldtype"`
	Value      any    `json:"value"`
	Page       int    `json:"pageposfrom1"`
	Annotation struct {
		Flags int    `json:"annotationflags"`
		State string `json:"appearancestate"`
	} `json:"annotation"`
}

func corpusFormFacts(t *testing.T, tools oracle.Tools, path string) []corpusFormFact {
	t.Helper()
	raw, err := oracle.QPDFJSON(tools, path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Form struct {
			Fields []corpusFormFact `json:"fields"`
		} `json:"acroform"`
	}
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	return document.Form.Fields
}
func corpusImageFacts(t *testing.T, tools oracle.Tools, path string, page int) []string {
	t.Helper()
	raw, err := oracle.QPDFJSON(tools, path)
	if err != nil {
		t.Fatal(err)
	}
	// qpdf names these dimensions in lowercase; v2 does not fold keys.
	var all map[string]any
	if err = json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	pages, ok := all["pages"].([]any)
	if !ok || page < 1 || page > len(pages) {
		t.Fatal("image page missing")
	}
	entry, ok := pages[page-1].(map[string]any)
	if !ok {
		t.Fatal("image page not an object")
	}
	images, ok := entry["images"].([]any)
	if !ok {
		t.Fatal("image inventory missing")
	}
	var facts []string
	for _, image := range images {
		v, ok := image.(map[string]any)
		if !ok {
			t.Fatal("image inventory entry")
		}
		fact := map[string]any{}
		for _, key := range []string{"width", "height", "bitspercomponent", "colorspace", "filter"} {
			fact[key] = v[key]
		}
		data, err := json.Marshal(fact, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		facts = append(facts, string(data))
	}
	slices.Sort(facts)
	return facts
}

func corpusVisibleFidelity(actual, original *oracle.Document, firstOutput int) error {
	before, err := original.RawTexts()
	if err != nil {
		return err
	}
	after, err := actual.RawTexts()
	if err != nil {
		return err
	}
	for n, text := range before {
		page := firstOutput + n
		if page > len(after) || after[page-1] != text {
			return fmt.Errorf("complete producer text differs on output page %d", page)
		}
		difference, err := actual.AppearanceDifference(page, original, n+1, 0)
		if err != nil {
			return err
		}
		if difference != 0 {
			return fmt.Errorf("complete producer raster differs on output page %d: %g", page, difference)
		}
	}
	return nil
}
func corpusPassThroughFidelity(t *testing.T, tools oracle.Tools, output, source string, firstOutput int) {
	t.Helper()
	actual, err := oracle.Load(tools, output)
	if err != nil {
		t.Fatal(err)
	}
	original, err := oracle.Load(tools, source)
	if err != nil {
		t.Fatal(err)
	}
	if err = corpusVisibleFidelity(actual, original, firstOutput); err != nil {
		t.Fatal(err)
	}
	for n := range original.PageCount() {
		page := firstOutput + n
		if want := corpusImageFacts(t, tools, source, n+1); !slices.Equal(corpusImageFacts(t, tools, output, page), want) {
			t.Fatalf("actual output image/filter/color facts differ on page %d", page)
		}
	}
}
func corpusProducerResources(t *testing.T, tools oracle.Tools, output, source string) {
	t.Helper()
	original, err := oracle.Load(tools, source)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := oracle.Load(tools, output)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := original.OutlineTree()
	if err != nil {
		t.Fatal(err)
	}
	if err = actual.CheckOutlineTree(expected); err != nil {
		t.Fatal("producer native navigation not preserved", err)
	}
	if !reflect.DeepEqual(corpusFormFacts(t, tools, source), corpusFormFacts(t, tools, output)) {
		t.Fatal("canonical producer field values/ownership/state changed")
	}
	before := corpusCFFPrograms(t, tools, source)
	after := corpusCFFPrograms(t, tools, output)
	if !slices.Equal(before, after) {
		t.Fatal("actual output CFF programs differ", before, after)
	}
	if len(before) > 0 {
		fonts, err := exec.CommandContext(t.Context(), tools.PDFFonts, output).Output()
		if err != nil || !strings.Contains(string(fonts), "Type 0C") && !strings.Contains(string(fonts), "Type 1C") {
			t.Fatal("actual output lost CFF font resources", string(fonts), err)
		}
	}
}
