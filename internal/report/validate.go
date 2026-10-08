// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report

import (
	"math"
	"regexp"
	"strconv"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

type (
	// fault is a shape violation at a JSON pointer.
	fault struct {
		pointer string
		message string
		code    Code
	}

	// validator walks a report once.
	validator struct {
		report *Report
	}
)

const (
	pointerReportWrite = "/publication/report_write"
	pointerStatus      = "/status"
	pointerDiagnostics = "/diagnostics"
	pointerCounts      = "/counts"
	pointerSources     = "/sources"
	pointerParts       = "/parts"
	maxPageSide        = float64(assembly.MaxLength)
	maxOffset          = float64(assembly.MaxLength)
	minLeading         = assembly.MinLeading
	maxLeading         = assembly.MaxLeading
	maxPartPages       = assembly.MaxBlankCount
	digestHexSize      = 64

	// memoryFile names a report that is validated in memory rather than read from a file.
	memoryFile = "report"
)

// Name patterns shared with the schema.
var (
	namePattern              = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	commandPattern           = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	idPattern                = regexp.MustCompile(`^(argv:(0|[1-9]\d*)|(/items/(0|[1-9]\d*))+)$`)
	colorPattern             = regexp.MustCompile(`^#[0-9a-f]{6}$`)
	digestPattern            = regexp.MustCompile(`^[0-9a-f]{64}$`)
	attemptPattern           = regexp.MustCompile(`^[A-Z2-7]{26}$`)
	recoveryCommandPattern   = regexp.MustCompile(`^(|build|check|report|schema|version|help)$`)
	replacementPattern       = regexp.MustCompile(`^(|--[a-z][a-z0-9-]{0,62}|-[a-zA-Z])$`)
	locationReferencePattern = regexp.MustCompile(
		`^(|original_argv|original_argv\.report_operand|complete_report\.diagnostics/[0-9]{1,7}/recovery/location)$`,
	)
)

func newFault(pointer string, code Code, message string) *fault {
	return &fault{pointer: pointer, code: code, message: message}
}

func at(pointer string, index int, member string) string {
	return pointer + "/" + strconv.Itoa(index) + member
}

// Validate checks every invariant of a report that JSON syntax and types cannot: enumerations, references,
// ranges, counts, and the relations between status, phases, and publication. It returns an *Error.
func (r *Report) Validate() error {
	found := (&validator{report: r}).run()
	if found == nil {
		return nil
	}

	return invalid(StageShape, found.code, &Location{File: memoryFile, Pointer: found.pointer}, "%s", found.message)
}

func (v *validator) identity() *fault {
	report := v.report

	if report.Producer != nil {
		fields := []struct{ name, value string }{
			{"tool", report.Producer.Tool},
			{"version", report.Producer.Version},
			{"commit", report.Producer.Commit},
			{"go", report.Producer.Go},
			{"platform", report.Producer.Platform},
		}
		for _, field := range fields {
			if field.value == "" || utf8.RuneCountInString(field.value) > 256 {
				return newFault("/producer/"+field.name, CodeInvalidValue, "producer identity must be 1–256 characters")
			}
		}
	}

	if report.Publication.OutputDigest != "" && !digestPattern.MatchString(report.Publication.OutputDigest) {
		return newFault("/publication/output_digest", CodeInvalidValue, "output_digest must be a lowercase SHA-256 digest")
	}

	return nil
}

func (v *validator) run() *fault {
	r := v.report
	if found := v.identity(); found != nil {
		return found
	}

	if !attemptPattern.MatchString(r.AttemptID) {
		return newFault("/attempt_id", CodeInvalidValue, "attempt_id must be a 26-character base32 opaque identity")
	}

	if r.FormatVersion != Version {
		return newFault("/format_version", CodeUnsupportedVersion, "format_version must be "+strconv.Itoa(Version))
	}

	if r.Kind != KindReport {
		return newFault("/kind", CodeWrongKind, `kind must be "report"`)
	}

	for _, check := range []func() *fault{
		v.header, v.counts, v.publication, v.diagnostics, v.sources, v.fonts, v.styles, v.parts, v.layout, v.consumerLinks,
	} {
		if found := check(); found != nil {
			return found
		}
	}

	return nil
}

func validState(s PhaseState) bool {
	return s == PhaseNotRun || s == PhaseIncomplete || s == PhaseComplete
}

func (v *validator) header() *fault {
	rep := v.report

	switch rep.Status {
	case StatusOK, StatusInvalid, StatusFailed, StatusInterrupted:
	default:
		return newFault(pointerStatus, CodeInvalidValue, "status must be ok, invalid, failed, or interrupted")
	}

	if !commandPattern.MatchString(rep.Command) {
		return newFault("/command", CodeInvalidValue, "command must be lower-case words joined by hyphens")
	}

	found := phasesFault(&rep.Phases)
	if found != nil {
		return found
	}

	return v.statusRelations()
}

// phasesFault checks that every phase state is known and that no phase is complete after one that is not.
func phasesFault(phases *Phases) *fault {
	entries := []struct {
		name  string
		state PhaseState
	}{
		{"instructions", phases.Instructions},
		{"input_inspection", phases.InputInspection},
		{"layout", phases.Layout},
		{"output_verification", phases.OutputVerification},
	}

	previousName, previousComplete := "", true

	for _, entry := range entries {
		if !validState(entry.state) {
			return newFault("/phases/"+entry.name, CodeInvalidValue, "phase state must be not_run, incomplete, or complete")
		}

		if entry.state == PhaseComplete && !previousComplete {
			return newFault("/phases/"+entry.name, CodeInvalidValue, entry.name+" cannot be complete when "+previousName+" is not")
		}

		previousName, previousComplete = entry.name, entry.state == PhaseComplete
	}

	return nil
}

func (v *validator) statusRelations() *fault {
	rep := v.report

	if rep.Status != StatusOK {
		if len(rep.Diagnostics) == 0 {
			return newFault(pointerDiagnostics, CodeInvalidValue, "a report whose status is not ok needs at least one diagnostic")
		}

		return nil
	}

	if len(rep.Diagnostics) > 0 {
		return newFault(pointerDiagnostics, CodeInvalidValue, "a report whose status is ok has no diagnostics")
	}

	if rep.Command != commandBuild && rep.Command != commandCheck {
		return newFault(pointerStatus, CodeInvalidValue, "only build and check can report ok")
	}

	if rep.Phases.Layout != PhaseComplete {
		return newFault(pointerStatus, CodeInvalidValue, "status ok needs instructions, input inspection, and layout complete")
	}

	if rep.Command == commandBuild && (rep.Phases.OutputVerification != PhaseComplete || !rep.Publication.Published) {
		return newFault(pointerStatus, CodeInvalidValue, "a build is ok only when its output is verified and published")
	}

	return nil
}

func countFault(pointer string, count *int64) *fault {
	if count != nil && *count < 0 {
		return newFault(pointer, CodeInvalidValue, "a count must not be negative")
	}

	return nil
}

func (v *validator) counts() *fault {
	counts := v.report.Counts

	entries := []struct {
		count   *int64
		pointer string
	}{
		{counts.SourcePages, "/counts/source_pages"},
		{counts.GeneratedPages, "/counts/generated_pages"},
		{counts.TotalPages, "/counts/total_pages"},
	}

	for _, entry := range entries {
		if found := countFault(entry.pointer, entry.count); found != nil {
			return found
		}
	}

	if counts.SourcePages == nil || counts.GeneratedPages == nil || counts.TotalPages == nil {
		return nil
	}

	switch {
	case *counts.SourcePages > math.MaxInt64-*counts.GeneratedPages:
		return newFault(pointerCounts, CodeInvalidValue, "counts overflow")
	case *counts.SourcePages+*counts.GeneratedPages != *counts.TotalPages:
		return newFault("/counts/total_pages", CodeInvalidValue, "total_pages must equal source_pages plus generated_pages")
	default:
		return nil
	}
}

func validWriteState(state WriteState) bool {
	return state == ReportNotRequested || state == ReportWritten || state == ReportFailed
}

func (v *validator) publication() *fault {
	pub := v.report.Publication
	if found := unresolvedReportFault(pub); found != nil {
		return found
	}

	if found := requestedReportPathFault(pub); found != nil {
		return found
	}

	if found := reportWriteFault(pub); found != nil {
		return found
	}

	if found := recoveryStateFault(&pub); found != nil {
		return found
	}

	switch {
	case !validWriteState(pub.ReportStatus):
		return newFault("/publication/report_status", CodeInvalidValue, "report_status must be not_requested, written, or failed")
	case pub.Published && pub.Output == "":
		return newFault("/publication/output", CodeInvalidValue, "a published output needs its path")
	case pub.ReportStatus == ReportNotRequested && pub.ReportPath != "":
		return newFault("/publication/report_path", CodeInvalidValue, "report_path needs a requested report")
	default:
		return recoveryRelationship(pub)
	}
}

func recoveryRelationship(pub Publication) *fault {
	if pub.RecoveryReport != "" && (pub.ReportStatus != ReportFailed || !pub.Published) {
		return newFault(
			"/publication/recovery_report",
			CodeInvalidValue,
			"a recovery reference is only valid after a PDF commit and failed report publication",
		)
	}

	return nil
}

func (v *validator) diagnostics() *fault {
	for index := range v.report.Diagnostics {
		diagnostic := &v.report.Diagnostics[index]

		switch {
		case !namePattern.MatchString(string(diagnostic.Stage)):
			return newFault(at(pointerDiagnostics, index, "/stage"), CodeInvalidValue, "stage must be lower-case snake_case")
		case !namePattern.MatchString(string(diagnostic.Code)):
			return newFault(at(pointerDiagnostics, index, "/code"), CodeInvalidValue, "code must be lower-case snake_case")
		case diagnostic.Message == "":
			return newFault(at(pointerDiagnostics, index, "/message"), CodeInvalidValue, "a diagnostic needs an actionable message")
		}

		if diagnostic.Recovery != nil {
			if found := recoveryFault(at(pointerDiagnostics, index, "/recovery"), diagnostic.Recovery); found != nil {
				return found
			}
		}

		if diagnostic.Location != nil {
			if found := locationFault(at(pointerDiagnostics, index, "/location"), diagnostic.Location); found != nil {
				return found
			}
		}
	}

	return nil
}

func locationFault(pointer string, loc *Location) *fault {
	position := loc.position()

	found := positionFault(pointer, &position)
	if found != nil {
		return found
	}

	if loc.ArgvIndex != nil && (*loc.ArgvIndex < 0 || loc.Pointer != "") {
		return newFault(pointer+"/argv_index", CodeInvalidValue, "argv_index is a non-negative index and excludes pointer")
	}

	return nil
}

func positionFault(pointer string, pos *Position) *fault {
	switch {
	case pos.File == "":
		return newFault(pointer+"/file", CodeInvalidValue, "a location needs its file")
	case pos.Offset != nil && (*pos.Offset < 0 || pos.Line < 1 || pos.Column < 1):
		return newFault(pointer+"/offset", CodeInvalidValue, "a byte offset needs a line and column of at least 1")
	case pos.Offset == nil && (pos.Line != 0 || pos.Column != 0):
		return newFault(pointer+"/line", CodeInvalidValue, "a line and column need a byte offset")
	default:
		return nil
	}
}

func (v *validator) sources() *fault {
	for index, s := range v.report.Sources {
		switch {
		case s.Path == "":
			return newFault(at(pointerSources, index, "/path"), CodeInvalidValue, "a source needs its path")
		case s.Digest != "" && !digestPattern.MatchString(s.Digest):
			return newFault(at(pointerSources, index, "/digest"), CodeInvalidValue, "digest must be 64 lower-case hexadecimal digits")
		case s.Bytes != nil && *s.Bytes < 0:
			return newFault(at(pointerSources, index, "/bytes"), CodeInvalidValue, "bytes must not be negative")
		}
	}

	return nil
}

func (v *validator) fonts() *fault {
	for index, f := range v.report.Fonts {
		switch {
		case !digestPattern.MatchString(f.Digest):
			return newFault(at("/fonts", index, "/digest"), CodeInvalidValue, "digest must be 64 lower-case hexadecimal digits")
		case f.Name == "":
			return newFault(at("/fonts", index, "/name"), CodeInvalidValue, "a font needs its name")
		}
	}

	return nil
}

func within(value, low, high float64) bool {
	return value >= low && value <= high
}

func (v *validator) styles() *fault {
	for index := range v.report.Styles {
		style := &v.report.Styles[index]
		pointer := at("/styles", index, "")

		found := styleFault(pointer, style)
		if found != nil {
			return found
		}

		if style.Text != nil {
			found = v.text(pointer+"/text", style.Text)
			if found != nil {
				return found
			}
		}
	}

	return nil
}

func styleFault(pointer string, style *Style) *fault {
	size := &style.Size

	switch {
	case size.Origin != SizeExplicit && size.Origin != SizeFollowingSource && size.Origin != SizePrecedingSource:
		return newFault(pointer+"/size/origin", CodeInvalidValue, "size origin must be explicit, following_source, or preceding_source")
	case !within(size.Width, float64(assembly.MinPageSide), maxPageSide) ||
		!within(size.Height, float64(assembly.MinPageSide), maxPageSide):
		return newFault(pointer+"/size", CodeInvalidValue, "page sides must be between 1 and 14400 points")
	case style.Background != BackgroundNone && !colorPattern.MatchString(style.Background):
		return newFault(pointer+"/background", CodeInvalidValue, `background must be "none" or "#rrggbb"`)
	default:
		return nil
	}
}

func (v *validator) text(pointer string, text *Text) *fault {
	if text.Font < 0 || text.Font >= len(v.report.Fonts) {
		return newFault(pointer+"/font", CodeDanglingReference, "font index is outside the fonts table")
	}

	found := textContentFault(pointer, &text.TextSettings)
	if found != nil {
		return found
	}

	found = textGeometryFault(pointer, &text.TextSettings)
	if found != nil {
		return found
	}

	return textFindingsFault(pointer, text.Findings)
}

func textContentFault(pointer string, text *TextSettings) *fault {
	switch {
	case utf8.RuneCountInString(text.Value) > MaxTextRunes:
		return newFault(pointer+"/value", CodeInvalidValue, "text exceeds "+strconv.Itoa(MaxTextRunes)+" characters")
	case !colorPattern.MatchString(text.Color):
		return newFault(pointer+"/color", CodeInvalidValue, `color must be "#rrggbb"`)
	case !validAnchor(text.Anchor):
		return newFault(pointer+"/anchor", CodeInvalidValue, "anchor must be one of the nine anchor names")
	case !validAlign(text.Align):
		return newFault(pointer+"/align", CodeInvalidValue, "align must be left, center, right, or justify")
	case text.Overflow != assembly.OverflowError.String() && text.Overflow != assembly.OverflowAllow.String():
		return newFault(pointer+"/overflow", CodeInvalidValue, "overflow must be error or allow")
	default:
		return nil
	}
}

func textGeometryFault(pointer string, text *TextSettings) *fault {
	switch {
	case !within(text.Size, float64(assembly.MinFontSize), maxPageSide):
		return newFault(pointer+"/size", CodeInvalidValue, "size must be between 1 and 14400 points")
	case !within(text.X, -maxOffset, maxOffset) || !within(text.Y, -maxOffset, maxOffset):
		return newFault(pointer+"/x", CodeInvalidValue, "offsets must be within 14400 points")
	case !within(text.Width, float64(assembly.MinPageSide), maxPageSide):
		return newFault(pointer+"/width", CodeInvalidValue, "wrap width must be between 1 and 14400 points")
	case !within(text.Leading, minLeading, maxLeading):
		return newFault(pointer+"/leading", CodeInvalidValue, "leading must be between 1 and 10")
	case text.Bounds != nil && !validBounds(text.Bounds):
		return newFault(pointer+"/bounds", CodeInvalidValue, "bounds must be finite with non-negative size")
	case text.InkBounds != nil && !validBounds(text.InkBounds):
		return newFault(pointer+"/ink_bounds", CodeInvalidValue, "ink bounds must be finite with non-negative size")
	default:
		return nil
	}
}

// validBounds checks computed metadata, whose magnitude never drives allocation or rendering.
func validBounds(bounds *Rect) bool {
	for _, value := range []float64{bounds.X, bounds.Y, bounds.Width, bounds.Height} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}

	return bounds.Width >= 0 && bounds.Height >= 0
}

