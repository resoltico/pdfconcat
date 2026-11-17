// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import "testing"

func TestLanguageTagSyntaxAndUniqueness(t *testing.T) {
	for _, tag := range []string{"en-K-foo", "en-İ-foo", "x-K"} {
		if validLanguageTag(tag) {
			t.Errorf("non-ASCII tag accepted: %s", tag)
		}
	}
	for _, tag := range []string{"en", "xyz", "abcd", "abcde", "en-Latn-US", "en-abc-def-ghi-Latn-123-abcde-1abc-a-foo-b-bar-x-private", "x-private", "x-x-x", "en-a-foo-x-a-a", "en-abcde-fghij", "en-u-ca-gregory", "i-klingon", "en-GB-oed", "sgn-BE-FR"} {
		if !validLanguageTag(tag) {
			t.Errorf("valid tag rejected: %s", tag)
		}
	}
	for _, tag := range []string{"", "en_US", "en--US", "en-Latn-Latn", "en-US-US", "en-a-foo-a-bar", "en-A-foo-a-bar", "en-abcde-abcde", "en-Abcde-ABCDE", "en-x", "x", "en-a", "en-a-x-private", "en-1234-1234", "e", "en-abcdefghi", "en-@-foo", "i-unknown", "en-Latn-US-Latn"} {
		if validLanguageTag(tag) {
			t.Errorf("invalid tag accepted: %s", tag)
		}
	}
}
