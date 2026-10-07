// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

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
		Location         *Location `json:"location,omitempty"`
		Stage            Stage     `json:"stage"`
		Code             Code      `json:"code"`
		Path             string    `json:"path,omitempty"`
		Message          string    `json:"message"`
		Consumers        []string  `json:"consumers,omitempty"`
		MessageTruncated bool      `json:"message_truncated,omitzero"`
	}

	// Summary is the compact default result. It is labeled as a summary, never lists every diagnostic of a
	// large failure, and points at the saved report for the rest.
	Summary struct {
		Counts                Counts           `json:"counts"`
		Phases                Phases           `json:"phases"`
		RecoveryDirectoryFrom string           `json:"recovery_directory_from,omitempty"`
		Command               string           `json:"command"`
		Kind                  string           `json:"kind"`
		Status                Status           `json:"status"`
		RecoveryBasename      string           `json:"recovery_basename,omitempty"`
		Publication           Publication      `json:"publication"`
		Next                  []string         `json:"next,omitempty"`
		Diagnostics           []DiagnosticView `json:"diagnostics"`
		TruncatedFields       []string         `json:"truncated_fields,omitempty"`
		ReportVersion         int              `json:"report_version"`
		PartCount             int              `json:"part_count"`
		DiagnosticCount       int              `json:"diagnostic_count"`
		DiagnosticsOmitted    int              `json:"diagnostics_omitted,omitzero"`
		NextOmitted           bool             `json:"next_omitted,omitzero"`
	}
)

const (
	// SummaryBytes is the maximum encoded default result, including its newline.
	SummaryBytes     = 2048
	previewBytes     = 96
	firstPrintable   = 0x20
	nextCommandBytes = 256
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
	return DiagnosticView{
		Consumers: slices.Clone(d.Consumers),
		Location:  d.Location,
		Stage:     d.Stage,
		Code:      d.Code,
		Path:      d.Path,
		Message:   d.Message,
	}
}

// previewDiagnosticView is a diagnostic with its message cut to PreviewRunes.
func previewDiagnosticView(d *Diagnostic) DiagnosticView {
	view := DiagnosticView{Location: d.Location, Stage: d.Stage, Code: d.Code, Path: d.Path, Message: d.Message}
	view.Message, view.MessageTruncated = preview(d.Message)

	return view
}

// Summary returns the compact result: counts, publication state, and the first PreviewDiagnostics
// diagnostics in report order.
func (r *Report) Summary() *Summary {
	shown := min(len(r.Diagnostics), PreviewDiagnostics)
	summary := &Summary{
		ReportVersion:      Version,
		Kind:               KindSummary,
		Status:             r.Status,
		Command:            r.Command,
		Phases:             r.Phases,
		Counts:             r.Counts,
		Publication:        r.Publication,
		PartCount:          len(r.Parts),
		DiagnosticCount:    len(r.Diagnostics),
		Diagnostics:        make([]DiagnosticView, shown),
		DiagnosticsOmitted: len(r.Diagnostics) - shown,
	}

	for i := range shown {
		summary.Diagnostics[i] = previewDiagnosticView(&r.Diagnostics[i])
	}

	if path := r.Publication.ReportPath; r.Publication.ReportStatus == ReportWritten {
		view := ViewParts
		if len(r.Diagnostics) > 0 {
			view = ViewDiagnostics
		}

		summary.Next = []string{"pdfconcat", reportCommand, path, "--view", view}
	}

	summary.bound()

	return summary
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

		if size > previewBytes {
			return value[:index], true
		}
	}

	return value, false
}

func (s *Summary) fits() bool {
	data, err := Encode(s)
	if err != nil || len(data) >= SummaryBytes {
		return false
	}

	var human strings.Builder

	textErr := s.RenderText(&human)

	return textErr == nil && human.Len() <= SummaryBytes
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
		s.RecoveryDirectoryFrom = "complete_report.publication.recovery_report"
	} else {
		s.RecoveryBasename = basename
		s.RecoveryDirectoryFrom = "original_report_argument"
	}

	s.Publication.RecoveryReport = ""
	s.TruncatedFields = append(s.TruncatedFields, "publication.recovery_report")
}

func (s *Summary) boundDiagnostic(index int) {
	diagnostic := &s.Diagnostics[index]
	prefix := "diagnostics/" + strconv.Itoa(index) + "/"
	s.cut(prefix+"path", &diagnostic.Path)
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

	message, truncated := BoundPreview(diagnostic.Message)
	diagnostic.Message = message
	diagnostic.MessageTruncated = diagnostic.MessageTruncated || truncated
}

func (s *Summary) bound() {
	if s.fits() {
		return
	}

	s.cut("command", &s.Command)
	s.cut("publication.output", &s.Publication.Output)
	s.cut("publication.report_path", &s.Publication.ReportPath)
	s.boundRecovery()

	if len(s.Next) > 0 {
		data, err := Encode(s.Next)
		if err != nil || len(data) > nextCommandBytes {
			s.Next = nil
			s.NextOmitted = true
		}
	}

	for index := range s.Diagnostics {
		s.boundDiagnostic(index)
	}

	for len(s.Diagnostics) > 0 && !s.fits() {
		s.omitDiagnostic()
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
