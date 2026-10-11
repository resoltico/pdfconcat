// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
package repopolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

var ErrLintConfig = errors.New("golangci-lint data")

const (
	maxSourceLines   = 1000
	maxExportedTypes = 20
	pairWidth        = 2
)

type (
	// LinterInfo is what the golangci-lint binary reports about one linter.
	LinterInfo struct {
		Name       string `json:"name"`
		Deprecated bool   `json:"deprecated"`
	}

	// Issue is one diagnostic from golangci-lint's JSON report.
	Issue struct {
		Linter string
		File   string
		Text   string
		Source string
		Line   int
	}

	// linterListing is the output of `golangci-lint linters --json`.
	linterListing struct {
		Enabled  []LinterInfo `json:"enabled"`
		Disabled []LinterInfo `json:"disabled"`
	}

	// issuePosition is the position of a diagnostic in golangci-lint's JSON report.
	issuePosition struct {
		Filename string `json:"filename"`
		Line     int    `json:"line"`
	}

	// issueRecord is one issue of golangci-lint's JSON report. The tool writes FromLinter, which
	// encoding/json matches case-insensitively with the lower-case tag below.
	issueRecord struct {
		LineRange   *issueRange   `json:"linerange"`
		FromLinter  string        `json:"fromlinter"`
		Text        string        `json:"text"`
		Pos         issuePosition `json:"pos"`
		SourceLines []string      `json:"sourcelines"`
	}

	// issueRange describes displayed source lines, which can extend beyond the diagnostic position.
	issueRange struct {
		From int `json:"from"`
		To   int `json:"to"`
	}

	// issueReport is golangci-lint's JSON report.
	issueReport struct {
		Issues []issueRecord `json:"issues"`
	}
)

func activeItems(value any) []string {
	switch typed := value.(type) {
	case bool:
		if typed {
			return []string{"true"}
		}

		return nil
	case string:
		if typed != "" {
			return []string{typed}
		}

		return nil
	case []any:
		return stringList(typed)
	default:
		return nil
	}
}

func lookup(document map[string]any, dotted string) any {
	var current any = document

	for key := range strings.SplitSeq(dotted, ".") {
		table, isMap := current.(map[string]any)
		if !isMap {
			return nil
		}

		current = table[key]
	}

	return current
}

// asMap returns value as a mapping, or nil when it is anything else.

func asMap(value any) map[string]any {
	typed, matches := value.(map[string]any)
	if !matches {
		return nil
	}

	return typed
}

// asList returns value as a list, or nil when it is anything else.

func asList(value any) []any {
	typed, matches := value.([]any)
	if !matches {
		return nil
	}

	return typed
}

// asString returns value as a string, or "" when it is anything else.

func asString(value any) string {
	typed, matches := value.(string)
	if !matches {
		return ""
	}

	return typed
}

func stringList(value any) []string {
	list := asList(value)
	items := make([]string, 0, len(list))

	for _, item := range list {
		switch typed := item.(type) {
		case string:
			items = append(items, typed)
		case int, bool, float64:
			items = append(items, fmt.Sprint(typed))
		default:
		}
	}

	return items
}

// ParseLinterInfo reads the output of `golangci-lint linters --json`.

func ParseLinterInfo(data []byte) (map[string]LinterInfo, error) {
	var listing linterListing

	err := json.Unmarshal(data, &listing)
	if err != nil {
		return nil, fmt.Errorf("%w: decode linter list: %w", ErrLintConfig, err)
	}

	known := map[string]LinterInfo{}

	for _, info := range slices.Concat(listing.Enabled, listing.Disabled) {
		known[info.Name] = info
	}

	if len(known) == 0 {
		return nil, fmt.Errorf("%w: the linter list is empty", ErrLintConfig)
	}

	return known, nil
}

func ParseIssues(data []byte, root string) ([]Issue, error) {
	var parsed issueReport

	err := json.Unmarshal(data, &parsed)
	if err != nil {
		return nil, fmt.Errorf("%w: decode report: %w", ErrLintConfig, err)
	}

	issues := make([]Issue, 0, len(parsed.Issues))

	for _, record := range parsed.Issues {
		source, sourceErr := diagnosticSourceLine(&record)
		if sourceErr != nil {
			return nil, sourceErr
		}

		file := strings.ReplaceAll(record.Pos.Filename, `\`, "/")
		file = strings.TrimPrefix(file, strings.ReplaceAll(root, `\`, "/")+"/")

		issues = append(
			issues,
			Issue{
				Linter: record.FromLinter,
				File:   path.Clean(file),
				Text:   record.Text,
				Source: source,
				Line:   record.Pos.Line,
			},
		)
	}

	return issues, nil
}

// diagnosticSourceLine matches golangci-lint's source exclusion: the physical position's line,
// rather than every line displayed for a multi-line diagnostic.

func diagnosticSourceLine(record *issueRecord) (string, error) {
	if len(record.SourceLines) == 0 {
		return "", nil
	}

	if record.Pos.Line <= 0 {
		return "", fmt.Errorf("%w: diagnostic source has no physical position", ErrLintConfig)
	}

	index := 0
	if record.LineRange != nil && record.LineRange.From != 0 {
		index = record.Pos.Line - record.LineRange.From
		if record.LineRange.From < 0 || index < 0 || record.Pos.Line > record.LineRange.To ||
			len(record.SourceLines)-1 != record.LineRange.To-record.LineRange.From {
			return "", fmt.Errorf("%w: diagnostic source range does not contain its physical position", ErrLintConfig)
		}
	} else if len(record.SourceLines) != 1 || record.LineRange != nil && record.LineRange.To != 0 {
		return "", fmt.Errorf("%w: diagnostic source without a range must contain only its physical line", ErrLintConfig)
	}

	return record.SourceLines[index], nil
}

func decodeLintConfig(config []byte) (map[string]any, error) {
	var document map[string]any

	decoder := yaml.NewDecoder(bytes.NewReader(config))

	err := decoder.Decode(&document)
	if err != nil {
		return nil, fmt.Errorf("%w: decode configuration: %w", ErrLintConfig, err)
	}

	var trailing any
	if endErr := decoder.Decode(&trailing); endErr == nil {
		return nil, fmt.Errorf(yamlSingleDocumentError, ErrLintConfig)
	} else if !errors.Is(endErr, io.EOF) {
		return nil, fmt.Errorf(yamlEOFFailedError, ErrLintConfig, endErr)
	}

	return document, nil
}
