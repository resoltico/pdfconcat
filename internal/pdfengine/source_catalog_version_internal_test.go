// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"path/filepath"
	"testing"
)

type sourceCatalogVersionCase struct {
	name, entry, target    string
	generation             int
	free, refuse, override bool
}

func TestSourceOptionalCatalogVersionUsesNullAndExactIdentity(t *testing.T) {
	t.Parallel()

	for _, test := range []sourceCatalogVersionCase{
		{name: "direct null", entry: nullPDFObject, target: nullPDFObject},
		{name: "missing version", entry: sourceMissingReference, target: nullPDFObject},
		{name: "free version", entry: "4 0 R", free: true},
		{name: "explicit null version", entry: "4 0 R", target: nullPDFObject},
		{name: "wrong generation version", entry: "4 8 R", target: "/2.0", generation: 7},
		{name: "matching nonzero version", entry: "4 7 R", target: "/2.0", generation: 7, override: true},
		{name: "direct invalid type", entry: "42", target: nullPDFObject, refuse: true},
		{name: "indirect invalid type", entry: "4 7 R", target: "42", generation: 7, refuse: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			bodies := []string{
				"<< /Type /Catalog /Pages 2 0 R /Version " + test.entry + " >>",
				guardSinglePageTree,
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources <<>> >>",
				test.target,
			}
			data := sourceIdentityPDF(fitCatalogSourceVersion, "", bodies, map[int]int{4: test.generation}, map[int]bool{4: test.free})
			dir := t.TempDir()

			path := filepath.Join(dir, "version.pdf")
			sourceWrite(t, path, data)

			engine := sourceNewEngine(t)

			for _, target := range []*PageSize{nil, {595, 842}, {612, 1008}} {
				test.verifyMode(t, engine, path, target)
			}
		})
	}

	data := sourceIdentityPDF(fitCatalogSourceVersion, "", []string{nullPDFObject, "<< /Type /Pages /Count 0 /Kids [] >>"}, nil, nil)

	path := filepath.Join(t.TempDir(), "null-root.pdf")
	sourceWrite(t, path, data)

	engine := sourceNewEngine(t)

	if _, err := engine.Inspect(t.Context(), path, nil); err == nil {
		t.Fatal("required null Root accepted")
	}
}
