// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

const (
	byteOrderMark = "\xef\xbb\xbf"
	// indexBlock is the size of the block the line index works in.
	indexBlock = 4096
	// planHead and planTail surround the whitespace that padding adds to a plan.
	planHead = `{"version":1,"items":[`
	planTail = `"a.pdf"]}`
	// longMember is longer than the room a pointer reserves for its steps.
	longMember = "/blank/text/font/file/a-member-name-longer-than-sixteen-bytes"
)

// positionPlan builds a valid plan of exactly size bytes whose padding whitespace puts a line feed at each of the
// given absolute offsets and spaces everywhere else.
func positionPlan(tb testing.TB, size int, mark string, newlines []int) string {
	tb.Helper()

	head := mark + planHead

	padding := []byte(strings.Repeat(" ", size-len(head)-len(planTail)))

	for _, offset := range newlines {
		index := offset - len(head)
		if index < 0 || index >= len(padding) {
			tb.Fatalf("a line feed at offset %d is outside the padding of a %d-byte plan", offset, size)
		}

		padding[index] = '\n'
	}

	return head + string(padding) + planTail
}

// wantPosition is the line and column of an offset worked out directly from the text: lines count line feeds,
// and columns count bytes from the line start, a leading byte order mark excluded.
func wantPosition(document string, bomLength int, offset int64) (int, int) {
	offset = max(0, min(offset, int64(len(document))))
	before := document[:offset]
	lineStart := int64(bomLength)

	if last := strings.LastIndexByte(before, '\n'); last >= 0 {
		lineStart = int64(last) + 1
	}

	return strings.Count(before, "\n") + 1, int(max(offset-lineStart, 0)) + 1
}

// TestEveryOffsetHasItsLineAndColumn checks every offset of plans whose size and line feeds sit on and next to the
// edges of the line index blocks.
func TestEveryOffsetHasItsLineAndColumn(t *testing.T) {
	t.Parallel()

	sizes := []int{indexBlock - 1, indexBlock, indexBlock + 1, 2*indexBlock - 1, 2 * indexBlock, 2*indexBlock + 1}
	feeds := [][]int{
		nil,
		{indexBlock},
		{indexBlock - 1},
		{100},
		{100, 5000},
		{5000},
		{indexBlock, 2*indexBlock - 1},
		{indexBlock + 1, 2 * indexBlock},
	}

	for _, size := range sizes {
		for _, newlines := range feeds {
			for _, mark := range []string{"", byteOrderMark} {
				if slices.ContainsFunc(newlines, func(offset int) bool { return offset >= size-len(planTail) }) {
					continue
				}

				t.Run(fmt.Sprintf("size %d feeds %v mark %q", size, newlines, mark), func(t *testing.T) {
					t.Parallel()

					checkEveryPosition(t, positionPlan(t, size, mark, newlines), mark)
				})
			}
		}
	}
}

func checkEveryPosition(tb testing.TB, document, mark string) {
	tb.Helper()

	job := mustDecode(tb, document)

	for offset := range int64(len(document) + 2) {
		location := assembly.Locate(job.Source, assembly.Origin{Offset: offset}, "")
		line, column := wantPosition(document, len(mark), offset)

		if location.Line != line || location.Column != column {
			tb.Fatalf("offset %d of %d is %d:%d, want %d:%d", offset, len(document), location.Line, location.Column, line, column)
		}
	}
}

func TestPointersAppendLongMembers(t *testing.T) {
	t.Parallel()

	job := mustDecode(t, `{"version":1,"items":["a",{"dir":"d","items":["b","c"]}]}`)
	group := job.Items[1]

	rows := []struct {
		name   string
		member string
		want   string
		origin assembly.Origin
	}{
		{"root", "", "", assembly.Origin{}},
		{"root with member", rootMember, rootMember, assembly.Origin{}},
		{"item", "", "/items/0", job.Items[0].Origin},
		{"item with long member", longMember, "/items/0" + longMember, job.Items[0].Origin},
		{"group", "", "/items/1", group.Origin},
		{"nested item", "", "/items/1/items/1", group.Items[1].Origin},
		{"nested item with long member", longMember, "/items/1/items/1" + longMember, group.Items[1].Origin},
	}

	for _, row := range rows {
		got := assembly.Locate(job.Source, row.origin, row.member).Pointer
		if got != row.want {
			t.Errorf("%s: pointer %q, want %q", row.name, got, row.want)
		}
	}
}
