// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"io"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// proveZeroOffsetDiscardHint keeps the failed preserve admission. Only the
// original typed candidate can authorize one source-level discard probe.
func proveZeroOffsetDiscardHint(c context.Context, reader io.ReadSeeker, policies SourcePolicies, refusal error) error {
	var candidate *pdfcpu.PossibleZeroOffsetStructureIndex
	if (policies.Structure != "" && policies.Structure != packet.StructurePreserve) || !errors.As(refusal, &candidate) {
		return refusal
	}
	if err := c.Err(); err != nil {
		return err
	}
	probe := &observedReadSeeker{ReadSeeker: reader}
	if _, err := probe.Seek(0, io.SeekStart); err != nil {
		return err
	}
	policies.Structure = packet.StructureDiscard
	s, origins, findings, err := readSourceReader(c, probe, policies)
	var location *diag.PDFLocation
	if err == nil {
		proved := false
		for _, reference := range s.Read.DiscardedZeroOffsetReferences {
			if reference.Root == candidate.Owner && reference.Slot == candidate.Slot &&
				reference.Entry.Object == candidate.Target.ObjectNumber.Value() &&
				reference.Entry.Generation == candidate.Target.GenerationNumber.Value() {
				proved = true
				break
			}
		}
		if proved {
			for _, finding := range findings {
				if finding.Code == diag.SourceXRefStructureReferenceDiscarded && finding.PDFLocation != nil &&
					finding.PDFLocation.Object == candidate.Owner.ObjectNumber.Value() && finding.PDFLocation.Path == candidate.Slot {
					location = finding.PDFLocation
					break
				}
			}
		}
		if location != nil {
			_, _, err = observeSource(c, s, origins)
		}
	}
	if canceled := c.Err(); canceled != nil {
		return canceled
	}
	// A source proof can already have classified the acquisition failure
	// and attached its location. Keep that richer diagnosis through replay.
	var readFailure *policyError
	if errors.As(err, &readFailure) && readFailure.finding.Code == diag.SourceUnreadable {
		return err
	}
	if probe.err != nil {
		return probe.err
	}
	if err != nil || location == nil {
		return refusal
	}
	return structureAt(pdfFailure(diag.SourceInvalid, diag.ProvedZeroOffsetDiscardHint(), refusal), *location)
}

// observedReadSeeker preserves native read and seek errors when a parser
// also reports syntax failures. EOF can be ordinary parser lookahead.
type observedReadSeeker struct {
	io.ReadSeeker
	err error
}

func (r *observedReadSeeker) Read(p []byte) (int, error) {
	n, err := r.ReadSeeker.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && r.err == nil {
		r.err = err
	}
	return n, err
}

func (r *observedReadSeeker) Seek(offset int64, whence int) (int64, error) {
	n, err := r.ReadSeeker.Seek(offset, whence)
	if err != nil && r.err == nil {
		r.err = err
	}
	return n, err
}
