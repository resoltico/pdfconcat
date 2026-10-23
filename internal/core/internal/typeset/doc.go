// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package typeset loads TrueType fonts, shapes and wraps text, and places one text block on a page.
// It is pure computation: it knows nothing about PDF.
//
// # Fonts
//
// A [Font] is an immutable, validated static TrueType font (glyf outlines). Variable fonts, CFF
// (OpenType/CFF), collections, fonts whose OS/2 fsType forbids embedding, and malformed files are
// rejected with an error; loading never panics. A font's identity is the SHA-256 of its bytes.
// A Font is safe to share between goroutines.
//
// # Text
//
// Text is left-to-right Latin, Greek and Cyrillic (script classes Common and Inherited included) with
// combining marks positioned by the font's GPOS. Text is never normalized: decomposed and precomposed
// forms stay as given. Every character must have a glyph in the font; a missing glyph is an error,
// never a substitute box. Right-to-left and complex scripts, control characters (including TAB), and
// format characters (joiners, bidi controls, soft hyphen) are rejected early with a [TextError].
//
// # Whitespace
//
//   - CRLF and lone CR are line breaks, equivalent to LF.
//   - Every LF ends a paragraph; one final LF adds no empty paragraph. Empty text has no lines.
//   - U+0020 is the only breaking and justification space. Runs of spaces are preserved verbatim:
//     leading and interior spaces occupy width. Spaces at a wrap point are consumed by the break.
//     Trailing spaces of a line never count towards its width and are never justified.
//   - U+00A0 and every other space character are ordinary glyphs: never a break opportunity,
//     never collapsed, never justified.
//   - U+2028 and U+2029 are rejected.
//
// # Placement
//
// A text block is as wide as the wrap width (or the widest line when there is no wrap width, or
// the widest overflowing line) and as high as first-line ascent plus last-line descent plus the line
// advances. The block is anchored to one of nine points of the page and moved by offsets
// (+x right, +y up, points); the alignment acts inside the block. The block bounds use font
// advances, ascent and descent, not glyph ink.
//
// # Concurrency
//
// A [Shaper] holds mutable shaping state and must not be shared between goroutines; use one per
// goroutine. The returned [Placed] values are plain data.
package typeset
