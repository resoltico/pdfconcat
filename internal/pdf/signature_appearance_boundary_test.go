// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestAppearanceClosingTokensMustBeComplete(t *testing.T) {
	for _, tc := range []struct {
		name, tail string
		eof        bool
		want       diag.SignatureDiscardReason
	}{
		{"comment before endobj", "endstream% writer comment\r\nendobj\n", false, ""},
		{"truncated endstream", "\nendstrea", true, diag.SignatureAppearanceBoundary},
		{"truncated endobj", "\nendstream\nendob", true, diag.SignatureAppearanceBoundary},
		{"no token boundary after endobj", "\nendstream\nendobjJ", false, diag.SignatureAppearanceBoundary},
		{"malformed token in window", "\nendstream\nendost", false, diag.SignatureAppearanceBoundary},
		{"valid token prefix at bound", "\nendstream\nendob", false, diag.SignatureAppearanceClosingLimit},
		{"bounded probe does not establish endobj", "\nendstream\nendobj", false, diag.SignatureAppearanceClosingLimit},
		{"comment continues beyond bound", "endstream % continuing comment", false, diag.SignatureAppearanceClosingLimit},
		{"comment reaches EOF without endobj", "endstream % continuing comment", true, diag.SignatureAppearanceBoundary},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := appearanceClosingFailure([]byte(tc.tail), tc.eof); got != tc.want {
				t.Fatalf("closing reason = %q, want %q", got, tc.want)
			}
		})
	}
}

type appearanceBoundaryFaultReader struct {
	*bytes.Reader
	boundary          int64
	seek              func() error
	read              func([]byte) (int, error)
	positioned, fired bool
}

func (r *appearanceBoundaryFaultReader) Seek(offset int64, whence int) (int64, error) {
	r.positioned = whence == io.SeekStart && offset == r.boundary
	if r.positioned && r.seek != nil {
		r.fired = true
		if err := r.seek(); err != nil {
			return 0, err
		}
	}
	return r.Reader.Seek(offset, whence)
}

func (r *appearanceBoundaryFaultReader) Read(p []byte) (int, error) {
	if r.positioned && r.read != nil {
		r.fired = true
		r.positioned = false
		return r.read(p)
	}
	return r.Reader.Read(p)
}

func TestAppearanceBoundarySnapshotFailuresKeepCauseAndLocation(t *testing.T) {
	member := []byte{0x78, 0x9c, 0x03, 0x00, 0x00, 0x00, 0x00, 0x01}
	d := pdffixture.SignatureArtwork()
	d.Objs[5] = bytes.Replace(d.Objs[5], []byte("[75 85 175 145]"), []byte("[0 0 0 0]"), 1)
	d.Objs[7] = admissionStream("/Type /XObject /Subtype /Form /BBox [0 0 0 0] /Filter /FlateDecode", string(member))
	raw := d.Bytes()
	start := bytes.Index(raw, member)
	if bytes.Count(raw, member) != 1 {
		t.Fatal("fixture stream member must be unique")
	}
	boundary := int64(start + len(member))
	fault := errors.New("injected appearance snapshot I/O failure")
	for _, tc := range []struct {
		name     string
		fault    error
		code     diag.Code
		outcome  diag.Status
		setFault func(*appearanceBoundaryFaultReader, context.CancelFunc)
	}{
		{"seek", fault, diag.SourceUnreadable, diag.StatusFailed, func(r *appearanceBoundaryFaultReader, _ context.CancelFunc) {
			r.seek = func() error { return fault }
		}},
		{"read", fault, diag.SourceUnreadable, diag.StatusFailed, func(r *appearanceBoundaryFaultReader, _ context.CancelFunc) {
			r.read = func([]byte) (int, error) { return 0, fault }
		}},
		{"read with complete buffer", fault, diag.SourceUnreadable, diag.StatusFailed, func(r *appearanceBoundaryFaultReader, _ context.CancelFunc) {
			r.read = func(p []byte) (int, error) {
				n, _ := r.Reader.Read(p)
				return n, fault
			}
		}},
		{"premature end of snapshot", io.EOF, diag.SourceUnreadable, diag.StatusFailed, func(r *appearanceBoundaryFaultReader, _ context.CancelFunc) {
			r.read = func([]byte) (int, error) { return 0, io.EOF }
		}},
		{"cancellation at boundary", context.Canceled, diag.Interrupted, diag.StatusInterrupted, func(r *appearanceBoundaryFaultReader, cancel context.CancelFunc) {
			r.seek = func() error { cancel(); return nil }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			r := &appearanceBoundaryFaultReader{Reader: bytes.NewReader(raw), boundary: boundary}
			tc.setFault(r, cancel)
			_, _, _, err := readSourceReader(ctx, r, SourcePolicies{Signatures: packet.SignaturesDiscard})
			if !r.fired || !errors.Is(err, tc.fault) {
				t.Fatalf("fault fired=%t, expected %v in error chain, got %v", r.fired, tc.fault, err)
			}
			finding := diagnostic(err, diag.SourceInvalid)
			if finding.Code != tc.code || diag.Outcome([]diag.Diagnostic{finding}) != tc.outcome ||
				finding.PDFLocation == nil || finding.PDFLocation.Object != 6 || finding.PDFLocation.Page != 1 ||
				finding.PDFLocation.Path != "Object.AP" {
				t.Fatalf("classified failure: %+v", finding)
			}
			if tc.code == diag.SourceUnreadable && !strings.Contains(finding.Detail, tc.fault.Error()) {
				t.Fatalf("I/O cause omitted: %+v", finding)
			}
		})
	}
}
