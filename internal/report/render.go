// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"fmt"
	"io"
	"strconv"
)

// textWriter keeps the first write error, so a renderer reports one failure instead of checking every line.
type textWriter struct {
	writer io.Writer
	err    error
}

func (t *textWriter) linef(format string, args ...any) {
	if t.err == nil {
		_, t.err = fmt.Fprintf(t.writer, format+"\n", args...)
	}
}

// RenderText writes the summary as human text: the same bounded preview as the JSON summary.
func (s *Summary) RenderText(w io.Writer) error {
	out := &textWriter{writer: w}
	out.linef("attempt: %s", s.AttemptID)
	out.overview(s.Status, s.Command, s.Phases, s.Counts, s.Publication)
	out.linef("diagnostic records: %d errors, %d warnings", s.ErrorCount, s.WarningCount)
	out.linef("summary only: %d parts, %d diagnostics (%d shown)", s.PartCount, s.DiagnosticCount, len(s.Diagnostics))

	for i := range s.Diagnostics {
		out.diagnostic(&s.Diagnostics[i])
	}

	if len(s.TruncatedFields) > 0 {
		out.linef("truncated previews: %v", s.TruncatedFields)
	}

	if s.RecoveryBasename != "" {
		out.linef("recovery basename: %q; directory reference: %s", s.RecoveryBasename, s.RecoveryDirectoryFrom)
	}

	if s.NextOmitted {
		out.linef("next command omitted: use the original report argument")
	}

	if s.Next != nil {
		out.linef("more: %v", *s.Next)
	}

	return out.err
}

// RenderText writes every complete report value as readable JSON for explicit human details.
func (r *Report) RenderText(w io.Writer) error { return renderComplete(w, r) }

func renderComplete(w io.Writer, value any) error {
	data, err := EncodePretty(value)
	if err != nil {
		return err
	}

	if _, err = w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write complete text detail: %w", err)
	}

	return nil
}

// RenderText writes the part as human text.
func (p *PartResponse) RenderText(w io.Writer) error {
	if p.Part.Source != nil || p.Part.Style != nil {
		return renderComplete(w, p)
	}

	out := &textWriter{writer: w}
	out.part(&p.Part)

	return out.err
}

// RenderText writes the page's part as human text.
func (p *PageResponse) RenderText(w io.Writer) error {
	if p.Part.Source != nil || p.Part.Style != nil {
		return renderComplete(w, p)
	}

	out := &textWriter{writer: w}
	out.linef("page %d is page %d of its part", p.Page, p.PageInPart)
	out.part(&p.Part)

	return out.err
}

// RenderText writes the page of records as human text.
func (v *ViewResponse[T]) RenderText(w io.Writer) error {
	out := &textWriter{writer: w}

	more := "no more"
	if v.NextOffset != nil {
		more = "next offset " + strconv.FormatInt(*v.NextOffset, 10)
	}

	out.linef("%s: %d of %d from offset %d (%s)", v.View, v.Returned, v.Total, v.Offset, more)

	if v.OversizedRecord {
		out.linef("this one record is larger than the response bound and is returned alone")
	}

	for i := range v.Records {
		switch record := any(&v.Records[i]).(type) {
		case *PartView:
			out.part(record)
		case *DiagnosticView:
			out.diagnostic(record)
		default:
		}
	}

	return out.err
}

func count(c *int64) string {
	if c == nil {
		return unknownMetadataValue
	}

	return strconv.FormatInt(*c, 10)
}

func (t *textWriter) overview(status Status, command string, phases Phases, counts Counts, pub Publication) {
	t.linef("%s %s", command, status)
	t.linef("phases: instructions %s, input inspection %s, layout %s, output verification %s",
		phases.Instructions, phases.InputInspection, phases.Layout, phases.OutputVerification)
	t.linef("pages: %s source, %s generated, %s total", count(counts.SourcePages), count(counts.GeneratedPages), count(counts.TotalPages))
	t.linef("output published: %t %s", pub.Published, pub.Output)
	t.linef("report: %s %s", pub.ReportStatus, pub.ReportPath)

	if pub.RecoveryReport != "" {
		t.linef("recovery reference (%s): %s", pub.RecoveryState, pub.RecoveryReport)
	}
}

func (t *textWriter) diagnostic(diagnostic *DiagnosticView) {
	where := ""
	if diagnostic.Location != nil {
		where = " (" + diagnostic.Location.String() + ")"
	}

	cut := ""
	if diagnostic.MessageTruncated {
		cut = " [message cut]"
	}

	t.linef("- [%s %s/%s] %s%s%s", diagnostic.Severity, diagnostic.Stage, diagnostic.Code, diagnostic.Message, cut, where)

	if diagnostic.ConsequenceContext != "" {
		t.linef("  consequence: %s", diagnostic.ConsequenceContext)
	}

	if diagnostic.Path != "" {
		path, truncated := preview(diagnostic.Path)

		cue := ""
		if truncated {
			cue = " [path cut; inspect details for the full path]"
		}

		t.linef("  path: %q%s", path, cue)
	}

	if diagnostic.Cause != "" {
		t.linef("  cause: %s", diagnostic.Cause)
	}

	if diagnostic.Recovery != nil {
		t.linef("  recovery: %s", diagnostic.Recovery.Action)
	}
}

func (t *textWriter) part(view *PartView) {
	span := "pages unknown"
	if view.Range != nil {
		span = fmt.Sprintf("pages %d-%d", view.Range.Start, view.Range.End)
	}

	what := view.Path

	switch {
	case view.Generated != nil:
		what = fmt.Sprintf("%gx%g pt, background %s", view.Generated.Width, view.Generated.Height, view.Generated.Background)

		if view.Generated.Text != nil {
			what += fmt.Sprintf(", text %q", view.Generated.Text.Preview)
			if view.Generated.Text.HasFindings {
				what += ", measured overflow findings"
			}
		}
	default:
	}

	t.linef("%s %s %s: %s (%d warning records)", view.ID, view.Kind, span, what, view.WarningCount)
}