func validAnchor(name string) bool {
	_, err := assembly.ParseAnchor(name)

	return err == nil
}

func validAlign(name string) bool {
	_, err := assembly.ParseTextAlign(name)

	return err == nil
}

func (v *validator) parts() *fault {
	seen := make(map[string]struct{}, len(v.report.Parts))
	previousEnd := int64(0)

	for index := range v.report.Parts {
		part := &v.report.Parts[index]
		pointer := at(pointerParts, index, "")

		found := v.partShape(pointer, part)
		if found != nil {
			return found
		}

		if _, duplicate := seen[part.ID]; duplicate {
			return newFault(pointer+"/id", CodeDuplicateID, "duplicate part id "+part.ID)
		}

		seen[part.ID] = struct{}{}

		if part.Range == nil {
			continue
		}

		if part.Range.Start <= previousEnd {
			return newFault(pointer+"/range", CodeInvalidRange, "part ranges must be ascending and must not overlap")
		}

		previousEnd = part.Range.End
	}

	return nil
}

// partShape checks one part in the order of its members: identity, kind, references, then extent.
func (v *validator) partShape(pointer string, part *Part) *fault {
	for _, check := range []func(string, *Part) *fault{partIdentityFault, partKindFault, v.partReferenceFault, partExtentFault} {
		found := check(pointer, part)
		if found != nil {
			return found
		}
	}

	return positionFault(pointer+"/origin", &part.Origin)
}

