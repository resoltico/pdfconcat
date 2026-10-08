// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset

import (
	_ "embed" // The default font file is compiled into the executable.
)

// DefaultFontName is the plan-level name of the embedded default font.
const DefaultFontName = "Noto Sans"

// The default font is Noto Sans Regular v2.015 (unhinted TrueType, SIL Open Font License 1.1),
// 431,364 bytes, SHA-256 f3961a9cde016d41a4879aecda1474d3a36d6bf54fa0e4643de029cc2248b0e8,
// downloaded from the notofonts.github.io repository on GitHub (organization notofonts) at commit
// 28b15b4b43b7bed62b5cf6e6b0b5ff5846270535, path fonts/NotoSans/unhinted/ttf/NotoSans-Regular.ttf.
// Its license text is fontdata/OFL.txt; the notice is in THIRD_PARTY_NOTICES.md.
//
//go:embed fontdata/NotoSans-Regular.ttf
var defaultFontBytes []byte

// LoadDefaultFont parses the embedded default font. Each call returns a new Font with the same
// identity; callers load it once per job.
func LoadDefaultFont() (*Font, error) {
	return LoadFont(defaultFontBytes)
}
