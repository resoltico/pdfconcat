// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// Version is the only plan-file format version currently defined.
const Version = 1

// documentFile is the JSON shape of a plan file.
type documentFile struct {
	Schema  string            `json:"$schema"`
	Version *int              `json:"version"`
	Output  string            `json:"output"`
	Dir     string            `json:"dir"`
	Blank   *blankStyleFile   `json:"blank"`
	Items   []json.RawMessage `json:"items"`
}

// itemFile is the JSON shape of an object item: a blank or a directory group.
type itemFile struct {
	Blank *blankStyleFile   `json:"blank"`
	Count *int              `json:"count"`
	Dir   *string           `json:"dir"`
	Items []json.RawMessage `json:"items"`
}

// blankStyleFile is the JSON shape of a blank page style.
type blankStyleFile struct {
	Size       *string        `json:"size"`
	Background *string        `json:"background"`
	Text       *textStyleFile `json:"text"`
}

// textStyleFile is the JSON shape of the text printed on a blank.
type textStyleFile struct {
	Value   *string      `json:"value"`
	Font    *string      `json:"font"`
	Size    *lengthField `json:"size"`
	Color   *string      `json:"color"`
	Anchor  *string      `json:"anchor"`
	X       *lengthField `json:"x"`
	Y       *lengthField `json:"y"`
	Width   *lengthField `json:"width"`
	Align   *string      `json:"align"`
	Leading *float64     `json:"leading"`
}

// lengthField is a JSON number (points) or a string with a unit such as "20mm".
type lengthField struct {
	text string
}

// UnmarshalJSON accepts a JSON number or string.
func (l *lengthField) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		err := json.Unmarshal(trimmed, &l.text)
		if err != nil {
			return fmt.Errorf("decode length string: %w", err)
		}

		return nil
	}

	var number float64

	err := json.Unmarshal(trimmed, &number)
	if err != nil {
		return errors.New("want a number of points or a string such as \"20mm\"")
	}

	l.text = strconv.FormatFloat(number, 'f', -1, 64)

	return nil
}

// decodeStrict unmarshals JSON into target, rejecting unknown fields and trailing data.
func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	err := decoder.Decode(target)
	if err != nil {
		return locate(data, decoder.InputOffset(), err)
	}

	_, err = decoder.Token()
	if err == nil {
		return errors.New("unexpected data after the top-level JSON value")
	}

	return nil
}

// locate adds a line and column to JSON syntax and type errors.
func locate(data []byte, fallbackOffset int64, err error) error {
	offset := fallbackOffset

	var (
		syntaxErr *json.SyntaxError
		typeErr   *json.UnmarshalTypeError
	)
	switch {
	case errors.As(err, &syntaxErr):
		offset = syntaxErr.Offset
	case errors.As(err, &typeErr):
		offset = typeErr.Offset
	default:
	}

	line, column := 1, 1

	for index := int64(0); index < offset && index < int64(len(data)); index++ {
		if data[index] == '\n' {
			line++
			column = 1

			continue
		}

		column++
	}

	return fmt.Errorf("line %d, column %d: %w", line, column, err)
}
