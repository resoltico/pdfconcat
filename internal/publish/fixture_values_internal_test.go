// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package publish

// Fixture paths, markers and expected contract values shared by these tests.
const (
	commitFailureFormat = "Commit() = %+v, %v"
	stagedContent       = "content"
	existingContent     = "existing"
	hardlinkVariant     = "hardlink"
	missingPath         = "missing"
	replacementContent  = "new"
	outputReportPath    = "out.json"
	outputPath          = "out.pdf"
	shortReportPath     = "r.json"
	realFilePath        = "real"
	reportPath          = "report.json"
	stagedPath          = "s.pdf"
	stagedPDFPath       = "staged.pdf"
	stagingLeftFormat   = "staging left behind: %v"
)
