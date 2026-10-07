// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

// Fixture paths, markers and expected contract values shared by these tests.
const (
	codePDFInvalid               = "pdf_invalid"
	keySize                      = "size"
	previousReportContent        = "old\n"
	namedFailureFormat           = "%s: %+v"
	baseDirectoryFlag            = "--base-dir"
	inlinePlanFlag               = "--plan-json"
	detailsFlag                  = "--details"
	pdfExtension                 = ".pdf"
	sourceAMarker                = "a p1"
	sourceCPath                  = "c.pdf"
	unreadablePlanPath           = "closed.json"
	sourceDPath                  = "d.pdf"
	diagnosticsFailureFormat     = "diagnostics: %+v"
	reportDirectoryPath          = "directory.json"
	exitFailureFormat            = "exit %d: %s"
	fontUnreadableCode           = "font_unreadable"
	planOutputPath               = "from-plan.pdf"
	missingPlanPath              = "missing.json"
	invalidFontPath              = "notafont.ttf"
	oversizedFontPath            = "over.ttf"
	plainMessage                 = "plain"
	publicationDiagnosticsFormat = "publication %+v, %d diagnostics"
	publicationFailureFormat     = "publication: %+v"
	reportWriteFailureCode       = "report_write_failed"
	sourcesView                  = "sources"
	summaryFailureFormat         = "summary: %+v"
	recoveryFilePattern          = ".pdfconcat-report-*"
	reportPublishFailureCode     = "report_publish_failed"
)
