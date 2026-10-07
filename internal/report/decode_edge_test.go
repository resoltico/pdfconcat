// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

const (
	nullToken   = "null"
	pointerKind = "/kind"
)

// decodeFault decodes a document that must be refused and returns the refusal.
func decodeFault(t *testing.T, doc string) *report.Error {
	t.Helper()

	_, err := decodeText(t, doc)

	found, ok := report.AsError(err)
	if !ok {
		t.Fatalf("%v is not a report error", err)
	}

	return found
}

func TestFaultsAreLocatedAtTheFirstByteOfTheirTokenPastSpaces(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		doc  string
		code report.Code
		want string // the text that starts at the byte the fault must be located at
	}{
		{"null after a colon and a space", replaceOnce(t, completeCheck, memberStatusOK, `"status": null`), report.CodeNull, nullToken},
		{
			"null after a comma and spaces",
			replaceOnce(t, completeCheck, `"status":"ok",`, `"status":null,  `),
			report.CodeNull,
			nullToken,
		},
		{
			"dangling reference after a comma and a space", replaceOnce(t, completeCheck, memberFontZeroEnd, ` "font":4}`),
			report.CodeDanglingReference, `"font"`,
		},
	} {
		found := decodeFault(t, tc.doc)
		location := found.Diagnostic.Location

		if found.Diagnostic.Code != tc.code || location == nil || location.Offset == nil {
			t.Errorf(namedErrorFormat, tc.name, found)

			continue
		}

		if want := int64(strings.Index(tc.doc, tc.want)); *location.Offset != want {
			t.Errorf("%s: located at byte %d, want %d", tc.name, *location.Offset, want)
		}
	}
}

func TestTextThatIsNotJSONIsASyntaxErrorNotAnEmptyReport(t *testing.T) {
	t.Parallel()

	for _, doc := range []string{"@", "x{}", "}"} {
		if code := decodeFault(t, doc).Diagnostic.Code; code != report.CodeSyntax {
			t.Errorf("%q: code %s", doc, code)
		}
	}
}

func TestFirstMissingMemberIsReported(t *testing.T) {
	t.Parallel()

	doc := replaceOnce(t, completeCheck, `"kind":"report","status":"ok",`, ``)
	found := decodeFault(t, doc)

	namesFirst := strings.Contains(found.Diagnostic.Message, `"kind"`)
	namesSecond := strings.Contains(found.Diagnostic.Message, `"status"`)

	if found.Diagnostic.Code != report.CodeMissingMember || !namesFirst || namesSecond {
		t.Errorf("the first of two missing members must be the one named: %v", found)
	}
}

func TestKindThatIsNotAStringIsAWrongType(t *testing.T) {
	t.Parallel()

	found := decodeFault(t, replaceOnce(t, completeCheck, `"kind":"report"`, `"kind":5`))
	location := found.Diagnostic.Location

	if found.Diagnostic.Code != report.CodeWrongType || location == nil || location.Pointer != pointerKind {
		t.Errorf("a number is not another kind of report, it is the wrong type: %v", found)
	}
}
