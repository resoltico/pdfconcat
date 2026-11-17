// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package model

// ZeroOffsetPolicy controls the sole additional classic-xref input tolerance.
type ZeroOffsetPolicy uint8

const (
	RejectZeroOffsets ZeroOffsetPolicy = iota
	AdmitUnreferencedZeroOffsets
)

// ZeroOffsetEntry identifies an effective tombstone, preserving revision precedence.
type ZeroOffsetEntry struct {
	Object, Generation, Revision int
	XRefOffset                   int64
}
