// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly

import "fmt"

// Font selects the font of generated text: the built-in default, or a TrueType file. A font choice is
// one atomic value; it never layers member by member.
type Font struct {
	// File is the declared path of a font file; empty selects the built-in font.
	File string
	// Base is the directory File resolves against: the base of the scope that declared the font. It is
	// fixed where the font is declared, before any layering, so a font keeps its own base wherever it is used.
	Base string
}

// DefaultFontName is the plan spelling of the built-in font.
const DefaultFontName = "default"

// FontFile returns a file-backed font declared in a scope whose base directory is base.
func FontFile(file, base string) (Font, error) {
	if file == "" {
		return Font{}, fmt.Errorf("%w: font file path is empty", ErrInvalidPath)
	}

	err := CheckPathText(file)
	if err != nil {
		return Font{}, fmt.Errorf("font file: %w", err)
	}

	return Font{File: file, Base: base}, nil
}

// IsDefault reports whether the font is the built-in one.
func (f Font) IsDefault() bool {
	return f.File == ""
}

// String describes the font: its plan name, or its file path.
func (f Font) String() string {
	if f.IsDefault() {
		return DefaultFontName
	}

	return f.File
}
