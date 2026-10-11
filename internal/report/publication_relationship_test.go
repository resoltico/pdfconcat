// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
package report_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestImpossiblePublishedReportsAreRejected(t *testing.T) {
	paths, globErr := filepath.Glob("testdata/outcomes/*.json")
	if globErr != nil || len(paths) != 2 {
		t.Fatalf("fixture discovery: %v %v", paths, globErr)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if closeErr := f.Close(); closeErr != nil {
					t.Error(closeErr)
				}
			}()
			_, err = report.Decode(report.DecoderTestContext(t.Context(), t), path, report.ReaderRequiringStorage(f))
			if err == nil {
				t.Fatal("accepted impossible publication")
			}
			found, ok := report.AsError(err)
			if !ok || found.Diagnostic.Location == nil || found.Diagnostic.Location.Pointer == "" {
				t.Fatalf("unlocated rejection: %v", err)
			}
		})
	}
}
