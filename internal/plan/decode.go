// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package plan reads PDFConcat plan files: strict JSON documents that describe
// the output, run-wide blank-page defaults, and the ordered sequence of PDFs
// and generated blanks.
package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"unicode/utf8"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

// maxPlanBytes bounds how much of a plan file is read; a plan of several
// hundred thousand entries stays far below it.
const maxPlanBytes = 64 << 20

// Document is a decoded plan file with every PDF path made absolute.
type Document struct {
	// Output is the destination named by the plan, absolute, or empty if the plan names none.
	Output string
	// Blank holds the plan's run-wide blank-page defaults.
	Blank assembly.BlankStyle
	// Sequence is the flattened ordered sequence.
	Sequence assembly.Sequence
}

// Decode reads a plan from reader. Relative paths in the plan resolve against baseDir,
// which must be absolute.
func Decode(reader io.Reader, baseDir string) (Document, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxPlanBytes+1))
	if err != nil {
		return Document{}, fmt.Errorf("read plan: %w", err)
	}

	if len(data) > maxPlanBytes {
		return Document{}, fmt.Errorf("plan exceeds %d bytes", maxPlanBytes)
	}

	if !utf8.Valid(data) {
		return Document{}, errors.New("plan is not valid UTF-8")
	}

	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))

	var file documentFile

	err = decodeStrict(data, &file)
	if err != nil {
		return Document{}, err
	}

	err = checkVersion(file.Version)
	if err != nil {
		return Document{}, err
	}

	blank, err := file.Blank.toStyle("blank")
	if err != nil {
		return Document{}, err
	}

	itemsDir := resolveDir(baseDir, file.Dir)

	items, err := decodeItems(file.Items, itemsDir, "items")
	if err != nil {
		return Document{}, err
	}

	document := Document{Blank: blank, Sequence: assembly.Sequence{Items: items}}
	if file.Output != "" {
		document.Output = resolveDir(baseDir, file.Output)
	}

	return document, nil
}

func checkVersion(version *int) error {
	if version == nil {
		return fmt.Errorf("version: required; this build reads plan version %d", Version)
	}

	if *version != Version {
		return fmt.Errorf("version: %d is not supported; this build reads plan version %d", *version, Version)
	}

	return nil
}

// resolveDir joins name onto base unless name is absolute; an empty name is base.
func resolveDir(base, name string) string {
	if name == "" {
		return base
	}

	name = filepath.FromSlash(name)
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}

	return filepath.Join(base, name)
}

// decodeItems flattens raw items, descending into directory groups.
func decodeItems(raws []json.RawMessage, dir, path string) ([]assembly.Item, error) {
	items := make([]assembly.Item, 0, len(raws))
	for index, raw := range raws {
		itemPath := fmt.Sprintf("%s[%d]", path, index)

		decoded, err := decodeItem(raw, dir, itemPath)
		if err != nil {
			return nil, err
		}

		items = append(items, decoded...)
	}

	return items, nil
}

func decodeItem(raw json.RawMessage, dir, path string) ([]assembly.Item, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%s: empty item", path)
	}

	switch trimmed[0] {
	case '"':
		var name string

		err := json.Unmarshal(trimmed, &name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}

		if name == "" {
			return nil, fmt.Errorf("%s: empty PDF path", path)
		}

		return []assembly.Item{assembly.PDFItem(resolveDir(dir, name))}, nil
	case '{':
		return decodeObjectItem(trimmed, dir, path)
	default:
		return nil, fmt.Errorf("%s: want a PDF path string or an object with \"blank\" or \"dir\"", path)
	}
}

func decodeObjectItem(raw []byte, dir, path string) ([]assembly.Item, error) {
	var file itemFile

	err := decodeStrict(raw, &file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	switch {
	case file.Blank != nil && file.Dir == nil && file.Items == nil:
		return decodeBlankItem(&file, path)
	case file.Dir != nil && file.Blank == nil && file.Count == nil:
		return decodeGroupItem(&file, dir, path)
	default:
		return nil, errors.New(path + `: an object item is either {"blank": {...}, "count": N} or {"dir": "...", "items": [...]}`)
	}
}

func decodeBlankItem(file *itemFile, path string) ([]assembly.Item, error) {
	style, err := file.Blank.toStyle(path + ".blank")
	if err != nil {
		return nil, err
	}

	count := 1
	if file.Count != nil {
		count = *file.Count
	}

	if count < 1 || count > assembly.MaxBlankCount {
		return nil, fmt.Errorf("%s.count: %d is invalid; want 1 to %d", path, count, assembly.MaxBlankCount)
	}

	return []assembly.Item{assembly.BlankItem(style, count)}, nil
}

func decodeGroupItem(file *itemFile, dir, path string) ([]assembly.Item, error) {
	if file.Items == nil {
		return nil, fmt.Errorf("%s: a directory group needs \"items\"", path)
	}

	return decodeItems(file.Items, resolveDir(dir, *file.Dir), path+".items")
}
