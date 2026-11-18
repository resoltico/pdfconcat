// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package files

import "testing"

func TestWindowsPotentialLeafAliases(t *testing.T) {
	for _, tc := range []struct {
		a, b                string
		bothAbsent, aliases bool
	}{
		{"packet.pdf", "Packet.pdf. ", true, true},
		{"packet.pdf", "packet.pdf::$DATA", true, true},
		{"I.pdf", "ı.pdf", true, true},
		{"LONGFI~1.PDF", "long filename.pdf", true, true},
		{"LONGFI~2.PDF", "long filename.pdf", false, false},
		{"shortrpt.JSO", "long report.json", true, true},
		{"packet.pdf", "long report.json", true, false},
		{".report.json", "report.json", true, false},
		{"short.pdf", "other.pdf", true, false},
	} {
		if got := possibleLeafAlias(tc.a, tc.b, true, tc.bothAbsent); got != tc.aliases {
			t.Errorf("%q vs %q, both absent %v: %v", tc.a, tc.b, tc.bothAbsent, got)
		}
	}
}
