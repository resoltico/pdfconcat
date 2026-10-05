// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan

import (
	"fmt"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

// toStyle converts the JSON blank style; a nil receiver is the empty style.
func (f *blankStyleFile) toStyle(path string) (assembly.BlankStyle, error) {
	var style assembly.BlankStyle
	if f == nil {
		return style, nil
	}

	var err error

	style.Size, err = parseOptional(f.Size, path+".size", assembly.ParsePageSize)
	if err != nil {
		return assembly.BlankStyle{}, err
	}

	style.Background, err = parseOptional(f.Background, path+".background", assembly.ParseColor)
	if err != nil {
		return assembly.BlankStyle{}, err
	}

	style.Text, err = f.Text.toStyle(path + ".text")
	if err != nil {
		return assembly.BlankStyle{}, err
	}

	return style, nil
}

// toStyle converts the JSON text style; a nil receiver is the empty style.
func (f *textStyleFile) toStyle(path string) (assembly.TextStyle, error) {
	var style assembly.TextStyle
	if f == nil {
		return style, nil
	}

	if f.Value != nil {
		style.Value = assembly.Some(*f.Value)
	}

	if f.Leading != nil {
		style.Leading = assembly.Some(*f.Leading)
	}

	var err error

	style.Font, err = parseOptional(f.Font, path+".font", assembly.ParseFont)
	if err != nil {
		return assembly.TextStyle{}, err
	}

	style.Color, err = parseOptional(f.Color, path+".color", assembly.ParseColor)
	if err != nil {
		return assembly.TextStyle{}, err
	}

	style.Anchor, err = parseOptional(f.Anchor, path+".anchor", assembly.ParseAnchor)
	if err != nil {
		return assembly.TextStyle{}, err
	}

	style.Align, err = parseOptional(f.Align, path+".align", assembly.ParseTextAlign)
	if err != nil {
		return assembly.TextStyle{}, err
	}

	err = f.parseLengths(&style, path)
	if err != nil {
		return assembly.TextStyle{}, err
	}

	return style, nil
}

// parseLengths fills the length-valued fields of style.
func (f *textStyleFile) parseLengths(style *assembly.TextStyle, path string) error {
	lengths := []struct {
		field  *lengthField
		target *assembly.Option[assembly.Length]
		name   string
	}{
		{f.Size, &style.Size, "size"},
		{f.X, &style.X, "x"},
		{f.Y, &style.Y, "y"},
		{f.Width, &style.Width, "width"},
	}

	for index := range lengths {
		length := &lengths[index]
		if length.field == nil {
			continue
		}

		value, err := assembly.ParseSome(length.field.text, assembly.ParseLength)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", path, length.name, err)
		}

		*length.target = value
	}

	return nil
}

// parseOptional parses an optional JSON string into an Option, reporting errors at path.
func parseOptional[T any](text *string, path string, parse func(string) (T, error)) (assembly.Option[T], error) {
	if text == nil {
		return assembly.Option[T]{}, nil
	}

	option, err := assembly.ParseSome(*text, parse)
	if err != nil {
		return assembly.Option[T]{}, fmt.Errorf("%s: %w", path, err)
	}

	return option, nil
}
