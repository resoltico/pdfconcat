// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestCLIEncodingFailureRetainsSourceAttribution(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "broken.pdf")
	if err := pdffixture.EncodedPages("broken", []pdffixture.EncodedStream{{Data: []byte("not a zlib stream"), Filter: "/FlateDecode"}}).WriteFile(source); err != nil {
		t.Fatal(err)
	}
	r, exit, _ := invokeJSON(t, []string{"check", source}, "")
	if exit != 2 || r.Status != "invalid" || len(r.Diagnostics) != 1 {
		t.Fatal(r)
	}
	d := r.Diagnostics[0]
	if d.Code.String() != "source_invalid" || d.File != source || len(d.Items) != 1 || d.Items[0] != "argv:1" || d.PDFLocation == nil || d.PDFLocation.Page != 1 || d.Detail == "" {
		t.Fatalf("serialized source attribution lost: %+v", d)
	}
}
