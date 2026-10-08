// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func TestMutationColumnsAreRequiredPositiveAndToolSpecific(t *testing.T) {
	t.Parallel()

	for _, replacement := range []string{"", "column: 0", "column: -1", "column: 1.5"} {
		content := registryHeader + mustReplace(t, mutationEntry, "column: 10", replacement)
		if _, err := repopolicy.ParseRegistry([]byte(content)); err == nil {
			t.Fatalf("invalid mutation byte-column accepted: %q", replacement)
		}
	}

	for _, entry := range []string{disableEntry, coverageEntry} {
		for _, value := range []string{"0", "1"} {
			content := registryHeader + entry + "    column: " + value + "\n"

			_, err := repopolicy.ParseRegistry([]byte(content))
			if !errors.Is(err, repopolicy.ErrRegistry) || !strings.Contains(err.Error(), "column does not apply") {
				t.Fatalf("column used by another tool: %v", err)
			}
		}
	}
}

func TestMutationTargetUsesRawUniquePhysicalOperatorPosition(t *testing.T) {
	t.Parallel()

	const (
		anchor = "if α < b && b < c {"
		source = "package pkg\n//line /tmp/display.go:900\nfunc Check(α,b,c int) {\n\t" + anchor + "\n}\n}\n"
	)

	column := 8
	entry := &repopolicy.Entry{
		ID: "mutation-position-control", Tool: repopolicy.ToolMutation, Path: mutationFile,
		Operator: conditionalBoundaryOperator, Anchor: anchor, Column: &column,
	}

	controls := []struct {
		name, source string
		column       int
		valid        bool
	}{
		{"UTF8 tab and display directive", source, column, true},
		{"rune-count column", source, column - 1, false},
		{"changed indentation", strings.Replace(source, "\tif", "    if", 1), column, false},
		{"repeated anchor", source + "func Again(α,b,c int) {\n\t" + anchor + "\n}\n}\n", column, false},
		{"comment text", "package pkg\n/*\n" + anchor + "\n*/\n", column, false},
		{"string text", "package pkg\nvar text = `\n" + anchor + "\n`\n", column, false},
	}
	for _, control := range controls {
		t.Run(control.name, func(t *testing.T) {
			t.Parallel()

			target := *entry
			target.Column = &control.column

			problems := repopolicy.RepositoryIssues(
				[]*repopolicy.Entry{&target},
				sourceFrom(map[string]string{mutationFile: control.source}),
			)
			if (len(problems) == 0) != control.valid {
				t.Fatalf("operator-column source check: %v", problems)
			}
		})
	}
}
