// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const nativeRetrySource = "cmd/pdfconcat/progress_transport_darwin.go"

// nativeRetryCounter independently attributes retry coverage to the actual retry child's data.
func nativeRetryCounter(root string, profile *repopolicy.CoverageProfile) error {
	content, err := readInRoot(root, nativeRetrySource)
	if err != nil {
		return err
	}

	line, err := nativeRetryContinueLine(content)
	if err != nil {
		return err
	}

	matches := 0

	for _, block := range profile.Blocks {
		if strings.HasSuffix(block.File, "/"+nativeRetrySource) && block.StartLine == line && block.EndLine == line {
			matches++

			if block.Stmts != 1 || block.Count <= 0 || profile.Mode != "atomic" {
				return fmt.Errorf("%w: actual retry child did not execute the EINTR continuation", errGate)
			}
		}
	}

	if matches != 1 {
		return fmt.Errorf("%w: actual retry child has no unique source-matched EINTR block", errGate)
	}

	return nil
}

func nativeRetryContinueLine(content []byte) (int, error) {
	positions := token.NewFileSet()

	source, err := parser.ParseFile(positions, nativeRetrySource, content, parser.SkipObjectResolution)
	if err != nil {
		return 0, fmt.Errorf("parse native retry source: %w", err)
	}

	lines := []int{}

	for _, declaration := range source.Decls {
		function, valid := declaration.(*ast.FuncDecl)
		if !valid || function.Name.Name != "writeProgressPipeRecord" {
			continue
		}

		ast.Inspect(function.Body, func(node ast.Node) bool {
			conditional, isIf := node.(*ast.IfStmt)
			if !isIf || !nativeRetryCondition(conditional.Cond) || len(conditional.Body.List) != 1 {
				return true
			}

			branch, isBranch := conditional.Body.List[0].(*ast.BranchStmt)
			if isBranch && branch.Tok == token.CONTINUE {
				lines = append(lines, positions.PositionFor(branch.Pos(), false).Line)
			}

			return true
		})
	}

	if len(lines) != 1 {
		return 0, fmt.Errorf("%w: source has no unique native EINTR continuation", errGate)
	}

	return lines[0], nil
}

func nativeRetryCondition(expression ast.Expr) bool {
	call, valid := expression.(*ast.CallExpr)
	if !valid || len(call.Args) != 2 {
		return false
	}

	function, valid := call.Fun.(*ast.SelectorExpr)
	if !valid || function.Sel.Name != "Is" {
		return false
	}

	owner, valid := function.X.(*ast.Ident)
	if !valid || owner.Name != "errors" {
		return false
	}

	argument, valid := call.Args[0].(*ast.Ident)
	if !valid || argument.Name != "err" {
		return false
	}

	target, valid := call.Args[1].(*ast.SelectorExpr)
	if !valid || target.Sel.Name != "EINTR" {
		return false
	}

	namespace, valid := target.X.(*ast.Ident)

	return valid && namespace.Name == "unix"
}
