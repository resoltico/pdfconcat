// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package diag

import (
	"strings"
	"testing"
)

func TestAppearanceKindsOwnWholeSentences(t *testing.T) {
	cases := []struct {
		kind AnnotationIssueKind
		word string
	}{{RotatedAppearanceMissing, "NoRotate"}, {TextIconAppearanceMissing, "Text icon"}, {CaretAppearanceMissing, "Caret"}, {CaptionedLineAppearanceMissing, "captioned Line"}, {FreeTextAppearanceMissing, "FreeText"}, {InteractiveAreaCrossesFrame, "Interactive annotation hit area"}, {ArtworkCrossesFrame, "viewer-generated artwork"}}
	for _, tc := range cases {
		e := FittingAnnotationRefusal(AnnotationIssue{Kind: tc.kind, Page: 4, Subtype: "machine-token"})
		if !strings.HasPrefix(e.text, "Selected page 4 ") || !strings.Contains(e.text, tc.word) || !strings.HasSuffix(e.text, "or use original page sizes.") {
			t.Fatalf("wrong complete explanation: %s", e.text)
		}
	}
	e := PDF20AnnotationRefusal(AnnotationIssue{Kind: PDF20AppearanceMissing, Subtype: "FreeText"}, "newer.pdf")
	if !strings.Contains(e.text, "PDF 2.0 FreeText annotation requires") || !strings.Contains(e.text, `source "newer.pdf"`) {
		t.Fatal(e.text)
	}
}
