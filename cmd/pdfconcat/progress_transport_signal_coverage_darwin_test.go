// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin && cgo && progress_native

package main

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/exectest"
	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func progressSignalCoverDirectory(t *testing.T, scenario string) string {
	t.Helper()

	if root := os.Getenv(exectest.EnvCoverDir); root != "" {
		// The gate owns this evidence root; unlike TempDir it retains each original child profile.
		directory, err := os.MkdirTemp(root, "native-signal-"+scenario+"-")
		requireProgressNoError(t, err)

		return directory
	}

	return t.TempDir()
}

func assertProgressNativeRetryCounter(ctx context.Context, t *testing.T, directory string) {
	t.Helper()

	profile := filepath.Join(directory, "retry-child.out")
	tool := progressNativeCoverageTool(ctx, t)
	command := exec.CommandContext(ctx, tool, "tool", "covdata", "textfmt", "-i="+directory, "-o="+profile)

	output, err := command.CombinedOutput()
	if ctx.Err() != nil || err != nil {
		t.Fatalf("native retry child counters unavailable (watchdog=%v): %v\n%s", ctx.Err(), err, output)
	}

	owned, err := os.OpenRoot(directory)

	requireProgressNoError(t, err)
	defer func() { requireProgressNoError(t, owned.Close()) }()

	data, err := owned.ReadFile("retry-child.out")
	requireProgressNoError(t, err)
	parsed, err := repopolicy.ParseCoverageProfile(bytes.NewReader(data))
	requireProgressNoError(t, err)
	line := progressNativeRetryContinueLine(t)
	matched := 0

	for _, block := range parsed.Blocks {
		if !strings.HasSuffix(block.File, "/cmd/pdfconcat/progress_transport_darwin.go") || block.StartLine != line ||
			block.EndLine != line {
			continue
		}

		matched++

		if parsed.Mode != "atomic" || block.Stmts != 1 || block.Count == 0 {
			t.Fatalf("retry child did not execute the source-matched EINTR continuation: mode=%s block=%+v", parsed.Mode, block)
		}

		t.Logf("actual retry child counter: %s:%d count=%d atomic; raw=%s", block.File, line, block.Count, directory)
	}

	if matched != 1 {
		t.Fatal("retry child's EINTR continuation did not have exactly one source-matched coverage block")
	}
}

func progressNativeCoverageTool(ctx context.Context, t *testing.T) string {
	t.Helper()

	tool, err := exec.LookPath("go")
	requireProgressNoError(t, err)

	command := exec.CommandContext(ctx, tool, "version")
	output, err := command.CombinedOutput()
	requireProgressNoError(t, err)

	if strings.TrimSpace(string(output)) != "go version "+runtime.Version()+" "+runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatal("native coverage tool does not match this test's Go SDK and platform")
	}

	return tool
}

func progressNativeRetryContinueLine(t *testing.T) int {
	t.Helper()

	positions := token.NewFileSet()
	source, err := parser.ParseFile(positions, "progress_transport_darwin.go", nil, 0)
	requireProgressNoError(t, err)

	for _, declaration := range source.Decls {
		function, valid := declaration.(*ast.FuncDecl)
		if !valid || function.Name.Name != "writeProgressPipeRecord" {
			continue
		}

		return positions.Position(progressNativeRetryPosition(t, function.Body)).Line
	}

	t.Fatal("production source has no native pipe writer")

	return 0
}

func progressNativeRetryPosition(t *testing.T, body *ast.BlockStmt) token.Pos {
	t.Helper()

	var positions []token.Pos

	ast.Inspect(body, func(node ast.Node) bool {
		guard, valid := node.(*ast.IfStmt)
		if !valid || !progressNativeEINTRCondition(guard.Cond) || len(guard.Body.List) != 1 {
			return true
		}

		branch, valid := guard.Body.List[0].(*ast.BranchStmt)
		if valid && branch.Tok == token.CONTINUE && branch.Label == nil {
			positions = append(positions, branch.Pos())
		}

		return true
	})

	if len(positions) != 1 {
		t.Fatal("production source must have exactly one identifiable native EINTR retry continuation")
	}

	return positions[0]
}

func progressNativeEINTRCondition(expression ast.Expr) bool {
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

	value, valid := call.Args[1].(*ast.SelectorExpr)
	if !valid || value.Sel.Name != "EINTR" {
		return false
	}

	owner, valid = value.X.(*ast.Ident)

	return valid && owner.Name == "unix"
}
