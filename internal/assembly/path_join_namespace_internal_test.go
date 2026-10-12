// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly

import "testing"

func TestLexicalJoinRetainsNamespaceSpellingForValidation(t *testing.T) {
	t.Parallel()
	// JoinDir does no validation. Namespace slashes must reach the validating
	// boundary unchanged so they can be rejected rather than silently repaired.
	for _, prefix := range []string{`\\?\`, `\\.\`} {
		base, name := prefix+`C:\a/b`, prefix+`D:\x/y`
		if got := joinPath(PathStyleWindows, base, "leaf"); got != base+`\leaf` {
			t.Fatalf("namespace base spelling changed: %q", got)
		}
		if got := joinPath(PathStyleWindows, `C:\base`, name); got != name {
			t.Fatalf("namespace name spelling changed: %q", got)
		}
	}
}
