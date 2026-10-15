// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pipeline_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

// TestComparisonFixtures exports only the small inputs needed by run-comparison.sh.
func TestComparisonFixtures(t *testing.T) {
	dir := os.Getenv("PDFCONCAT_COMPARISON_DIR")
	if dir == "" {
		t.Skip("fixture export is requested by scripts/run-comparison.sh")
	}
	fixtures := map[string]*pdffixture.Doc{
		"plain-a.pdf": pdffixture.Plain("A"), "links.pdf": pdffixture.Links("L"),
		"named.pdf": pdffixture.NamedDests("N"), "legacy-dests.pdf": pdffixture.LegacyDests("D"),
		"form.pdf": pdffixture.Form("F"), "outlined.pdf": pdffixture.Outlined("O"),
		"signed.pdf": pdffixture.SignatureValue("S"), "fitmarks.pdf": pdffixture.FitMarks("FIT"),
	}
	damaged := pdffixture.Pages("DAMAGED", 2)
	damaged.Objs[5] = []byte("<< >>")
	fixtures["damaged.pdf"] = damaged
	for name, document := range fixtures {
		path := filepath.Join(dir, name)
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("comparison fixture target must be fresh: %s %v", path, err)
		}
		if err := document.WriteFile(path); err != nil {
			t.Fatal(err)
		}
	}
}
