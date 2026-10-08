// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly_test

import (
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

func TestPathsRejectNonUnicodeTextAtTheDomainBoundary(t *testing.T) {
	t.Parallel()

	bad := string([]byte{0xff})
	if err := assembly.CheckPathText(bad); err == nil {
		t.Fatal("invalid UTF8 path accepted")
	}

	for _, paths := range [][2]string{{t.TempDir(), bad}, {filepath.Join(t.TempDir(), bad), "source.pdf"}} {
		if _, err := assembly.ResolvePath(paths[0], "source", paths[1]); err == nil {
			t.Fatal("invalid UTF8 name/base resolved")
		}
	}
}

func TestPathResolutionRejectsUnrepresentableTextAcrossStyles(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"\xb1", "bad\xe2\x82", "C:\xb1", "\\\xb1", "C:\x00"} {
		for _, style := range []assembly.PathStyle{assembly.PathStylePOSIX, assembly.PathStyleWindows} {
			checkPathStyle(t, style, name)
		}
	}
}
