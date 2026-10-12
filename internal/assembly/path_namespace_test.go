// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly_test

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

func TestWindowsBindingPreservesComponentsAndNamespaceGrammar(t *testing.T) {
	t.Parallel()
	for _, row := range []struct{ expression, want string }{
		{`nested//./../Å.pdf`, `C:\base\nested\\.\..\Å.pdf`},
		{`C:/absolute//./../x.pdf`, `C:\absolute\\.\..\x.pdf`},
		{`\\server\share\folder\x.pdf`, `\\server\share\folder\x.pdf`},
		{`\\?\C:\long\x.pdf`, `\\?\C:\long\x.pdf`},
		{`\\?\UNC\server\share\x.pdf`, `\\?\UNC\server\share\x.pdf`},
		{`\\?\UNC\server\share`, `\\?\UNC\server\share`},
	} {
		_, got, err := assembly.ResolvePathWithStyle(assembly.PathStyleWindows, `C:\base`, "source", row.expression)
		if err != nil || got != row.want {
			t.Fatalf("%q selected %q/%v, want %q", row.expression, got, err, row.want)
		}
	}
	for _, expression := range []string{`C:relative`, `\rooted`, `/rooted`, `\\?\C:/x`, `\\?\C:\a\..\x`, `\\?\UNC\server`, `\\?\1:\x`, `\\?\C!\x`, `//?/C:/x`} {
		if _, got, err := assembly.ResolvePathWithStyle(assembly.PathStyleWindows, `C:\base`, "source", expression); err == nil || got != "" {
			t.Fatalf("ambiguous or repaired namespace accepted: %q -> %q", expression, got)
		}
	}
}

func TestVerbatimBaseNeverRepairsRelativeExpressions(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"a/b", `a\..\x`} {
		if _, _, err := assembly.ResolvePathWithStyle(assembly.PathStyleWindows, `\\?\C:\base`, "source", name); err == nil {
			t.Fatalf("verbatim base repaired %q", name)
		}
	}
	_, got, err := assembly.ResolvePathWithStyle(assembly.PathStyleWindows, `\\?\C:\`, "source", "x.pdf")
	if err != nil || got != `\\?\C:\x.pdf` {
		t.Fatalf("verbatim root binding: %q/%v", got, err)
	}
	_, got, err = assembly.ResolvePathWithStyle(assembly.PathStyleWindows, `\\?\C:\base`, "source", "D:/ordinary/x.pdf")
	if err != nil || got != `D:\ordinary\x.pdf` {
		t.Fatalf("absolute source must select its own namespace: %q/%v", got, err)
	}
}
