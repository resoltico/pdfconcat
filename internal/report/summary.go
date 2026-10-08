// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

type (
	// DiagnosticView is a diagnostic as a query or a summary shows it: the message is cut to PreviewRunes
	// characters and marked, unless the full record was asked for.
	DiagnosticView struct {
		Recovery           *Recovery `json:"recovery,omitempty"`
		Location           *Location `json:"location,omitempty"`
		Severity           Severity  `json:"severity"`
		ConsequenceContext string    `json:"consequence_context,omitempty"`
		Stage              Stage     `json:"stage"`
		Code               Code      `json:"code"`
		Path               string    `json:"path,omitempty"`
		Message            string    `json:"message"`
		Cause              string    `json:"cause,omitempty"`
		Consumers          []string  `json:"consumers,omitempty"`
		reportIndex        int
		MessageTruncated   bool `json:"message_truncated,omitzero"`
	}

	// Summary is the compact default result. It is labeled as a summary, never lists every diagnostic of a
	// large failure, and points at the saved report for the rest.
	Summary struct {
		Counts                Counts                 `json:"counts"`
		Next                  *[]string              `json:"next"`
		NextReference         *ContinuationReference `json:"next_reference,omitempty"`
		Phases                Phases                 `json:"phases"`
		RecoveryBasename      string                 `json:"recovery_basename,omitempty"`
		AttemptID             string                 `json:"attempt_id"`
		RecoveryDirectoryFrom string                 `json:"recovery_directory_from,omitempty"`
		Command               string                 `json:"command"`
		Kind                  string                 `json:"kind"`
		Status                Status                 `json:"status"`
		PublicationContext    string                 `json:"publication_context,omitempty"`
		Publication           Publication            `json:"publication"`
		Diagnostics           []DiagnosticView       `json:"diagnostics"`
		TruncatedFields       []string               `json:"truncated_fields,omitempty"`
		byteLimit             int
		FormatVersion         int  `json:"format_version"`
		PartCount             int  `json:"part_count"`
		DiagnosticCount       int  `json:"diagnostic_count"`
		ErrorCount            int  `json:"error_count"`
		WarningCount          int  `json:"warning_count"`
		DiagnosticsOmitted    int  `json:"diagnostics_omitted,omitzero"`
		NextOmitted           bool `json:"next_omitted,omitzero"`
	}
)

const (
	// SummaryBytes is the maximum encoded default result, including its newline.
	SummaryBytes        = 2048
	previewBytes        = 96
	messagePreviewBytes = 256
	firstPrintable      = 0x20
	// PreviewRunes caps a text or message preview, in Unicode scalar values.
	PreviewRunes = 160
	// PreviewDiagnostics is how many diagnostics a summary shows.
	PreviewDiagnostics = 5
)

// preview cuts text to PreviewRunes scalar values and says whether it cut anything.
func preview(text string) (string, bool) {
	count := 0

	for index := range text {
		if count == PreviewRunes {
			return text[:index], true
		}

		count++
	}

	return text, false
}

// fullDiagnosticView is the complete record of a diagnostic.
func fullDiagnosticView(d *Diagnostic) DiagnosticView {
	snapshot := cloneDiagnostic(*d)
	d = &snapshot

	return DiagnosticView{
		Severity: d.Severity, ConsequenceContext: d.ConsequenceContext,
		Recovery:  d.Recovery,
		Consumers: slices.Clone(d.Consumers),
		Location:  d.Location,
		Stage:     d.Stage,
		Code:      d.Code,
		Path:      d.Path,
		Message:   d.Message,
		Cause:     d.Cause,
	}
}

// previewDiagnosticView is a diagnostic with its message cut to PreviewRunes.
func previewDiagnosticView(d *Diagnostic) DiagnosticView {
	snapshot := cloneDiagnostic(*d)
	d = &snapshot

	view := DiagnosticView{
		Severity: d.Severity, ConsequenceContext: d.ConsequenceContext,
		Recovery: d.Recovery,
		Location: d.Location,
		Stage:    d.Stage,
		Code:     d.Code,
		Path:     d.Path,
		Message:  d.Message,
		Cause:    d.Cause,
	}
	view.Message, view.MessageTruncated = preview(d.Message)

	return view
}