func partIdentityFault(pointer string, part *Part) *fault {
	switch {
	case !idPattern.MatchString(part.ID):
		return newFault(pointer+"/id", CodeInvalidValue, "id must be a /items/N pointer or argv:N")
	case part.Origin.File == "":
		return newFault(pointer+"/origin/file", CodeInvalidValue, "a part needs the file it was written in")
	default:
		return nil
	}
}

func partKindFault(pointer string, part *Part) *fault {
	switch {
	case part.Kind == PartPDF && (part.Source == nil || part.Style != nil):
		return newFault(pointer+"/source", CodeInvalidValue, "a pdf part references a source and no style")
	case part.Kind == PartBlank && part.Source != nil:
		return newFault(pointer+"/source", CodeInvalidValue, "a blank part references a style and no source")
	case part.Kind != PartPDF && part.Kind != PartBlank:
		return newFault(pointer+"/kind", CodeInvalidValue, "kind must be pdf or blank")
	default:
		return nil
	}
}

func (v *validator) partReferenceFault(pointer string, part *Part) *fault {
	switch {
	case part.Source != nil && (*part.Source < 0 || *part.Source >= len(v.report.Sources)):
		return newFault(pointer+"/source", CodeDanglingReference, "source index is outside the sources table")
	case part.Style != nil && (*part.Style < 0 || *part.Style >= len(v.report.Styles)):
		return newFault(pointer+"/style", CodeDanglingReference, "style index is outside the styles table")
	default:
		return nil
	}
}

