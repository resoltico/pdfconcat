// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package typeset

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-text/typesetting/language"
	"golang.org/x/text/unicode/bidi"
)

type (
	// Problem is one reason text cannot be rendered with a font.
	Problem struct {
		Reason string
		// Offset is the rune offset into the text, or -1 for invalid UTF-8.
		Offset int
		// More counts further consecutive code points rejected for the same reason.
		More int
		Rune rune
	}

	// TextError lists the rejected positions of a text, capped at a few.
	TextError struct {
		Problems []Problem
	}
)

const (
	// MaxTextRunes bounds the text of one block, so wrapping and shaping memory stays proportional to it.
	MaxTextRunes = 100_000

	maxReportedProblems = 8

	// supportedText states what text is accepted; it ends every TextError.
	supportedText = "supported: left-to-right Latin, Greek and Cyrillic text with combining marks, " +
		"U+0020 and U+00A0 spaces, and line breaks (LF, CRLF, CR); every character needs a glyph in the font"
)

// Error implements error.
func (e *TextError) Error() string {
	var message strings.Builder

	message.WriteString("text cannot be rendered: ")

	for index, problem := range e.Problems {
		if index > 0 {
			message.WriteString("; ")
		}

		if problem.Offset < 0 {
			message.WriteString(problem.Reason)

			continue
		}

		fmt.Fprintf(&message, "U+%04X at rune %d", problem.Rune, problem.Offset)

		if problem.More > 0 {
			fmt.Fprintf(&message, " (+%d more)", problem.More)
		}

		fmt.Fprintf(&message, ": %s", problem.Reason)
	}

	message.WriteString(" (" + supportedText + ")")

	return message.String()
}

func allowedScript(s language.Script) bool {
	return s == language.Common || s == language.Inherited || s == language.Latin || s == language.Greek || s == language.Cyrillic
}

// normalizeNewlines maps CRLF and lone CR to LF.
func normalizeNewlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// rejection returns why r is not supported, or "" when it is.
func rejection(r rune) string {
	properties, _ := bidi.LookupRune(r)
	switch {
	case r == '\n':
		return ""
	case r == '\t':
		return "TAB is not supported; use spaces"
	case unicode.IsControl(r):
		return "control character"
	case r == 0x2028 || r == 0x2029:
		return "line and paragraph separators are not supported; use LF"
	case unicode.Is(unicode.Cf, r):
		return "format character (joiner, bidi control, soft hyphen) is not supported"
	case !allowedScript(language.LookupScript(r)):
		return fmt.Sprintf("script %s (ISO 15924 code) is not supported", language.LookupScript(r))
	case properties.Class() == bidi.R || properties.Class() == bidi.AL:
		return "right-to-left character is not supported"
	default:
		return ""
	}
}

// checkSupported applies the early-rejection policy to newline-normalized text.
func checkSupported(text string) []Problem {
	if !utf8.ValidString(text) {
		return []Problem{{Offset: -1, Rune: utf8.RuneError, Reason: "text is not valid UTF-8"}}
	}

	var problems []Problem

	index := 0

	for _, r := range text {
		reason := rejection(r)
		if reason != "" {
			last := len(problems) - 1

			switch {
			case last >= 0 && problems[last].Reason == reason && problems[last].Offset+problems[last].More+1 == index:
				problems[last].More++
			case len(problems) < maxReportedProblems:
				problems = append(problems, Problem{Offset: index, Rune: r, Reason: reason})
			default:
			}
		}

		index++
	}

	return problems
}

// ValidateText checks geometry-independent script and glyph availability before source inspection.
// A missing nominal glyph is checked through shaping, since composition may supply it.
func (s *Shaper) ValidateText(font *Font, text string) error {
	if font == nil {
		return &InvalidParamError{Field: "font", Message: "must not be nil"}
	}

	text = normalizeNewlines(text)
	if problems := checkSupported(text); len(problems) > 0 {
		return &TextError{Problems: problems}
	}

	state := s.fontState(font)
	offset := 0

	for paragraph := range strings.SplitSeq(text, "\n") {
		missing, nominalErr := state.checkNominalParagraph(font, paragraph)
		if nominalErr != nil {
			return nominalErr
		}

		if missing {
			if _, err := s.shapeParagraph(font, paragraph, offset, ""); err != nil {
				return err
			}
		}

		offset += utf8.RuneCountInString(paragraph) + 1
	}

	return nil
}

func (state *shaperFont) checkNominalParagraph(font *Font, paragraph string) (bool, error) {
	for _, r := range paragraph {
		glyph, found := state.face.NominalGlyph(r)
		if !found || glyph == 0 || glyph > maxGlyphID {
			return true, nil
		}

		if _, checked := state.checked[uint16(glyph)]; checked {
			continue
		}

		if _, hasOutline := state.face.GlyphDataOutline(glyph); !hasOutline {
			return false, &OutlineError{Font: font.name, Rune: r, Glyph: uint16(glyph)}
		}

		state.checked[uint16(glyph)] = struct{}{}
	}

	return false, nil
}