// Summary returns the compact result: counts, publication state, and the first PreviewDiagnostics
// diagnostics, retaining the primary fault and prioritizing report-write recovery before other previews.
func (r *Report) Summary() *Summary {
	shown := min(len(r.Diagnostics), PreviewDiagnostics)
	summary := &Summary{
		FormatVersion:   Version,
		AttemptID:       r.AttemptID,
		Kind:            KindSummary,
		Status:          r.Status,
		Command:         r.Command,
		Phases:          r.Phases,
		Counts:          r.Counts,
		Publication:     r.Publication,
		PartCount:       len(r.Parts),
		DiagnosticCount: len(r.Diagnostics),
		ErrorCount:      r.ErrorCount, WarningCount: r.WarningCount,
		Diagnostics:        make([]DiagnosticView, shown),
		DiagnosticsOmitted: len(r.Diagnostics) - shown,
	}

	indices := summaryIndices(r.Diagnostics, shown)
	for index, original := range indices {
		summary.Diagnostics[index] = previewDiagnosticView(&r.Diagnostics[original])
		summary.Diagnostics[index].reportIndex = original
	}

	summary.bound()

	return summary
}

func summaryIndices(diagnostics []Diagnostic, shown int) []int {
	indices := make([]int, 0, shown)
	if shown == 0 {
		return indices
	}

	indices = appendSummarySeverity(indices, diagnostics, SeverityError, 1)
	if len(indices) == 0 {
		indices = appendSummarySeverity(indices, diagnostics, SeverityWarning, 1)
	}

	for i := range diagnostics {
		recovery := diagnostics[i].Recovery
		if recovery != nil && recovery.Action == recoveryChooseNewReport && !slices.Contains(indices, i) && len(indices) < shown {
			indices = append(indices, i)
			break
		}
	}

	indices = appendSummarySeverity(indices, diagnostics, SeverityError, shown)

	return appendSummarySeverity(indices, diagnostics, SeverityWarning, shown)
}

func appendSummarySeverity(indices []int, diagnostics []Diagnostic, severity Severity, limit int) []int {
	for i := 0; i < len(diagnostics) && len(indices) < limit; i++ {
		if diagnostics[i].Severity == severity && !slices.Contains(indices, i) {
			indices = append(indices, i)
		}
	}

	return indices
}

// ParseNumber parses a decimal integer flag value: optional minus sign, ASCII digits, nothing else, and it
// must fit an int64. Overflow is a usage error, never a wrapped value.
func ParseNumber(flag, text string) (int64, error) {
	digits := text
	digits = strings.TrimPrefix(digits, "-")

	if digits == "" || digits[0] < '0' || digits[0] > '9' {
		return 0, usage(CodeInvalidNumber, "%s needs a whole number, not %q", flag, text)
	}

	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, usage(CodeInvalidNumber, "%s needs a whole number that fits 64 bits, not %q", flag, text)
	}

	return value, nil
}

// BoundPreview limits a preview by encoded JSON bytes, preserving UTF-8 and marking omissions separately.
func BoundPreview(value string) (string, bool) {
	return boundPreviewBytes(value, previewBytes)
}

func boundPreviewBytes(value string, limit int) (string, bool) {
	size := 0

	for index, char := range value {
		switch {
		case char < firstPrintable:
			size += 6
		case char == '\\' || char == '"':
			size += 2
		default:
			size += utf8.RuneLen(char)
		}

		if size > limit {
			return value[:index], true
		}
	}

	return value, false
}

func (s *Summary) fits() bool {
	data, err := Encode(s)
	if err != nil || len(data) >= s.limit() {
		return false
	}

	var human strings.Builder

	textErr := s.RenderText(&human)

	return textErr == nil && human.Len() <= s.limit()
}

func (s *Summary) cut(name string, value *string) {
	bounded, truncated := BoundPreview(*value)
	*value = bounded

	if truncated {
		s.TruncatedFields = append(s.TruncatedFields, name)
	}
}

func (s *Summary) boundRecovery() {
	recovery := s.Publication.RecoveryReport
	if _, truncated := BoundPreview(recovery); !truncated {
		return
	}

	basename := filepath.Base(recovery)
	if _, truncated := BoundPreview(basename); truncated {
		s.RecoveryDirectoryFrom = historicalRecoveryReference
	} else {
		s.RecoveryBasename = basename
		s.RecoveryDirectoryFrom = "original_report_argument"

		if s.PublicationContext == "historical_target" {
			s.RecoveryDirectoryFrom = historicalRecoveryReference
		}
	}

	s.Publication.RecoveryReport = ""
	s.TruncatedFields = append(s.TruncatedFields, "publication.recovery_report")
}