func partExtentFault(pointer string, part *Part) *fault {
	switch {
	case part.Pages != nil && (*part.Pages < 1 || (part.Kind == PartBlank && *part.Pages > maxPartPages)):
		return newFault(pointer+"/pages", CodeInvalidValue, "pages must be at least 1 (at most 1000000 for a blank)")
	case part.Range != nil && !spansPages(part.Range, part.Pages):
		return newFault(pointer+"/range", CodeInvalidRange, "a range runs from 1 up and spans exactly the part's page count")
	default:
		return nil
	}
}

// spansPages is true when the range starts at page 1 or later and covers exactly the given page count.
func spansPages(pageRange *PageRange, pages *int64) bool {
	return pages != nil && pageRange.Start >= 1 && pageRange.End >= pageRange.Start && pageRange.End-pageRange.Start+1 == *pages
}

// layout checks the relations that hold once layout is complete: every part placed, no gaps, the totals
// matching the last range.
func (v *validator) layout() *fault {
	rep := v.report

	if rep.Phases.Layout != PhaseComplete {
		return nil
	}

	counts := rep.Counts
	if counts.SourcePages == nil || counts.GeneratedPages == nil || counts.TotalPages == nil {
		return newFault(pointerCounts, CodeInvalidValue, "counts are required once layout is complete")
	}

	next := int64(1)
	generated := int64(0)

	for index := range rep.Parts {
		part := &rep.Parts[index]

		found := placementFault(index, part, next)
		if found != nil {
			return found
		}

		next = part.Range.End + 1

		if part.Kind == PartBlank {
			generated += *part.Pages
		}
	}

	if next-1 != *counts.TotalPages || generated != *counts.GeneratedPages {
		return newFault(pointerCounts, CodeInvalidRange, "counts must match the part ranges")
	}

	return nil
}

