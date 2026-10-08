// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPreviewByteLimitPreservesExactUTF8AndEscapedBoundaries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value, want string
		limit       int
		cut         bool
	}{
		{"abcd efg", "abcd efg", 8, false},
		{"xxxxxxx", "xxxxxx", 6, true},
		{strings.Repeat("\\", 5), strings.Repeat("\\", 4), 8, true},
		{strings.Repeat("\"", 5), strings.Repeat("\"", 4), 8, true},
		{strings.Repeat("😀", 3), strings.Repeat("😀", 2), 8, true},
		{strings.Repeat("\x01", 2), "\x01", 6, true},
	}

	for _, test := range cases {
		got, cut := boundPreviewBytes(test.value, test.limit)
		if got != test.want || cut != test.cut || !utf8.ValidString(got) {
			t.Errorf("preview %q at %d bytes: %q/%t, want %q/%t", test.value, test.limit, got, cut, test.want, test.cut)
		}
	}
}
