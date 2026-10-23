// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package diag defines product diagnostics and outcome classes.
package diag

import (
	"errors"
	"slices"
	"strings"
)

// Code is a stable diagnostic identifier.
type Code struct{ name string }

// String returns the diagnostic wire name.
func (c Code) String() string { return c.name }

// MarshalText encodes a diagnostic identifier.
func (c Code) MarshalText() ([]byte, error) { return []byte(c.name), nil }

// UnmarshalText accepts only registered identifiers.
func (c *Code) UnmarshalText(text []byte) error {
	for _, entry := range registry {
		if entry.Code.name == string(text) {
			*c = entry.Code
			return nil
		}
	}
	return errors.New("unknown diagnostic code: " + string(text))
}

// Entry is one ordered diagnostic definition.
type Entry struct {
	Code       Code
	Definition Definition
}

var registry []Entry

func define(name string, severity Severity, class Class, description string) Code {
	code := Code{name: name}
	registry = append(registry, Entry{Code: code, Definition: Definition{Severity: severity, Class: class, Description: description}})
	return code
}

// Entries returns the ordered registry.
func Entries() []Entry { return slices.Clone(registry) }

// Severity distinguishes errors from warnings.
type Severity string

// Class determines an error outcome; warnings have no class.
type Class string

// Status is the complete execution outcome.
type Status string

// Diagnostic outcomes and severity values.
const (
	Error             Severity = "error"
	Warning           Severity = "warning"
	NoClass           Class    = ""
	Invalid           Class    = "invalid"
	Failed            Class    = "failed"
	ClassInterrupted  Class    = "interrupted"
	OK                Status   = "ok"
	StatusInvalid     Status   = "invalid"
	StatusFailed      Status   = "failed"
	StatusInterrupted Status   = "interrupted"
)

// Definition is the authoritative meaning and outcome of one code.
type Definition struct {
	Severity    Severity
	Class       Class
	Description string
}

// Location identifies a zero-based byte offset and one-based byte line/column.
type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Offset int64  `json:"offset"`
	Ref    string `json:"pointer"`
}

// Diagnostic is a located error or warning with its consuming plan entries.
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
	values := make(map[Code]Definition, len(registry))
	for _, entry := range registry {
		values[entry.Code] = entry.Definition
	}
	return values
}

