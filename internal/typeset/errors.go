// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package typeset

import (
	"errors"
	"fmt"
)

// OutlineError reports a font whose outline for a glyph that the text needs cannot be read, which
// would render that character blank. The parsing library discards the whole outline table when any
// glyph of it is damaged, so every glyph of such a font reports this error, not only the damaged one.
// It matches [ErrMalformedFont] under [errors.Is].
type OutlineError struct {
	// Font is the PostScript name of the font.
	Font string
	// Rune is the first character of the cluster that needs the glyph.
	Rune rune
	// Glyph is the glyph identifier in the font.
	Glyph uint16
}

var (
	// ErrMalformedFont is wrapped by every error that reports a font file that is damaged or inconsistent.
	ErrMalformedFont = errors.New("malformed font")
	// ErrUnsupportedFont is wrapped by every error that reports a well-formed font of a kind this package does not embed.
	ErrUnsupportedFont = errors.New("unsupported font")
	// ErrShapingFailed is wrapped by every error that reports a failure of the shaper itself rather than of the text.
	ErrShapingFailed = errors.New("shaping failed")
)

// Error implements error.
func (e *OutlineError) Error() string {
	return fmt.Sprintf("font %q has an unreadable outline for glyph %d, needed for U+%04X; the font file is corrupt",
		e.Font, e.Glyph, e.Rune)
}

// Unwrap lets [errors.Is] match [ErrMalformedFont].
func (*OutlineError) Unwrap() error { return ErrMalformedFont }
