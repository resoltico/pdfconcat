// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package diag defines product diagnostics and outcome classes.
package diag

type Code string
type Severity string
type Class string

type Definition struct {
	Severity    Severity
	Class       Class
	Description string
}
type Location struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Offset  int64  `json:"offset"`
	Pointer string `json:"pointer"`
}
type Diagnostic struct {
	Severity     Severity  `json:"severity"`
	Code         Code      `json:"code"`
	Message      string    `json:"message"`
	File         string    `json:"file,omitempty"`
	Items        []string  `json:"items,omitempty"`
	ItemsOmitted int       `json:"items_omitted"`
	Location     *Location `json:"location,omitempty"`
}

// Registry returns a copy so callers cannot change the public code definitions.
func Registry() map[Code]Definition {
	return map[Code]Definition{
		"source_form_unsupported":         {Severity: "error", Class: "invalid", Description: "form default resources or defaults differ between nonempty source forms"},
		"source_uri_base_removed":         {Severity: "warning", Description: "relative URI actions lose their source catalog Base"},
		"usage_unknown_command":           {Severity: "error", Class: "invalid", Description: "first argument is not a command"},
		"usage_unknown_option":            {Severity: "error", Class: "invalid", Description: "option not defined for the command"},
		"usage_missing_value":             {Severity: "error", Class: "invalid", Description: "option needs a value"},
		"usage_invalid_value":             {Severity: "error", Class: "invalid", Description: "option value not allowed (`--format`, `--fit-to`, `schema` name, \u2026)"},
		"usage_conflict":                  {Severity: "error", Class: "invalid", Description: "`--plan` with PDF operands; `--base-dir` without `--plan -`; repeated single-use option"},
		"usage_missing_input":             {Severity: "error", Class: "invalid", Description: "neither `--plan` nor PDF operands"},
		"usage_missing_output":            {Severity: "error", Class: "invalid", Description: "`build` without `-o` and without plan `output`"},
		"plan_unreadable":                 {Severity: "error", Class: "failed", Description: "plan file or stdin could not be read"},
		"plan_too_large":                  {Severity: "error", Class: "invalid", Description: "plan exceeds 64 MiB"},
		"plan_syntax":                     {Severity: "error", Class: "invalid", Description: "not one valid JSON object in UTF-8 (incl. trailing data, invalid UTF-8, comments)"},
		"plan_version_unsupported":        {Severity: "error", Class: "invalid", Description: "`version` is not 2 (message explains v1 \u2192 v2 renames)"},
		"plan_unknown_member":             {Severity: "error", Class: "invalid", Description: "member not defined at that place (case-sensitive)"},
		"plan_duplicate_member":           {Severity: "error", Class: "invalid", Description: "member repeated in one object"},
		"plan_null":                       {Severity: "error", Class: "invalid", Description: "`null` anywhere"},
		"plan_type":                       {Severity: "error", Class: "invalid", Description: "wrong JSON type"},
		"plan_value":                      {Severity: "error", Class: "invalid", Description: "value outside its grammar or range (length, colour, size, anchor, count, \u2026)"},
		"plan_item_shape":                 {Severity: "error", Class: "invalid", Description: "an item object that is neither a generated page nor a group"},
		"plan_limit":                      {Severity: "error", Class: "invalid", Description: "nesting, entries, count or total generated pages over the limit"},
		"path_invalid":                    {Severity: "error", Class: "invalid", Description: "empty path, NUL, Windows drive-relative (`C:x`) or rooted-without-drive (`\\x`)"},
		"output_exists":                   {Severity: "error", Class: "invalid", Description: "output exists and `--overwrite` not given"},
		"report_exists":                   {Severity: "error", Class: "invalid", Description: "report exists and `--overwrite` not given (or inputs unknown, see CLI.md)"},
		"output_not_regular":              {Severity: "error", Class: "invalid", Description: "output exists and is not a regular file (directory, symlink, device)"},
		"report_not_regular":              {Severity: "error", Class: "invalid", Description: "same for the report"},
		"output_is_input":                 {Severity: "error", Class: "invalid", Description: "output is (or aliases) a plan, font or source file"},
		"report_is_input":                 {Severity: "error", Class: "invalid", Description: "report is (or aliases) an input"},
		"report_is_output":                {Severity: "error", Class: "invalid", Description: "report and output are the same file"},
		"font_unreadable":                 {Severity: "error", Class: "failed", Description: "font file could not be read"},
		"font_invalid":                    {Severity: "error", Class: "invalid", Description: "not a static TrueType (`glyf`) font, a collection, variable, or damaged"},
		"font_not_embeddable":             {Severity: "error", Class: "invalid", Description: "`OS/2.fsType` forbids embedding"},
		"text_unsupported_character":      {Severity: "error", Class: "invalid", Description: "character outside the accepted scripts, right-to-left, control or format character"},
		"text_glyph_missing":              {Severity: "error", Class: "invalid", Description: "the font cannot render a character sequence"},
		"text_overflow":                   {Severity: "error", Class: "invalid", Description: "text block or glyph ink outside the page, or a word wider than the block, with `overflow: error`"},
		"page_size_required":              {Severity: "error", Class: "invalid", Description: "`inherit` with no source page to inherit from"},
		"page_size_too_large":             {Severity: "error", Class: "invalid", Description: "inherited size above 14,400 pt; give an explicit size"},
		"source_unreadable":               {Severity: "error", Class: "failed", Description: "source file could not be read"},
		"source_changed":                  {Severity: "error", Class: "failed", Description: "source bytes at merge time differ from the inspected bytes"},
		"source_invalid":                  {Severity: "error", Class: "invalid", Description: "pdfcpu could not parse or validate the file (incl. recovered panics)"},
		"source_damaged":                  {Severity: "error", Class: "invalid", Description: "broken page tree (missing/empty kid, wrong count) or content-replacing repair"},
		"source_encrypted":                {Severity: "error", Class: "invalid", Description: "encrypted, including empty-password encryption"},
		"source_signed":                   {Severity: "error", Class: "invalid", Description: "signature value, certification (`/Perms`) or document timestamp"},
		"source_dynamic_form":             {Severity: "error", Class: "invalid", Description: "XFA with `NeedsRendering true`"},
		"source_optional_content":         {Severity: "error", Class: "invalid", Description: "`/OCProperties` present"},
		"fit_form_unsupported":            {Severity: "error", Class: "invalid", Description: "`fit_to` with a source that has form widgets"},
		"assembly_failed":                 {Severity: "error", Class: "failed", Description: "pdfcpu merge, resize, optimise or write failed (incl. recovered panics)"},
		"output_write_failed":             {Severity: "error", Class: "failed", Description: "writing the temporary output failed (disk full, permissions)"},
		"output_invalid":                  {Severity: "error", Class: "failed", Description: "the written output did not validate"},
		"output_page_count_mismatch":      {Severity: "error", Class: "failed", Description: "written page count \u2260 expected"},
		"output_publish_failed":           {Severity: "error", Class: "failed", Description: "the final rename failed (incl. no-replace unsupported and link fallback failed)"},
		"report_write_failed":             {Severity: "error", Class: "failed", Description: "the report could not be written"},
		"interrupted":                     {Severity: "error", Class: "interrupted", Description: "cancelled by a signal"},
		"text_overflow_allowed":           {Severity: "warning", Class: "", Description: "`overflow: allow` and measured text extends past the page or block"},
		"form_fields_renamed":             {Severity: "warning", Class: "", Description: "pdfcpu namespaces later nonempty forms even without collisions; build reports actual names"},
		"form_appearances_regenerated":    {Severity: "warning", Class: "", Description: "a source requests `NeedAppearances`; viewers may redraw all fields"},
		"destinations_renamed":            {Severity: "warning", Class: "", Description: "colliding named destinations were renamed for later occurrences"},
		"destinations_dropped":            {Severity: "warning", Class: "", Description: "named destinations whose target is not a page of the same document were omitted"},
		"source_xfa_removed":              {Severity: "warning", Class: "", Description: "XFA removed; the static AcroForm remains"},
		"source_structure_removed":        {Severity: "warning", Class: "", Description: "tagged-PDF structure removed; output is not tagged"},
		"source_page_labels_removed":      {Severity: "warning", Class: "", Description: "page labels removed"},
		"source_output_intents_removed":   {Severity: "warning", Class: "", Description: "output intents removed; output makes no PDF/A or PDF/X claim"},
		"source_document_actions_removed": {Severity: "warning", Class: "", Description: "`/OpenAction`, catalog `/AA` or document JavaScript removed"},
		"source_attachments_removed":      {Severity: "warning", Class: "", Description: "embedded-files name tree or catalog `/AF` removed (page attachments remain)"},
	}
}

var definitions = Registry()

func New(code Code, message string, items ...string) Diagnostic {
	definition, ok := definitions[code]
	if !ok {
		panic("undefined diagnostic: " + string(code))
	}
	return Diagnostic{Severity: definition.Severity, Code: code, Message: message, Items: items}
}

// Outcome gives cancellation precedence over environmental failure over invalid input.
func Outcome(diagnostics []Diagnostic) (string, int) {
	status, exit := "ok", 0
	for _, d := range diagnostics {
		if d.Severity != "error" {
			continue
		}
		switch definitions[d.Code].Class {
		case "interrupted":
			return "interrupted", 130
		case "failed":
			status, exit = "failed", 1
		case "invalid":
			if status == "ok" {
				status, exit = "invalid", 2
			}
		}
	}
	return status, exit
}