// Registered diagnostic identifiers. Definitions follow declaration order.
var (
	RequestInvalid                      = define("request_invalid", Error, Invalid, "invalid application request envelope")
	SourceSelectionOutOfRange           = define("source_selection_out_of_range", Error, Invalid, "selected page range exceeds the original source page count")
	SourceSelectionFormUnsupported      = define("source_selection_form_unsupported", Error, Invalid, "partial page selection of a source with form fields or widgets is unsupported")
	SourceNavigationRemoved             = define("source_navigation_removed", Warning, NoClass, "navigation to omitted source pages was removed for affected occurrences")
	SourceOutlineDepthUnsupported       = define("source_outline_depth_unsupported", Error, Invalid, "authored sections and imported bookmarks exceed the supported outline hierarchy")
	SourceFormUnsupported               = define("source_form_unsupported", Error, Invalid, "nonempty forms require identical resolved DR/DA/Q representations")
	SourceURIBaseRemoved                = define("source_uri_base_removed", Warning, NoClass, "source URI Base is removed; retained relative actions may resolve differently")
	UsageUnknownCommand                 = define("usage_unknown_command", Error, Invalid, "first argument is not a command")
	UsageUnknownOption                  = define("usage_unknown_option", Error, Invalid, "option not defined for the command")
	UsageMissingValue                   = define("usage_missing_value", Error, Invalid, "option needs a value")
	UsageInvalidValue                   = define("usage_invalid_value", Error, Invalid, "option value not allowed (`--fit-to`, `schema` name, \u2026)")
	UsageConflict                       = define("usage_conflict", Error, Invalid, "`--plan` with PDF operands; `--base-dir` without `--plan -`; repeated single-use option")
	UsageMissingInput                   = define("usage_missing_input", Error, Invalid, "neither `--plan` nor PDF operands")
	UsageMissingOutput                  = define("usage_missing_output", Error, Invalid, "`build` without `-o` and without plan `output`")
	PlanUnreadable                      = define("plan_unreadable", Error, Failed, "plan file or stdin could not be read")
	PlanTooLarge                        = define("plan_too_large", Error, Invalid, "plan exceeds 64 MiB")
	PlanSyntax                          = define("plan_syntax", Error, Invalid, "not one valid JSON object in UTF-8 (incl. trailing data, invalid UTF-8, comments)")
	PlanVersionUnsupported              = define("plan_version_unsupported", Error, Invalid, "required plan version is missing or unsupported")
	PlanUnknownMember                   = define("plan_unknown_member", Error, Invalid, "member not defined at that place (case-sensitive)")
	PlanDuplicateMember                 = define("plan_duplicate_member", Error, Invalid, "member repeated in one object")
	PlanNull                            = define("plan_null", Error, Invalid, "`null` anywhere")
	PlanType                            = define("plan_type", Error, Invalid, "wrong JSON type")
	PlanValue                           = define("plan_value", Error, Invalid, "value outside its grammar or range (length, colour, size, anchor, count, \u2026)")
	PlanItemShape                       = define("plan_item_shape", Error, Invalid, "an item object without exactly one supported source, page, group or contents shape")
	PlanLimit                           = define("plan_limit", Error, Invalid, "nesting, entries, count or total generated pages over the limit")
	PathInvalid                         = define("path_invalid", Error, Invalid, "empty path, NUL, Windows drive-relative (`C:x`) or rooted-without-drive (`\\x`)")
	OutputExists                        = define("output_exists", Error, Invalid, "output exists and `--overwrite` not given")
	ReportExists                        = define("report_exists", Error, Invalid, "report exists and `--overwrite` not given (or inputs unknown, see CLI.md)")
	OutputNotRegular                    = define("output_not_regular", Error, Invalid, "output exists and is not a regular file (directory, symlink, device)")
	ReportNotRegular                    = define("report_not_regular", Error, Invalid, "same for the report")
	OutputIsInput                       = define("output_is_input", Error, Invalid, "output is (or aliases) a plan, font or source file")
	ReportIsInput                       = define("report_is_input", Error, Invalid, "report is (or aliases) an input")
	ReportIsOutput                      = define("report_is_output", Error, Invalid, "report and output are the same file")
	FontUnreadable                      = define("font_unreadable", Error, Failed, "font file could not be read")
	FontInvalid                         = define("font_invalid", Error, Invalid, "not a supported static TrueType (`glyf`) font, damaged, or its PostScript name cannot be preserved")
	FontNotEmbeddable                   = define("font_not_embeddable", Error, Invalid, "`OS/2.fsType` forbids embedding")
	TextUnsupportedCharacter            = define("text_unsupported_character", Error, Invalid, "character outside the accepted scripts, right-to-left, control or format character")
	TextGlyphMissing                    = define("text_glyph_missing", Error, Invalid, "the font cannot render a character sequence")
	TextOverflow                        = define("text_overflow", Error, Invalid, "unusable derived text width, or measured block/ink/word overflow with overflow: error")
	PageSizeRequired                    = define("page_size_required", Error, Invalid, "`inherit` with no source page to inherit from")
	PageSizeTooLarge                    = define("page_size_too_large", Error, Invalid, "inherited size above 14,400 pt; give an explicit size")
	SourceUnreadable                    = define("source_unreadable", Error, Failed, "source file could not be read")
	SourceChanged                       = define("source_changed", Error, Failed, "source bytes at merge time differ from the inspected bytes")
	SourceInvalid                       = define("source_invalid", Error, Invalid, "unsupported unsafe PDF shape or parse/validation failure (including recovered panics)")
	SourceDamaged                       = define("source_damaged", Error, Invalid, "broken page tree (missing/empty kid, wrong count) or content-replacing repair")
	SourceEncrypted                     = define("source_encrypted", Error, Invalid, "encrypted, including empty-password encryption")
	SourceSigned                        = define("source_signed", Error, Invalid, "signature value, certification (`/Perms`) or document timestamp")
	SourceDynamicForm                   = define("source_dynamic_form", Error, Invalid, "source requests NeedsRendering true")
	SourceOptionalContent               = define("source_optional_content", Error, Invalid, "`/OCProperties` present")
	FitDestinationUnsupported           = define("fit_destination_unsupported", Error, Invalid, "fitting internal page destinations with numeric position coordinates is unsupported")
	SourceOutputIncompatible            = define("source_output_incompatible", Error, Invalid, "retained annotations lack appearances required by the effective PDF 2.0 output")
	FitFormUnsupported                  = define("fit_form_unsupported", Error, Invalid, "`fit_to` with a source that has form widgets")
	AssemblyFailed                      = define("assembly_failed", Error, Failed, "PDF/text backend failed during assembly or shaping (including recovered panics)")
	OutputWriteFailed                   = define("output_write_failed", Error, Failed, "writing the temporary output failed (disk full, permissions)")
	OutputInvalid                       = define("output_invalid", Error, Failed, "the written output did not validate")
	OutputPageCountMismatch             = define("output_page_count_mismatch", Error, Failed, "written page count \u2260 expected")
	OutputPublishFailed                 = define("output_publish_failed", Error, Failed, "the final rename failed (incl. no-replace unsupported and link fallback failed)")
	ReportWriteFailed                   = define("report_write_failed", Error, Failed, "the report could not be written")
	Interrupted                         = define("interrupted", Error, ClassInterrupted, "cancelled by a signal")
	TextOverflowAllowed                 = define("text_overflow_allowed", Warning, NoClass, "`overflow: allow` and measured text extends past the page or block")
	FormFieldsRenamed                   = define("form_fields_renamed", Warning, NoClass, "pdfcpu namespaces later nonempty forms even without collisions; build reports actual names")
	FormAppearanceRegenerationRequested = define("form_appearance_regeneration_requested", Warning, NoClass, "a source requests `NeedAppearances`; viewers may redraw all fields")
	DestinationsRenamed                 = define("destinations_renamed", Warning, NoClass, "colliding named destinations were renamed for later occurrences")
	DestinationsDropped                 = define("destinations_dropped", Warning, NoClass, "named destinations whose target is not a page of the same document were omitted")
	SourceXfaRemoved                    = define("source_xfa_removed", Warning, NoClass, "XFA removed; the static AcroForm remains")
	SourceStructureRemoved              = define("source_structure_removed", Warning, NoClass, "tagged-PDF structure removed; output is not tagged")
	SourcePageLabelsRemoved             = define("source_page_labels_removed", Warning, NoClass, "page labels removed")
	SourceOutputIntentsRemoved          = define("source_output_intents_removed", Warning, NoClass, "output intents removed; output makes no PDF/A or PDF/X claim")
	SourceDocumentActionsRemoved        = define("source_document_actions_removed", Warning, NoClass, "`/OpenAction`, catalog `/AA` or document JavaScript removed")
	SourceAttachmentsRemoved            = define("source_attachments_removed", Warning, NoClass, "embedded-files name tree or catalog `/AF` removed (page attachments remain)")
	WorkingDirectoryUnavailable         = define("working_directory_unavailable", Error, Failed, "The process working directory could not be determined")
	OutputInspectionFailed              = define("output_inspection_failed", Error, Failed, "The output destination could not be inspected")
	ReportInspectionFailed              = define("report_inspection_failed", Error, Failed, "The report destination could not be inspected")
)

var definitions = Registry()

// New constructs a diagnostic using its fixed registry severity.
func New(code Code, message string, items ...string) Diagnostic {
	definition, ok := definitions[code]
	if !ok {
		panic("undefined diagnostic: " + code.String())
	}
	return Diagnostic{Severity: definition.Severity, Code: code, Message: strings.ToValidUTF8(message, "�"), Items: items}
}

// Outcome gives cancellation precedence over environmental failure over invalid input.
func Outcome(diagnostics []Diagnostic) (Status, int) {
	status, exit := OK, 0
	for _, d := range diagnostics {
		if d.Severity != Error {
			continue
		}
		switch definitions[d.Code].Class {
		case NoClass:
			continue
		case ClassInterrupted:
			return StatusInterrupted, 130
		case Failed:
			status, exit = StatusFailed, 1
		case Invalid:
			if status == OK {
				status, exit = StatusInvalid, 2
			}
		}
	}
	return status, exit
}
