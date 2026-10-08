// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main_test

// Names the tests use more than once: members of plans, reports and summaries, commands and options,
// files, and the statuses, phases and codes that several tests expect.
const (
	// Members of plans, reports and summaries that the tests build and read.
	keyVersion                  = "version"
	keyItems                    = "items"
	keyOutput                   = "output"
	keyBlank                    = "blank"
	keyText                     = "text"
	keyValue                    = "value"
	keyFont                     = "font"
	keyFile                     = "file"
	keySize                     = "size"
	keyWidth                    = "width"
	keyDir                      = "dir"
	keyParts                    = "parts"
	keyPublication              = "publication"
	keyAttemptID                = "attempt_id"
	keyExpectedAttempt          = "expect_attempt"
	keyNextOmitted              = "next_omitted"
	invokingExecutableReference = "invoking_executable"
	keySavedRun                 = "saved_run"
	keyBackground               = "background"

	// Commands, command-line options and their values.
	commandBuild   = "build"
	commandCheck   = "check"
	commandReport  = "report"
	commandHelp    = "help"
	commandSchema  = "schema"
	commandVersion = "version"

	flagPlan      = "--plan"
	flagHelp      = "--help"
	flagReport    = "--report"
	flagDetails   = "--details"
	flagBlank     = "--blank"
	flagFormat    = "--format"
	flagOverwrite = "--overwrite"
	flagPage      = "--page"
	flagView      = "--view"
	flagBaseDir   = "--base-dir"

	formatText         = "text"
	viewParts          = "parts"
	kindReport         = "report"
	kindHelp           = "help"
	responseSchemaName = "response"

	// Names of the files the tests create, and of the ways a plan reaches the command.
	fileA               = "a.pdf"
	fileB               = "b.pdf"
	fileOut             = "out.pdf"
	fileMissing         = "missing.pdf"
	fileJob             = "job.json"
	fileSavedReport     = "saved.json"
	fileBadPlan         = "bad.json"
	fileFont            = "font.ttf"
	copiedFailureReport = "failed copy.json"
	historicalReport    = "historic.json"
	freshReportPath     = "fresh.json"
	directBlankPart     = "argv:3"

	transportFile  = "file"
	transportStdin = "stdin"

	// Statuses, phases, diagnostic codes and page text that several tests expect.
	reportFailed         = "failed"
	reportWritten        = "written"
	phaseIncomplete      = "incomplete"
	phaseComplete        = "complete"
	phaseInstructions    = "instructions"
	statusInterrupted    = "interrupted"
	statusInvalid        = "invalid"
	codeJSONSyntax       = "json_syntax"
	codeSourceUnreadable = "source_unreadable"
	codeAliasConflict    = "alias_conflict"
	codeReportWriteBad   = "report_write_failed"
	dividerText          = "Divider"
	unbreakableText      = "unbreakableunbreakable"
)
