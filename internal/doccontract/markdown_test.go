// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package doccontract_test checks that the product contract documents agree with the code they describe.
package doccontract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// moduleRoot returns the repository root: the nearest ancestor holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for {
		_, statErr := os.Stat(filepath.Join(dir, "go.mod"))
		if statErr == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}

		dir = parent
	}
}

// readText reads a repository file given as slash-separated path elements.
func readText(t *testing.T, parts ...string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(append([]string{moduleRoot(t)}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}

	return string(content)
}

// documentedFiles are the documents whose fenced examples are checked.
func documentedFiles() []string {
	return []string{"README.md", "docs/CLI.md", "docs/PLAN.md", "docs/BLANK_PAGES.md"}
}

// readDocument reads one of documentedFiles.
func readDocument(t *testing.T, file string) string {
	t.Helper()

	return readText(t, filepath.FromSlash(file))
}

// fencedBlocks returns the bodies of the fenced code blocks tagged language, in order.
func fencedBlocks(text, language string) []string {
	var blocks []string

	body := []string{}
	inside := false

	for line := range strings.SplitSeq(text, "\n") {
		switch {
		case !inside && line == "```"+language:
			inside, body = true, body[:0]
		case inside && line == "```":
			inside = false

			blocks = append(blocks, strings.Join(body, "\n")+"\n")
		case inside:
			body = append(body, line)
		default:
		}
	}

	return blocks
}

// tableRows returns the cells of each row of the first table after heading, header and rule rows excluded.
// A pipe escaped as \| stays inside its cell.
func tableRows(t *testing.T, text, heading string) [][]string {
	t.Helper()

	_, after, found := strings.Cut(text, "\n"+heading+"\n")
	if !found {
		t.Fatalf("no %q heading", heading)
	}

	var rows [][]string

	for line := range strings.SplitSeq(after, "\n") {
		if !strings.HasPrefix(line, "|") {
			if len(rows) > 0 {
				break
			}

			continue
		}

		rows = append(rows, tableCells(line))
	}

	if len(rows) < 3 {
		t.Fatalf("the table under %q has no rows", heading)
	}

	return rows[2:]
}

// tableCells splits one table line into trimmed cells.
func tableCells(line string) []string {
	const escapedPipe = "\x00"

	cells := strings.Split(strings.ReplaceAll(strings.Trim(line, "| "), `\|`, escapedPipe), "|")
	for i, cell := range cells {
		cells[i] = strings.TrimSpace(strings.ReplaceAll(cell, escapedPipe, "|"))
	}

	return cells
}