// placementFault checks that a part of a completed layout starts at page next and is fully resolved.
func placementFault(index int, part *Part, next int64) *fault {
	switch {
	case part.Range == nil:
		return newFault(at(pointerParts, index, "/range"), CodeInvalidRange, "every part needs its range once layout is complete")
	case part.Range.Start != next:
		return newFault(
			at(pointerParts, index, "/range"),
			CodeInvalidRange,
			"ranges must be contiguous from page 1 once layout is complete",
		)
	case part.Kind == PartBlank && part.Style == nil:
		return newFault(
			at(pointerParts, index, "/style"),
			CodeInvalidValue,
			"a blank part needs its resolved style once layout is complete",
		)
	default:
		return nil
	}
}

func textFindingsFault(pointer string, findings []TextFinding) *fault {
	if len(findings) > MaxTextRunes {
		return newFault(pointer+"/findings", CodeInvalidValue, "too many text findings")
	}

	for index, finding := range findings {
		validLine := finding.Line == -1
		switch finding.Kind {
		case "word-wider-than-box":
			validLine = finding.Line >= 0 && finding.Line < MaxTextRunes
		case "outside-page-horizontal", "outside-page-vertical":
		default:
			return newFault(at(pointer+"/findings", index, "/kind"), CodeInvalidValue, "unknown text finding kind")
		}

		if !validLine || finding.Detail == "" {
			return newFault(at(pointer+"/findings", index, ""), CodeInvalidValue, "text finding needs a valid line and detail")
		}
	}

	return nil
}

