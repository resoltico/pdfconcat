// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly_test

import (
	"errors"
	"math"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

const (
	// textBlanks is the number of blanks of three nodes each (item, blank object, text object) that leave
	// room for exactly one more node under MaxNodes: 3 * 83,333 = 249,999.
	textBlanks      = 83333
	nodesBeforeTail = 3 * textBlanks
)

// plainBlanks returns count blanks that carry no style.
func plainBlanks(tb testing.TB, count int) []assembly.Item {
	tb.Helper()

	var style assembly.BlankStyle

	items := make([]assembly.Item, count)
	for index := range items {
		items[index] = mustBlankItem(tb, assembly.Ref(index+1), &style, 1)
	}

	return items
}

func TestValidateBoundsContributionsExactly(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name    string
		blanks  int
		wantErr bool
	}{
		{"at the limit", assembly.MaxContributions, false},
		{"one above the limit", assembly.MaxContributions + 1, true},
	}

	for _, row := range rows {
		err := argumentJob(plainBlanks(t, row.blanks)...).Validate()
		if (err != nil) != row.wantErr || (err != nil && !errors.Is(err, assembly.ErrInvalidJob)) {
			t.Errorf("%s: Validate() = %v", row.name, err)
		}
	}
}

func TestValidateCountsStructuralNodesExactly(t *testing.T) {
	t.Parallel()

	// A text blank occupies three nodes: the item, its blank object and its text object. Naming the built-in
	// font explicitly adds no font object, because only a file font has one.
	textBlank := assembly.BlankStyle{Text: assembly.TextStyle{
		Value: assembly.Set("x", itemOrigin()),
		Font:  assembly.Set(assembly.Font{}, itemOrigin()),
	}}
	defaults := sizeStyle(1, 1)

	rows := []struct {
		defaults *assembly.BlankStyle
		name     string
		tail     int // PDF items after the text blanks, one node each
		wantErr  bool
	}{
		{nil, "at the limit without defaults", assembly.MaxNodes - nodesBeforeTail, false},
		{nil, "above the limit without defaults", assembly.MaxNodes - nodesBeforeTail + 1, true},
		{&defaults, "defaults take one node, at the limit", assembly.MaxNodes - nodesBeforeTail - 1, false},
		{&defaults, "defaults take one node, above the limit", assembly.MaxNodes - nodesBeforeTail, true},
	}

	for _, row := range rows {
		items := make([]assembly.Item, 0, textBlanks+row.tail)

		for index := range textBlanks {
			items = append(items, mustBlankItem(t, assembly.Ref(index+1), &textBlank, 1))
		}

		for index := range row.tail {
			items = append(items, pdfAt(assembly.Ref(textBlanks+index+1), aPath))
		}

		job := argumentJob(items...)
		if row.defaults != nil {
			job.Defaults = *row.defaults
		}

		err := job.Validate()
		if (err != nil) != row.wantErr || (err != nil && !errors.Is(err, assembly.ErrInvalidJob)) {
			t.Errorf("%s: Validate() = %v", row.name, err)
		}
	}
}

func TestValidateAcceptsLeadingAtItsBounds(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name    string
		leading float64
		wantErr bool
	}{
		{"minimum", assembly.MinLeading, false},
		{"maximum", assembly.MaxLeading, false},
		{"below the minimum", math.Nextafter(assembly.MinLeading, 0), true},
		{"above the maximum", math.Nextafter(assembly.MaxLeading, math.Inf(1)), true},
	}

	for _, row := range rows {
		style := assembly.BlankStyle{Text: assembly.TextStyle{Leading: assembly.Set(row.leading, itemOrigin())}}

		err := argumentJob(mustBlankItem(t, 1, &style, 1)).Validate()
		if (err != nil) != row.wantErr || (err != nil && !errors.Is(err, assembly.ErrOutOfRange)) {
			t.Errorf("%s: leading %v: Validate() = %v", row.name, row.leading, err)
		}
	}
}

func TestNewOperandJobAcceptsTheLargestPosition(t *testing.T) {
	t.Parallel()

	job, err := assembly.NewOperandJob(hostPath(workingDirectory), []assembly.Operand{{Position: math.MaxUint32, Path: aPath}})
	if err != nil {
		t.Fatal(err)
	}

	if got := job.Items[0].Origin.Ref; got != math.MaxUint32 {
		t.Errorf("origin ref %d, want %d", got, uint32(math.MaxUint32))
	}
}
