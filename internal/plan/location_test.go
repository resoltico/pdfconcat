// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

func TestOffsetsLinesAndColumnsIncludeTheBOM(t *testing.T) {
	t.Parallel()

	document := "\xef\xbb\xbf{\n  \"version\": 1,\r\n  \"items\": [\n    \"a.pdf\",\n" +
		"    {\"blank\": {\"text\": {\"size\": 12}}, \"count\": 3}\n  ]\n}\n"
	job := mustDecode(t, document)

	check := func(origin assembly.Origin, member string, line, column int, prefix string) {
		t.Helper()

		if !strings.HasPrefix(document[origin.Offset:], prefix) {
			t.Fatalf(
				"offset %d does not start with %q: %q",
				origin.Offset,
				prefix,
				document[origin.Offset:min(int(origin.Offset)+10, len(document))],
			)
		}

		location := assembly.Locate(job.Source, origin, member)
		if location.Line != line || location.Column != column || location.Source != inputName {
			t.Fatalf("Locate(%d) = %+v, want %d:%d", origin.Offset, location, line, column)
		}
	}

	check(job.Origin, "", 1, 1, "{")
	check(job.Items[0].Origin, "", 4, 5, sourceStringJSON)
	check(job.Items[1].Origin, "", 5, 5, `{"blank"`)
	check(job.Items[1].Blank.CountOrigin, "/count", 5, 48, "3}")
	check(job.Items[1].Blank.Style.Text.Size.Origin, textSizePointer, 5, 33, "12}")

	location := assembly.Locate(job.Source, job.Items[1].Blank.CountOrigin, "/count")
	if location.Pointer != "/items/1/count" {
		t.Fatalf("pointer = %q", location.Pointer)
	}

	if text := location.String(); !strings.Contains(text, "plan.json:5:48 (byte") || !strings.Contains(text, "at /items/1/count") {
		t.Fatalf("String() = %q", text)
	}
}

func TestPositionsAcrossIndexBlocksAndOutOfRange(t *testing.T) {
	t.Parallel()

	// More than one 4 KiB index block, with a newline in every block.
	document := `{"version":1,"items":[` + strings.Repeat("\n\"a.pdf\",", 2000) + "\n\"z.pdf\"]}"
	job := mustDecode(t, document)

	last := job.Items[len(job.Items)-1]
	location := assembly.Locate(job.Source, last.Origin, "")

	if location.Line != 2002 || location.Column != 1 {
		t.Fatalf("last item at %d:%d, want 2002:1", location.Line, location.Column)
	}

	far := assembly.Locate(job.Source, assembly.Origin{Offset: 1 << 40}, "")
	if far.Line != 2002 {
		t.Fatalf("an offset past the end clamps to the last line, got %d", far.Line)
	}

	before := assembly.Locate(job.Source, assembly.Origin{Offset: -5}, "")
	if before.Line != 1 || before.Column != 1 {
		t.Fatalf("a negative offset clamps to the start, got %+v", before)
	}
}

func TestProvenancePointersMatchTheGenerator(t *testing.T) {
	t.Parallel()

	// Independent oracle: the generator knows each leaf's pointer and path by construction.
	document, want := generatedPlan()
	job := mustDecode(t, document)

	seen := 0

	for _, leaf := range leaves(job.Items) {
		pointer := assembly.Locate(job.Source, leaf.Origin, "").Pointer
		if want[pointer] != leaf.Path {
			t.Fatalf("%s -> %q, want %q", pointer, leaf.Path, want[pointer])
		}

		if !strings.HasPrefix(document[leaf.Origin.Offset:], `"`+leaf.Path+`"`) {
			t.Fatalf("offset %d for %s does not point at the item", leaf.Origin.Offset, pointer)
		}

		seen++
	}

	if seen != len(want) {
		t.Fatalf("visited %d leaves, want %d", seen, len(want))
	}
}

// generatedPlan writes a plan of 50 entries, every fifth a nested group, and the pointer and path of each PDF.
func generatedPlan() (string, map[string]string) {
	var builder strings.Builder

	want := map[string]string{}

	builder.WriteString(`{"version":1,"items":[`)

	for index := range 50 {
		if index > 0 {
			builder.WriteString(",")
		}

		if index%5 != 0 {
			fmt.Fprintf(&builder, `"f%d.pdf"`, index)
			want[fmt.Sprintf("/items/%d", index)] = fmt.Sprintf("f%d.pdf", index)

			continue
		}

		fmt.Fprintf(&builder, `{"dir":"g%d","items":["x.pdf",{"dir":"h","items":["y%d.pdf"]},"z.pdf"]}`, index, index)
		want[fmt.Sprintf("/items/%d/items/0", index)] = "x.pdf"
		want[fmt.Sprintf("/items/%d/items/1/items/0", index)] = fmt.Sprintf("y%d.pdf", index)
		want[fmt.Sprintf("/items/%d/items/2", index)] = "z.pdf"
	}

	builder.WriteString(`]}`)

	return builder.String(), want
}

// leaves lists the PDF items below items, in order.
func leaves(items []assembly.Item) []assembly.Item {
	var found []assembly.Item

	for _, item := range items {
		if item.Kind == assembly.ItemGroup {
			found = append(found, leaves(item.Items)...)
		} else {
			found = append(found, item)
		}
	}

	return found
}

func TestEverySourceRefIsUnique(t *testing.T) {
	t.Parallel()

	job := mustDecode(t, `{"version":1,"items":["a",{"blank":{}},{"dir":"d","items":["b","c"]},"e"]}`)

	seen := map[assembly.Ref]bool{job.Origin.Ref: true}

	var visit func(items []assembly.Item)

	visit = func(items []assembly.Item) {
		for _, item := range items {
			if seen[item.Origin.Ref] {
				t.Fatalf("ref %d reused", item.Origin.Ref)
			}

			seen[item.Origin.Ref] = true

			visit(item.Items)
		}
	}

	visit(job.Items)

	if len(seen) != 7 {
		t.Fatalf("%d refs, want 7", len(seen))
	}
}
