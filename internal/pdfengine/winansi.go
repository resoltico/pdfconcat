// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine

import "fmt"

// Windows-1252 code points for bytes 0x80-0x9F; zero marks an undefined byte.
const (
	winAnsiFirstExtended = 0x80
	winAnsiLastExtended  = 0x9F
	latin1First          = 0xA0
	latin1Last           = 0xFF
	asciiFirstPrintable  = 0x20
	asciiDelete          = 0x7F
)

// winAnsiExtended maps bytes 0x80-0x9F to their Unicode code points.
func winAnsiExtended() [winAnsiLastExtended - winAnsiFirstExtended + 1]rune {
	return [...]rune{
		0x20AC, 0, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
		0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0, 0x017D, 0,
		0, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
		0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0, 0x017E, 0x0178,
	}
}

// winAnsiByte returns the WinAnsiEncoding byte for r, which every PDF viewer
// supports for the standard fonts without embedding.
func winAnsiByte(r rune) (byte, bool) {
	switch {
	case r >= asciiFirstPrintable && r < asciiDelete:
		return byte(r), true
	case r >= latin1First && r <= latin1Last:
		return byte(r), true
	}

	for offset, extended := range winAnsiExtended() {
		if extended != 0 && extended == r {
			return byte(winAnsiFirstExtended + offset), true
		}
	}

	return 0, false
}

// encodeWinAnsi converts one line of text to WinAnsiEncoding bytes.
func encodeWinAnsi(line string) ([]byte, error) {
	encoded := make([]byte, 0, len(line))
	for _, r := range line {
		b, ok := winAnsiByte(r)
		if !ok {
			return nil, fmt.Errorf(
				"character %q (U+%04X) cannot be drawn with the standard PDF fonts (Latin text in the Windows-1252 repertoire only)",
				r,
				r,
			)
		}

		encoded = append(encoded, b)
	}

	return encoded, nil
}
