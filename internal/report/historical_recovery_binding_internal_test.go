// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQueryFirstBoundsRecoveryAgainstHistoricalDirectory(t *testing.T) {
	t.Parallel()

	saved := initiallyFittingHistoricalRecovery(t)
	copied, copiedPath := copiedRecoveryEvidence(t, saved)

	selected, err := copied.Query(Request{})
	if err != nil {
		t.Fatal(err)
	}

	query := QueryResultOf(copied, selected, "/pdfconcat", copiedPath)

	brief, ok := ContentOf[Summary](query.Result)
	if !ok || brief.RecoveryBasename != "receipt.json" || brief.Publication.RecoveryReport != "" {
		t.Fatalf(
			"query did not first omit historical recovery path: basename=%q path=%q",
			brief.RecoveryBasename,
			brief.Publication.RecoveryReport,
		)
	}

	if brief.RecoveryDirectoryFrom != "complete_report.publication.recovery_report" {
		t.Fatalf("copied query redirected historical recovery directory to current argument: %q", brief.RecoveryDirectoryFrom)
	}

	payload, err := Encode(query)
	if err != nil || len(payload)+1 > 2048 {
		t.Fatalf("historical recovery reference escaped measured query budget: bytes=%d error=%v", len(payload)+1, err)
	}
}

func initiallyFittingHistoricalRecovery(t *testing.T) *Report {
	t.Helper()

	saved := nodeSamples()[0]
	saved.Publication.RecoveryReport = "/historical/" + strings.Repeat("a/", 350) + "receipt.json"

	if err := saved.Validate(); err != nil {
		t.Fatal(err)
	}

	for padding := range 1800 {
		saved.Diagnostics[0].Cause = strings.Repeat("x", padding)

		response, err := saved.Query(Request{})
		if err != nil {
			t.Fatal(err)
		}

		brief, ok := ContentOf[Summary](response)
		if !ok || brief.RecoveryDirectoryFrom != "" || brief.Publication.RecoveryReport == "" {
			continue
		}

		encoded, err := Encode(response)
		if err != nil {
			t.Fatal(err)
		}

		if len(encoded)+1 > 1900 {
			return saved
		}
	}

	t.Fatal("no supported initially fitting recovery-path fixture")

	return nil
}

func copiedRecoveryEvidence(t *testing.T, saved *Report) (*Report, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "copied.report.json")

	complete, err := Encode(saved)
	if err != nil {
		t.Fatal(err)
	}

	if err = os.WriteFile(path, complete, 0o600); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}

	copied, decodeErr := Decode(DecoderTestContext(t.Context(), t), path, file)

	closeErr := file.Close()
	if decodeErr != nil || closeErr != nil {
		t.Fatalf("copied report decode=%v close=%v", decodeErr, closeErr)
	}

	return copied, path
}
