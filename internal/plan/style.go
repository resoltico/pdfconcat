// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan

import (
	"errors"
	"strconv"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

// readParsed reads a string and parses it with parse; a parse failure is a bad_value diagnostic that carries
// the parser's expected-value message.
func readParsed[T comparable](scan *parser, what string, parse func(string) (T, error)) (assembly.Field[T], error) {
	text, value, err := scan.readString(what)
	if err != nil {
		return assembly.Field[T]{}, err
	}

	parsed, err := parse(text)
	if err != nil {
		return assembly.Field[T]{}, scan.failHere(StageShape, CodeBadValue, value, "%v", err)
	}

	return assembly.Set(parsed, scan.origin(value)), nil
}

// readInteger reads a JSON number whose mathematical value is an integer in [low, high].
func (p *parser) readInteger(low, high int64) (assembly.Field[int64], error) {
	value, err := p.next()
	if err != nil {
		return assembly.Field[int64]{}, err
	}

	err = p.want(value, '0', "an integer")
	if err != nil {
		return assembly.Field[int64]{}, err
	}

	raw := value.value.String()

	number, err := exactInt64(raw)

	switch {
	case errors.Is(err, errNotIntegral):
		return assembly.Field[int64]{}, p.failHere(StageShape, CodeNotInteger, value,
			"%s is not an integer; want a whole number (1, 1.0 and 1e0 are accepted)", raw)
	case err != nil:
		return assembly.Field[int64]{}, p.failHere(StageShape, CodeOutOfRange, value, "%s is out of range; want %d to %d", raw, low, high)
	case number < low || number > high:
		return assembly.Field[int64]{}, p.failHere(
			StageShape,
			CodeOutOfRange,
			value,
			"%d is out of range; want %d to %d",
			number,
			low,
			high,
		)
	default:
		return assembly.Set(number, p.origin(value)), nil
	}
}

// readLength reads a number of points or a string with a unit.
func (p *parser) readLength() (assembly.Field[assembly.Length], error) {
	value, err := p.next()
	if err != nil {
		return assembly.Field[assembly.Length]{}, err
	}

	switch value.kind() {
	case '"':
		length, parseErr := assembly.ParseLength(value.value.String())
		if parseErr != nil {
			return assembly.Field[assembly.Length]{}, p.failHere(StageShape, CodeBadValue, value, "%v", parseErr)
		}

		return assembly.Set(length, p.origin(value)), nil
	case '0':
		return p.numberLength(value)
	default:
		return assembly.Field[assembly.Length]{}, p.want(value, '"', `a number of points or a string such as "20mm"`)
	}
}

// numberLength converts a JSON number of points; overflow and non-finite values are out of range.
func (p *parser) numberLength(value token) (assembly.Field[assembly.Length], error) {
	raw := value.value.String()

	points, err := strconv.ParseFloat(raw, 64)
	if err == nil {
		var length assembly.Length

		length, err = assembly.NewLength(points)
		if err == nil {
			return assembly.Set(length, p.origin(value)), nil
		}
	}

	return assembly.Field[assembly.Length]{}, p.failHere(StageShape, CodeOutOfRange, value,
		"%s is outside the supported range of ±%.0f points", raw, float64(assembly.MaxLength))
}

func (p *parser) readLeading() (assembly.Field[float64], error) {
	value, err := p.next()
	if err != nil {
		return assembly.Field[float64]{}, err
	}

	err = p.want(value, '0', "a number")
	if err != nil {
		return assembly.Field[float64]{}, err
	}

	leading, err := strconv.ParseFloat(value.value.String(), 64)
	if err != nil || leading < assembly.MinLeading || leading > assembly.MaxLeading {
		return assembly.Field[float64]{}, p.failHere(StageShape, CodeOutOfRange, value, "%s is out of range; want %.0f to %.0f",
			value.value.String(), assembly.MinLeading, assembly.MaxLeading)
	}

	return assembly.Set(leading, p.origin(value)), nil
}

// blank reads a "blank" style object, which counts as one structural node.
func (p *parser) blank() (assembly.BlankStyle, error) {
	open, err := p.openObject("a blank object")
	if err != nil {
		return assembly.BlankStyle{}, err
	}

	err = p.countNode(open)
	if err != nil {
		return assembly.BlankStyle{}, err
	}

	var style assembly.BlankStyle

	err = p.members([]string{memberSize, "background", "text"}, func(name string) error {
		var memberErr error

		switch name {
		case memberSize:
			style.Size, memberErr = readParsed(p, "a page-size string", assembly.ParsePageSize)
		case "background":
			style.Background, memberErr = readParsed(p, `a color such as "#F4EFE6", or "none"`, assembly.ParseFill)
		default: // "text"
			style.Text, memberErr = p.text()
		}

		return memberErr
	})

	return style, err
}

// text reads a "text" style object, which counts as one structural node.
func (p *parser) text() (assembly.TextStyle, error) {
	open, err := p.openObject("a text object")
	if err != nil {
		return assembly.TextStyle{}, err
	}

	err = p.countNode(open)
	if err != nil {
		return assembly.TextStyle{}, err
	}

	var text assembly.TextStyle

	known := []string{"value", "font", memberSize, "color", "anchor", "x", "y", "width", "align", "leading", "overflow"}

	err = p.members(known, func(name string) error { return p.textMember(name, &text) })

	return text, err
}

func (p *parser) textMember(name string, text *assembly.TextStyle) error {
	var err error

	switch name {
	case "value":
		text.Value, err = readParsed(p, wantedString, checkedText)
	case "font":
		text.Font, err = p.readFont()
	case memberSize:
		text.Size, err = p.readLength()
	case "color":
		text.Color, err = readParsed(p, `a color such as "#F4EFE6"`, assembly.ParseColor)
	case "anchor":
		text.Anchor, err = readParsed(p, "an anchor name", assembly.ParseAnchor)
	case "x":
		text.X, err = p.readLength()
	case "y":
		text.Y, err = p.readLength()
	case "width":
		text.Width, err = p.readLength()
	case "align":
		text.Align, err = readParsed(p, "an alignment name", assembly.ParseTextAlign)
	case "leading":
		text.Leading, err = p.readLeading()
	default: // "overflow": members only yields the known names
		text.Overflow, err = readParsed(p, "an overflow policy name", assembly.ParseOverflow)
	}

	return err
}

func checkedText(text string) (string, error) {
	return text, assembly.ValidateTextValue(text)
}

// readFont reads the built-in font name or a {"file": "..."} object, one atomic value. The base directory
// of a file font is fixed once the document is read (see resolveFontBases).
func (p *parser) readFont() (assembly.Field[assembly.Font], error) {
	value, err := p.next()
	if err != nil {
		return assembly.Field[assembly.Font]{}, err
	}

	const what = `"` + assembly.DefaultFontName + `" or an object {"file": "..."}`

	switch value.kind() {
	case '"':
		if value.value.String() != assembly.DefaultFontName {
			return assembly.Field[assembly.Font]{}, p.failHere(StageShape, CodeBadValue, value,
				"%q is not a font; want %s", value.value.String(), what)
		}

		return assembly.Set(assembly.Font{}, p.origin(value)), nil
	case '{':
		return p.fontObject(value)
	default:
		return assembly.Field[assembly.Font]{}, p.want(value, '{', what)
	}
}

func (p *parser) fontObject(open token) (assembly.Field[assembly.Font], error) {
	err := p.countNode(open)
	if err != nil {
		return assembly.Field[assembly.Font]{}, err
	}

	var font assembly.Font

	err = p.members([]string{"file"}, func(string) error {
		text, value, readErr := p.readString("a font file path string")
		if readErr != nil {
			return readErr
		}

		var fontErr error

		font, fontErr = assembly.FontFile(text, "")
		if fontErr != nil {
			return p.failHere(StageShape, CodeBadValue, value, "%v", fontErr)
		}

		return nil
	})
	if err != nil {
		return assembly.Field[assembly.Font]{}, err
	}

	if font.IsDefault() {
		return assembly.Field[assembly.Font]{}, p.fail(StageShape, CodeMissingMember, open.start, string(p.decoder.StackPointer()),
			`a font object needs "file"`)
	}

	return assembly.Set(font, p.origin(open)), nil
}
