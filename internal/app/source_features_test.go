// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/report"
)

const (
	repeatedFeatureConsumer = "argv:5"
)

func TestSourceFeatureWarningsAggregateRepeatedConsumersAndPreserveContext(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	if err := pdffixture.Outlined("outline").WriteFile(filepath.Join(dir, sourceA)); err != nil {
		t.Fatal(err)
	}

	for _, command := range []string{commandCheck, commandBuild} {
		result := execute(
			t.Context(),
			t,
			nativeInventoryApp(),
			dir,
			command,
			outputFlag,
			"features.pdf",
			sourceA,
			blankFlag,
			sourceA,
			reportFlag,
			reportFile,
			overwriteFlag,
		)
		result.requireCode(t, 0, "")

		saved := decodedInventoryReport(t, dir)
		if saved.WarningCount != 1 || saved.ErrorCount != 0 || len(saved.Diagnostics) != 1 {
			t.Fatalf("aggregated facts %+v", saved.Diagnostics)
		}

		warning := saved.Diagnostics[0]

		context := "predicted"
		if command == commandBuild {
			context = "committed"
		}

		if warning.Severity != report.SeverityWarning || warning.ConsequenceContext != context || len(warning.Consumers) != 2 ||
			warning.Consumers[0] != "argv:3" ||
			warning.Consumers[1] != repeatedFeatureConsumer {
			t.Fatalf("scope/consumers %+v", warning)
		}
	}
}

func TestSourceWarningsRemainCapturedWhenAnotherInputFails(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	if err := pdffixture.Tagged("tagged").WriteFile(filepath.Join(dir, sourceA)); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(dir, "malformed-feature-source.pdf"), "not a PDF")

	result := execute(
		t.Context(),
		t,
		nativeInventoryApp(),
		dir,
		commandCheck,
		sourceA,
		"malformed-feature-source.pdf",
		reportFlag,
		reportFile,
	)
	result.requireCode(t, 1, "pdf_invalid")

	saved := decodedInventoryReport(t, dir)
	if saved.WarningCount != 1 || saved.ErrorCount != 1 {
		t.Fatalf("mixed captured facts %+v", saved.Diagnostics)
	}
}

func TestURIBaseWarningAggregatesRepeatedLinksAndSurvivesSavedQueries(t *testing.T) {
	t.Parallel()

	dir := workDir(t)

	doc := pdffixture.URILink("relative URI", "annex.pdf", "https://example.invalid/report/", true)
	if err := doc.WriteFile(filepath.Join(dir, sourceA)); err != nil {
		t.Fatal(err)
	}

	for _, command := range []string{commandCheck, commandBuild} {
		result := execute(t.Context(), t, nativeInventoryApp(), dir, command, outputFlag, "uri.pdf",
			sourceA, sourceA, reportFlag, reportFile, overwriteFlag)
		result.requireCode(t, 0, "")

		saved := decodedInventoryReport(t, dir)
		if saved.WarningCount != 1 || len(saved.Diagnostics) != 1 {
			t.Fatalf("duplicated URI effect: %+v", saved.Diagnostics)
		}

		warning := saved.Diagnostics[0]

		context := "predicted"
		if command == commandBuild {
			context = "committed"
		}

		if warning.Code != "source_uri_base_removed" || warning.ConsequenceContext != context || len(warning.Consumers) != 2 {
			t.Fatalf("URI effect/provenance: %+v", warning)
		}

		query := execute(t.Context(), t, nativeInventoryApp(), dir, "report", reportFile, "--view=diagnostics", "--details")
		query.requireCode(t, 0, "")

		if !strings.Contains(query.stdout, "source_uri_base_removed") {
			t.Fatalf("saved query lost URI effect: %s", query.stdout)
		}
	}
}
