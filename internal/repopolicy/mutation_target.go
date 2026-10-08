// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// mutationAnchorLine finds a unique anchor and its physical operator-token byte column.
// The required actual discovery report establishes operator/type eligibility.
func mutationAnchorLine(entry *Entry, content []byte) (int, error) {
	if entry.Column == nil || *entry.Column < 1 {
		return 0, fmt.Errorf("%w: mutation column must be positive", ErrMutation)
	}

	line := 0

	for index, raw := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(raw) != entry.Anchor {
			continue
		}

		if line != 0 {
			return 0, fmt.Errorf("%w: mutation anchor is not unique", ErrMutation)
		}

		line = index + 1
	}

	if line == 0 {
		return 0, fmt.Errorf("%w: mutation anchor is missing", ErrMutation)
	}

	fset := token.NewFileSet()

	parsed, err := parser.ParseFile(fset, entry.Path, content, parser.SkipObjectResolution)
	if err != nil {
		return 0, fmt.Errorf("parse mutation source: %w", err)
	}

	found := false

	ast.Inspect(parsed, func(node ast.Node) bool {
		position := fset.PositionFor(operatorTokenPosition(node), false)
		if position.Line == line && position.Column == *entry.Column {
			found = true
		}

		return !found
	})

	if !found {
		return 0, fmt.Errorf("%w: mutation column %d does not identify an operator token at the anchor", ErrMutation, *entry.Column)
	}

	return line, nil
}

func operatorTokenPosition(node ast.Node) token.Pos {
	switch node := node.(type) {
	case *ast.AssignStmt:
		return node.TokPos
	case *ast.BinaryExpr:
		return node.OpPos
	case *ast.BranchStmt:
		return node.TokPos
	case *ast.IncDecStmt:
		return node.TokPos
	case *ast.UnaryExpr:
		return node.OpPos
	default:
		return token.NoPos
	}
}
