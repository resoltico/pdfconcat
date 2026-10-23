// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"
	"encoding/json/v2"
	"errors"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/files"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/result"
)

func knownCount(n int64) *int64           { return &n }
func hasErrors(ds []diag.Diagnostic) bool { status, _ := diag.Outcome(ds); return status != "ok" }
func ioDiagnostic(code diag.Code, err error, file string) diag.Diagnostic {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code = "interrupted"
	}
	d := diag.New(code, err.Error())
	if file != "<stdin>" {
		d.File = file
	}
	return d
}

func declaredReportPart(leaf packet.Leaf) result.Part {
	p := result.Part{Item: leaf.Pointer, Kind: leaf.Kind, Path: leaf.Source, Section: leaf.Owner}
	switch leaf.Kind {
	case "source":
		p.FieldsRenamed = []packet.FieldRename{}
	case "page", "divider":
		p.Count = leaf.Count
	}
	return p
}
func reportSections(sections []packet.Section, resolved bool) []result.Section {
	resultSections := make([]result.Section, 0, len(sections))
	for _, section := range sections {
		s := result.Section{Item: section.Item, Title: section.Title}
		if section.Parent != "" {
			parent := section.Parent
			s.Parent = &parent
		}
		if resolved {
			s.TargetPage = knownCount(section.Target)
			s.OutputPages = &[2]int64{section.FirstPage, section.LastPage}
		}
		resultSections = append(resultSections, s)
	}
	return resultSections
}

func writeReport(r *result.Result, roles files.Files) {
	if r.Report == nil {
		return
	}
	refusals := roles.CheckReport()
	r.Diagnostics = append(r.Diagnostics, refusals...)
	if hasErrors(refusals) {
		return
	}
	full := r.Full()
	report := *r.Report
	report.Written = true
	full.Report = &report
	data, err := json.Marshal(full)
	if err == nil {
		var staged *files.File
		staged, err = files.Create(roles.Report)
		if err == nil {
			defer staged.Abort()
			_, err = staged.Write(append(data, '\n'))
			if err == nil {
				r.Report.Written, err = staged.Commit(roles.Overwrite && roles.InputsKnown)
			}
		}
	}
	if err != nil {
		d := diag.New("report_write_failed", err.Error())
		d.File = roles.Report
		r.Diagnostics = append(r.Diagnostics, d)
	}
}

func noteCancellation(c context.Context, r *result.Result) {
	if err := c.Err(); err != nil {
		for _, d := range r.Diagnostics {
			if d.Code == "interrupted" {
				return
			}
		}
		r.Diagnostics = append(r.Diagnostics, diag.New("interrupted", err.Error()))
	}
}