func recoveryStateFault(pub *Publication) *fault {
	if pub.RecoveryReport == "" {
		if pub.RecoveryState != "" {
			return newFault("/publication/recovery_state", CodeInvalidValue, "recovery_state needs a recovery reference")
		}

		return nil
	}

	switch pub.RecoveryState {
	case RecoveryCurrent, RecoveryPending, RecoveryUnavailable:
		return nil
	default:
		return newFault(
			"/publication/recovery_state",
			CodeInvalidValue,
			"recovery_state must be current, publication_pending, or unavailable",
		)
	}
}

func (v *validator) consumerLinks() *fault {
	parts := map[string]bool{}
	for _, part := range v.report.Parts {
		parts[part.ID] = true
	}

	for index := range v.report.Diagnostics {
		seen := map[string]bool{}

		for consumerIndex, id := range v.report.Diagnostics[index].Consumers {
			pointer := at(at(pointerDiagnostics, index, "/consumers"), consumerIndex, "")
			if !parts[id] {
				return newFault(pointer, CodeDanglingReference, "consumer does not identify a recorded part")
			}

			if seen[id] {
				return newFault(pointer, CodeDuplicateID, "duplicate affected consumer")
			}

			seen[id] = true
		}
	}

	return nil
}

func recoveryFault(pointer string, recovery *Recovery) *fault {
	if found := recoveryReferencesFault(pointer, recovery); found != nil {
		return found
	}

	valid := false

	switch recovery.Action {
	case "open_help":
		valid = true
	case "edit_input":
		valid = recovery.Location != nil || recovery.LocationFrom != ""
	case recoveryChooseNewReport, "inspect_report":
		valid = recovery.ReportFrom != ""
	case "recover_report":
		valid = recovery.ReportFrom != "" && recovery.RecoveryFrom != ""
	default:
	}

	if !valid {
		return newFault(pointer, CodeInvalidValue, "recovery needs a supported action and its required reference")
	}

	if recovery.Location != nil {
		return locationFault(pointer+"/location", recovery.Location)
	}

	return nil
}

