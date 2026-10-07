// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
)

type (
	// limitWriter stops accepting bytes past its limit, so a report is never fully buffered just to be
	// found too large.
	limitWriter struct {
		writer    io.Writer
		remaining int64
		written   int64
		exceeded  bool
	}

	// nodeCounter counts the JSON values an encoded value will hold.
	nodeCounter struct{ nodes int64 }
)

// Node counts of the fixed parts of an encoded report: each is the object or array itself plus its members
// that are always present.
const (
	headerScalars       = 4 // report_version, kind, status, command
	phaseStates         = 4
	countValues         = 3
	publicationRequired = 2 // report_status, published
	tableCount          = 5 // diagnostics, parts, sources, fonts, styles
	diagnosticRequired  = 3 // stage, code, message
	sizeMembers         = 3 // origin, width, height
	producerNodes       = 6
	textSettings        = 10
	boundsMembers       = 4

	reportNodes     = 1 + headerScalars + (1 + phaseStates) + (1 + countValues) + (1 + publicationRequired) + tableCount
	diagnosticNodes = 1 + diagnosticRequired
	partNodes       = 1 + 1 + 1 + 1 + 1 + 1 + 1 // part, range, pages, id, kind, origin object, file
	rangeNodes      = 2                         // start, end
	sourceNodes     = 1 + 2                     // object, path, bytes
	fontNodes       = 1 + 2                     // object, digest, name
	styleNodes      = 1 + 1 + (1 + sizeMembers) // style, background, size object
	textNodes       = 1 + textSettings + 1 + 1 + 1
)

var errOverLimit = errors.New("byte limit reached")

func (w *limitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		w.exceeded = true

		return 0, errOverLimit
	}

	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	w.written += int64(n)

	if err != nil {
		return n, fmt.Errorf("write: %w", err)
	}

	return n, nil
}

// Write validates r and streams its compact JSON, newline terminated, to w. It stops with CodeTooLarge as soon
// as more than limit bytes would be written (MaxReportBytes is the format's limit), so the caller can stage
// next to the target before committing anything. It returns the bytes written.
func Write(w io.Writer, r *Report, limit int64) (int64, error) {
	err := r.Validate()
	if err != nil {
		return 0, err
	}

	if nodes := r.nodes(); nodes > MaxNodes {
		return 0, newError(StatusFailed, StageLimit, CodeLimitNodes, nil, nil,
			"the report holds %d JSON values; a saved report may hold %d. Record fewer distinct parts or styles", nodes, MaxNodes)
	}

	out := &limitWriter{writer: w, remaining: limit}

	// A path that is not valid UTF-8 is written with U+FFFD in its place rather than refusing the report.
	err = json.MarshalWrite(out, r, jsontext.AllowInvalidUTF8(true))
	if err == nil {
		_, err = out.Write([]byte("\n"))
	}

	switch {
	case out.exceeded:
		return out.written, newError(StatusFailed, StageLimit, CodeTooLarge, nil, nil,
			"the report exceeds the %d byte limit; check a smaller job or drop --details", limit)
	case err != nil:
		return out.written, newError(StatusFailed, StageWrite, CodeWriteFailed, nil, err, "write report: %v", err)
	default:
		return out.written, nil
	}
}

// Encode returns the compact JSON of any report value (a summary or a query response).
func Encode(value any) ([]byte, error) {
	data, err := json.Marshal(value, jsontext.AllowInvalidUTF8(true))
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}

	return data, nil
}

// nodes counts the JSON values of the encoded report: objects, arrays, and scalars, nulls included, member
// names not. It is the writer's side of the decoder's node limit; a test checks it against the decoder's count.
func (r *Report) nodes() int64 {
	count := &nodeCounter{}

	count.add(reportNodes)

	if r.Producer != nil {
		count.add(producerNodes)
	}

	count.publication(&r.Publication)

	for i := range r.Diagnostics {
		d := &r.Diagnostics[i]

		count.add(diagnosticNodes)
		count.when(d.Path != "")

		if len(d.Consumers) > 0 {
			count.add(1 + int64(len(d.Consumers)))
		}

		if d.Location != nil {
			count.add(1)

			position := d.Location.position()
			count.position(&position)
			count.when(d.Location.ArgvIndex != nil, d.Location.Pointer != "")
		}
	}

	for i := range r.Parts {
		part := &r.Parts[i]

		count.add(partNodes)

		if part.Range != nil {
			count.add(rangeNodes)
		}

		count.when(part.Source != nil, part.Style != nil)
		count.optionalPosition(&part.Origin)
	}

	for i := range r.Sources {
		count.add(sourceNodes)
		count.when(r.Sources[i].Digest != "")
	}

	for i := range r.Fonts {
		count.add(fontNodes)
		count.when(r.Fonts[i].File != "")
	}

	count.styles(r.Styles)

	return count.nodes
}

func (c *nodeCounter) add(n int64) { c.nodes += n }

// when adds one node per true condition.
func (c *nodeCounter) when(conditions ...bool) {
	for _, condition := range conditions {
		if condition {
			c.nodes++
		}
	}
}

// position counts a location's position: its object is counted by the caller, so file is the one required value.
func (c *nodeCounter) position(p *Position) {
	c.add(1)
	c.optionalPosition(p)
}

// optionalPosition counts the members of a position besides its file.
func (c *nodeCounter) optionalPosition(p *Position) {
	c.when(p.Offset != nil, p.Line != 0, p.Column != 0)
}

func (c *nodeCounter) publication(p *Publication) {
	c.when(p.Output != "", p.ReportPath != "", p.RecoveryReport != "", p.OutputDigest != "", p.RecoveryState != "")
}

func (c *nodeCounter) styles(styles []Style) {
	for i := range styles {
		c.add(styleNodes)

		if styles[i].Text != nil {
			c.add(textNodes)

			if len(styles[i].Text.Findings) > 0 {
				c.add(1 + 4*int64(len(styles[i].Text.Findings)))
			}

			if styles[i].Text.Bounds != nil {
				c.add(boundsMembers)
			}

			if styles[i].Text.InkBounds != nil {
				c.add(boundsMembers)
			}
		}
	}
}

// EncodePretty renders explicit complete detail as readable JSON, preserving every value.
func EncodePretty(value any) ([]byte, error) {
	data, err := json.Marshal(value, jsontext.AllowInvalidUTF8(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, fmt.Errorf("encode complete text detail: %w", err)
	}

	return data, nil
}
