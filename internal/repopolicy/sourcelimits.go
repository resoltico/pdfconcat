// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
)

var errNoOwnedSources = errors.New("no owned Go sources discovered")

// ScanSourceLimits checks physical file size and exported declarations in every owned Go source.
// Compiler selection and source display directives cannot hide tests, fixtures or platform variants.
func ScanSourceLimits(root string) ([]string, error) {
	files, err := OwnedGoSources(root)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, errNoOwnedSources
	}

	var problems []string

	err = withRoot(root, func(tree *os.Root) error {
		for _, file := range files {
			source, readErr := tree.ReadFile(file)
			if readErr != nil {
				return fmt.Errorf("read source for size check %s: %w", file, readErr)
			}

			issues, scanErr := SourceLimitIssues(file, source)
			if scanErr != nil {
				return scanErr
			}

			problems = append(problems, issues...)
		}

		return nil
	})

	return problems, err
}

// SourceLimitIssues enforces the same thresholds protected in the native lint configuration.
// The native max-public-structs rule counts exported type declarations, including aliases.
func SourceLimitIssues(name string, source []byte) ([]string, error) {
	fset := token.NewFileSet()

	parsed, err := parser.ParseFile(fset, name, source, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse owned source %s: %w", name, err)
	}

	lines := codeLines(fset.File(parsed.Pos()), parsed.Comments, source)
	exported := 0

	ast.Inspect(parsed, func(node ast.Node) bool {
		if declaration, ok := node.(*ast.TypeSpec); ok && ast.IsExported(declaration.Name.Name) {
			exported++
		}

		return true
	})

	var problems []string
	if lines > maxSourceLines {
		problems = append(problems, fmt.Sprintf("%s: %d code-bearing lines exceeds %d", name, lines, maxSourceLines))
	}

	if exported > maxExportedTypes {
		problems = append(problems, fmt.Sprintf("%s: %d exported type declarations exceeds %d", name, exported, maxExportedTypes))
	}

	return problems, nil
}

func codeLines(physical *token.File, comments []*ast.CommentGroup, source []byte) int {
	code := bytes.Clone(source)

	for _, group := range comments {
		for _, comment := range group.List {
			start := physical.Offset(comment.Pos())

			end := physicalCommentEnd(source, start)
			for index := start; index < end; index++ {
				if code[index] != '\n' {
					code[index] = ' '
				}
			}
		}
	}

	lines := 0

	for line := range bytes.SplitSeq(code, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			lines++
		}
	}

	return lines
}

// Comment.Text omits carriage returns, so Comment.End cannot identify the physical end of CRLF comments.
func physicalCommentEnd(source []byte, start int) int {
	if source[start+1] == '*' {
		return start + 2 + bytes.Index(source[start+2:], []byte("*/")) + 2
	}

	if end := bytes.IndexByte(source[start:], '\n'); end >= 0 {
		return start + end
	}

	return len(source)
}
