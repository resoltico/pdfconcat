// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"strings"
	"testing"
)

func TestAMessageOfExactlyTheBoundIsNotCut(t *testing.T) {
	t.Parallel()

	for _, length := range []int{maxMessageRunes - 1, maxMessageRunes} {
		text := strings.Repeat("ā", length)
		if got := boundedMessage(text); got != text {
			t.Errorf("a message of %d characters was changed to %d characters", length, len([]rune(got)))
		}
	}

	over := strings.Repeat("ā", maxMessageRunes+1)
	if got := boundedMessage(over); got != strings.Repeat("ā", maxMessageRunes)+"…" {
		t.Errorf("a message one character over the bound: %d characters", len([]rune(got)))
	}
}
