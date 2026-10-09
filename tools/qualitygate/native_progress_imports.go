// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"log"
	"path"
	"strconv"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

// C is a compiler pseudo-import removed during cgo transformation, so native depguard cannot
// enforce its physical source boundary. Check every owned source before transformed analysis.
func architectureSourceIssues(root string, owners map[string]string, files []string, compiled map[string]bool) ([]string, error) {
	problems := repopolicy.SourceClassificationIssues(owners, files, compiled)
	for _, file := range files {
		content, err := readInRoot(root, file)
		if err != nil {
			return nil, err
		}

		issue, err := nativeCSourceIssue(file, content, owners)
		if err != nil {
			return nil, err
		}

		if issue != "" {
			problems = append(problems, issue)
		}
	}

	return problems, nil
}

func nativeCSourceIssue(file string, content []byte, owners map[string]string) (string, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), file, content, parser.ImportsOnly)
	if err != nil {
		return "", fmt.Errorf("parse physical C import boundary: %w", err)
	}

	for _, imported := range parsed.Imports {
		name, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil {
			return "", fmt.Errorf("parse source import: %w", unquoteErr)
		}

		if name == "C" && (path.Dir(file) != nativeProgressFixture || owners[path.Dir(file)] != nativeProgressOwner) {
			return file + ": C import belongs only to the exact native progress test-support role", nil
		}
	}

	return "", nil
}

func nativeCImportControl(root string, control architectureControl) (bool, error) {
	config, err := readInRoot(root, lintConfigFileName)
	if err != nil {
		return false, err
	}

	module, err := modulePath(root)
	if err != nil {
		return false, err
	}

	owners, err := repopolicy.SourceOwners(config, module)
	if err != nil {
		return false, err
	}

	content, err := readInRoot(root, control.file)
	if err != nil {
		return false, err
	}

	issue, err := nativeCSourceIssue(control.file, content, owners)
	if err != nil {
		return false, err
	}

	if issue != "" {
		log.Printf(
			"architecture control: compiled %s (%s); rejected physical C import outside %s",
			control.file,
			control.target,
			nativeProgressOwner,
		)
	}

	return issue != "", nil
}