func reportWriteFault(pub Publication) *fault {
	switch {
	case pub.ReportWrite != "" && pub.ReportWrite != "written" && pub.ReportWrite != attemptNotWritten:
		return newFault(pointerReportWrite, CodeInvalidValue, "report_write must be written or not_written")
	case pub.ReportTargetObservation != "" && pub.ReportTargetObservation != unknownMetadataValue &&
		pub.ReportTargetObservation != "absent_when_observed":
		return newFault(
			"/publication/report_target_observation",
			CodeInvalidValue,
			"report_target_observation must be unknown or absent_when_observed",
		)
	case pub.ReportWrite == "written" && pub.ReportStatus != ReportWritten:
		return newFault(pointerReportWrite, CodeInvalidValue, "written needs successful report publication")
	case pub.ReportWrite == attemptNotWritten && pub.ReportStatus == ReportWritten:
		return newFault(pointerReportWrite, CodeInvalidValue, "not_written excludes successful report publication")
	default:
		return nil
	}
}

func recoveryReferencesFault(pointer string, recovery *Recovery) *fault {
	switch {
	case !recoveryCommandPattern.MatchString(recovery.Command):
		return newFault(pointer+"/command", CodeInvalidValue, "recovery command must be a supported command or empty for root help")
	case !replacementPattern.MatchString(recovery.Replacement):
		return newFault(pointer+"/replacement", CodeInvalidValue, "replacement must be a bounded option spelling")
	case !locationReferencePattern.MatchString(recovery.LocationFrom):
		return newFault(
			pointer+"/location_from",
			CodeInvalidValue,
			"location_from must reference original argv or a complete report recovery location",
		)
	case recovery.ReportFrom != "" && recovery.ReportFrom != jobReportReference &&
		recovery.ReportFrom != queryReportReference && recovery.ReportFrom != "unused_report_target":
		return newFault(
			pointer+"/report_from",
			CodeInvalidValue,
			"report_from must identify the original report argument or an unused target",
		)
	case recovery.RecoveryFrom != "" && recovery.RecoveryFrom != "publication.recovery_report":
		return newFault(pointer+"/recovery_from", CodeInvalidValue, "recovery_from must identify publication.recovery_report")
	default:
		return nil
	}
}

func unresolvedReportFault(pub Publication) *fault {
	if pub.ReportFrom == "" {
		return nil
	}

	if pub.ReportFrom != jobReportReference || pub.ReportPath != "" || pub.ReportStatus != ReportFailed ||
		pub.ReportWrite != attemptNotWritten || pub.ReportTargetObservation != unknownMetadataValue {
		return newFault("/publication/report_from", CodeInvalidValue,
			"report_from is only original_argv.--report for an unresolved, failed report target that was not written and remains unknown")
	}

	return nil
}

func requestedReportPathFault(pub Publication) *fault {
	if pub.ReportStatus != ReportWritten && pub.ReportStatus != ReportFailed {
		return nil
	}

	if pub.ReportPath == "" && pub.ReportFrom == "" {
		return newFault("/publication/report_path", CodeInvalidValue,
			"a requested report needs its path or an unresolved invocation reference")
	}

	return nil
}
