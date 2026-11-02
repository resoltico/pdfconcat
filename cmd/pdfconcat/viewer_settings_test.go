// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestViewerSettingsCLITextAndJSONShareCoreFindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "viewer.pdf")
	doc := pdffixture.Plain("viewer")
	doc.Objs[0] = bytes.Replace(doc.Objs[0], []byte("/Type /Catalog"), []byte("/Type /Catalog /PageLayout /PDLayoutDontCare"), 1)
	if err := doc.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	r, exit, _ := invokeJSON(t, []string{"check", path, path}, "")
	if exit != 0 || r.Status != "ok" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code.String() != "source_viewer_settings_removed" || r.Diagnostics[0].PDFLocation == nil || len(r.Diagnostics[0].Items) != 2 {
		t.Fatal(r)
	}
	var stdout, stderr bytes.Buffer
	if exit = run([]string{"check", path, path}, strings.NewReader(""), &stdout, &stderr); exit != 0 {
		t.Fatalf("text failure: %s %s", &stdout, &stderr)
	}
	if strings.Count(stderr.String(), "source_viewer_settings_removed") != 1 || !strings.Contains(stderr.String(), "Catalog.PageLayout") || !strings.Contains(stderr.String(), "argv:1") || !strings.Contains(stderr.String(), "argv:2") {
		t.Fatalf("text findings: %q", stderr.String())
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, doc.Bytes()) {
		t.Fatal("CLI changed the original", err)
	}
}
