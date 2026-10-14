// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"maps"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func TestWindowsOracleArchivesRequireExactPins(t *testing.T) {
	t.Parallel()

	versions, err := repopolicy.ParseToolVersions(string(readRepoFile(t, toolVersionsPath)))
	if err != nil {
		t.Fatal(err)
	}
	config := string(readRepoFile(t, ".goreleaser.yml"))
	if issues := repopolicy.ToolVersionIssues(versions, config); len(issues) != 0 {
		t.Fatal(issues)
	}

	for _, key := range []string{"QPDF_WINDOWS_VERSION", "POPPLER_WINDOWS_RELEASE", "QPDF_WINDOWS_SHA256", "POPPLER_WINDOWS_SHA256"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			for _, invalid := range []string{"", "latest", strings.Repeat("A", 64)} {
				broken := maps.Clone(versions)
				broken[key] = invalid
				if len(repopolicy.ToolVersionIssues(broken, config)) == 0 {
					t.Fatalf("invalid %s=%q accepted", key, invalid)
				}
			}
		})
	}
}
