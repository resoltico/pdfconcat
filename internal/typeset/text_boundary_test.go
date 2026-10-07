// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package typeset_test

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

func TestTextErrorMessageListsEveryProblem(t *testing.T) {
	t.Parallel()

	err := &typeset.TextError{Problems: []typeset.Problem{
		{Offset: -1, Rune: utf8.RuneError, Reason: "bad bytes"},
		{Offset: 0, Rune: 'A', Reason: "first"},
		{Offset: 5, Rune: 'B', More: 2, Reason: "second"},
	}}

	got := err.Error()
	prefix := "text cannot be rendered: bad bytes; U+0041 at rune 0: first; U+0042 at rune 5 (+2 more): second ("

	if !strings.HasPrefix(got, prefix) || !strings.HasSuffix(got, ")") || !strings.Contains(got, "supported: left-to-right Latin") {
		t.Errorf(messageValueFormat, got)
	}
}

func TestInvalidUTF8IsReportedWithoutPosition(t *testing.T) {
	t.Parallel()

	var shaper typeset.Shaper

	_, err := shaper.Place(defaultFont(t), baseParams("a\xffb"))

	textErr, ok := errors.AsType[*typeset.TextError](err)
	if !ok || len(textErr.Problems) != 1 {
		t.Fatalf(gotValueFormat, err)
	}

	if problem := textErr.Problems[0]; problem.Offset != -1 || problem.Rune != utf8.RuneError || problem.More != 0 {
		t.Errorf("problem %+v", problem)
	}

	if !strings.HasPrefix(err.Error(), "text cannot be rendered: text is not valid UTF-8 (supported:") {
		t.Errorf(messageValueFormat, err)
	}
}

func TestRightToLeftRunIsLocatedInTheText(t *testing.T) {
	t.Parallel()

	var shaper typeset.Shaper

	// The Arabic question mark has script Common but a right-to-left direction; it follows "abc\n" and "xx ".
	_, err := shaper.Place(defaultFont(t), baseParams("abc\nxx ؟"))

	textErr, ok := errors.AsType[*typeset.TextError](err)
	if !ok || len(textErr.Problems) != 1 {
		t.Fatalf(gotValueFormat, err)
	}

	if problem := textErr.Problems[0]; problem.Offset != 7 || problem.Rune != '؟' {
		t.Errorf("problem %+v", problem)
	}
}
