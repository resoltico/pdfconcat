// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report

import (
	"strconv"
	"strings"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

const argvPrefix = "argv:"

// LocationOf converts a job location. A command-line operand becomes ArgvIndex instead of a pointer.
func LocationOf(loc assembly.Location) *Location {
	out := &Location{File: loc.Source, Line: loc.Line, Column: loc.Column}

	if loc.Line > 0 {
		offset := loc.Offset
		out.Offset = &offset
	}

	if index, found := strings.CutPrefix(loc.Pointer, argvPrefix); found {
		if n, err := strconv.Atoi(index); err == nil {
			out.ArgvIndex = &n

			return out
		}
	}

	out.Pointer = loc.Pointer

	return out
}

// position is the Position part of the location.
func (l Location) position() Position {
	return Position{Offset: l.Offset, File: l.File, Line: l.Line, Column: l.Column}
}

// OriginOf is the Position part of a job location.
func OriginOf(loc assembly.Location) Position {
	return LocationOf(loc).position()
}

// ArgvID is the part ID of command-line operand n.
func ArgvID(n int) string { return argvPrefix + strconv.Itoa(n) }