func (s *Summary) boundDiagnostic(index int) {
	diagnostic := &s.Diagnostics[index]
	prefix := "diagnostics/" + strconv.Itoa(index) + "/"
	s.cut(prefix+"path", &diagnostic.Path)
	s.cut(prefix+"cause", &diagnostic.Cause)
	stage, code := string(diagnostic.Stage), string(diagnostic.Code)
	s.cut(prefix+"stage", &stage)
	s.cut(prefix+"code", &code)

	diagnostic.Stage, diagnostic.Code = Stage(stage), Code(code)
	if diagnostic.Location != nil {
		location := *diagnostic.Location
		diagnostic.Location = &location
		s.cut(prefix+"location/file", &location.File)
		s.cut(prefix+"location/pointer", &location.Pointer)
	}

	if diagnostic.Recovery != nil && diagnostic.Recovery.Location != nil {
		recovery := *diagnostic.Recovery
		location := recovery.Location
		_, fileCut := BoundPreview(location.File)

		_, pointerCut := BoundPreview(location.Pointer)
		if fileCut || pointerCut {
			recovery.Location = nil
			recovery.LocationFrom = "complete_report.diagnostics/" + strconv.Itoa(diagnostic.reportIndex) + "/recovery/location"
			diagnostic.Recovery = &recovery
		}
	}

	message, truncated := boundPreviewBytes(diagnostic.Message, messagePreviewBytes)
	diagnostic.Message = message
	diagnostic.MessageTruncated = diagnostic.MessageTruncated || truncated
}

func (s *Summary) bound() {
	if s.fits() {
		return
	}

	s.cut(memberCommand, &s.Command)
	s.cut("publication.output", &s.Publication.Output)
	s.cut("publication.report_path", &s.Publication.ReportPath)
	s.boundRecovery()

	for index := range s.Diagnostics {
		s.boundDiagnostic(index)
	}

	required := 1
	if len(s.Diagnostics) > 1 && s.Diagnostics[1].Recovery != nil && s.Diagnostics[1].Recovery.Action == recoveryChooseNewReport {
		required = 2
	}

	for len(s.Diagnostics) > required && !s.fits() {
		s.omitDiagnostic()
	}

	if !s.fits() && s.Next != nil {
		s.Next = nil
		s.NextOmitted = true
	}

	for index := range s.Diagnostics {
		if s.fits() {
			return
		}

		s.omitDiagnosticPreviews(index)
	}

	s.tightenPublicationPreviews()
	shrinkDiagnosticMessages(s.Diagnostics, s.fits)
}

func (s *Summary) tightenPublicationPreviews() {
	for limit := previewBytes / 2; limit >= previewBytes/8 && !s.fits(); limit /= 2 {
		for _, field := range []struct {
			value *string
			name  string
		}{
			{&s.Publication.Output, "publication.output"}, {&s.Publication.ReportPath, "publication.report_path"},
		} {
			value, cut := boundPreviewBytes(*field.value, limit)
			if cut {
				*field.value = value
				s.markTruncated(field.name)
			}
		}
	}
}

// Required scope/recovery fields take priority over long message previews; codes and remedies remain.
func shrinkDiagnosticMessages(records []DiagnosticView, fits func() bool) {
	for limit := messagePreviewBytes / 2; limit >= messagePreviewBytes/8 && !fits(); limit /= 2 {
		for index := range records {
			diagnostic := &records[index]

			message, cut := boundPreviewBytes(diagnostic.Message, limit)
			if cut {
				diagnostic.Message = message
				diagnostic.MessageTruncated = true
			}
		}
	}
}

// omitDiagnostic removes both a preview and its truncation labels, keeping labels tied to visible fields.
func (s *Summary) omitDiagnostic() {
	last := len(s.Diagnostics) - 1
	prefix := "diagnostics/" + strconv.Itoa(last) + "/"

	labels := s.TruncatedFields[:0]
	for _, label := range s.TruncatedFields {
		if !strings.HasPrefix(label, prefix) {
			labels = append(labels, label)
		}
	}

	s.TruncatedFields = labels
	s.Diagnostics = s.Diagnostics[:last]
	s.DiagnosticsOmitted++
}

func (s *Summary) limit() int {
	if s.byteLimit > 0 {
		return s.byteLimit
	}

	return SummaryBytes
}

// omitDiagnosticPreviews gives causal messages and recovery actions priority over optional foreign detail.
func (s *Summary) omitDiagnosticPreviews(index int) {
	diagnostic := &s.Diagnostics[index]
	prefix := "diagnostics/" + strconv.Itoa(index) + "/"

	for _, field := range []struct {
		value *string
		name  string
	}{
		{name: "cause", value: &diagnostic.Cause}, {name: "path", value: &diagnostic.Path},
	} {
		if *field.value != "" && !s.fits() {
			*field.value = ""
			s.markTruncated(prefix + field.name)
		}
	}

	if diagnostic.Location == nil || s.fits() {
		return
	}

	diagnostic.Location = nil

	labels := s.TruncatedFields[:0]
	for _, label := range s.TruncatedFields {
		if !strings.HasPrefix(label, prefix+"location/") {
			labels = append(labels, label)
		}
	}

	s.TruncatedFields = labels
	s.markTruncated(prefix + "location")
}

func (s *Summary) markTruncated(field string) {
	if !slices.Contains(s.TruncatedFields, field) {
		s.TruncatedFields = append(s.TruncatedFields, field)
	}
}
