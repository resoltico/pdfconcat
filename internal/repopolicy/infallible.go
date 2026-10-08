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

const stringsBuilderSource = "src/strings/builder.go"

// InfallibleMethodPremiseIssues checks every registered errcheck method exception against the
// selected Go SDK source. The registry remains the exception authority; this small check recognizes
// only the concrete library contracts the project has independently reviewed.
func InfallibleMethodPremiseIssues(entries []*Entry, read SourceReader) []string {
	var problems []string

	for _, entry := range entries {
		if entry.Tool != ToolLint || entry.Setting != "linters.settings.errcheck.exclude-functions" {
			continue
		}

		for _, symbol := range entry.Values {
			if err := infallibleMethod(symbol, read); err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", entry.ID, err))
			}
		}
	}

	return problems
}

func infallibleMethod(symbol string, read SourceReader) error {
	if strings.HasPrefix(symbol, "fmt.Fprintf(") {
		return infallibleFormatter(symbol, read)
	}

	contracts := map[string]string{
		"(*strings.Builder).Write":       stringsBuilderSource,
		"(*strings.Builder).WriteRune":   stringsBuilderSource,
		"(*strings.Builder).WriteString": stringsBuilderSource,
		"(*bytes.Buffer).Write":          "src/bytes/buffer.go",
		"(*bytes.Buffer).WriteString":    "src/bytes/buffer.go",
	}

	path, known := contracts[symbol]
	if !known {
		return fmt.Errorf("%w: no independently reviewed infallible method %q", ErrLintConfig, symbol)
	}

	content, err := read(path)
	if err != nil {
		return fmt.Errorf("read Go SDK contract %s: %w", path, err)
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), path, content, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parse Go SDK contract %s: %w", path, err)
	}

	receiver, method, _ := strings.Cut(strings.TrimPrefix(symbol, "(*"), ").")
	_, receiver, _ = strings.Cut(receiver, ".")

	for _, decl := range parsed.Decls {
		function, isFunction := decl.(*ast.FuncDecl)
		if isFunction && function.Name.Name == method && pointerReceiver(function, receiver) && explicitNilErrors(function) {
			return nil
		}
	}

	return fmt.Errorf("%w: %s no longer has a verified nil-error implementation; review or remove its exception", ErrLintConfig, symbol)
}

func pointerReceiver(function *ast.FuncDecl, name string) bool {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return false
	}

	pointer, isPointer := function.Recv.List[0].Type.(*ast.StarExpr)
	if !isPointer {
		return false
	}

	identifier, isIdentifier := pointer.X.(*ast.Ident)

	return isIdentifier && identifier.Name == name
}

func explicitNilErrors(function *ast.FuncDecl) bool {
	if function.Body == nil || function.Type.Results == nil {
		return false
	}

	results := function.Type.Results.List

	errorType, isError := results[len(results)-1].Type.(*ast.Ident)
	if !isError || errorType.Name != "error" {
		return false
	}

	found, valid := false, true

	ast.Inspect(function.Body, func(node ast.Node) bool {
		returned, isReturn := node.(*ast.ReturnStmt)
		if !isReturn {
			return true
		}

		found = true

		if len(returned.Results) == 0 {
			valid = false
			return false
		}

		final, isIdentifier := returned.Results[len(returned.Results)-1].(*ast.Ident)
		valid = valid && isIdentifier && final.Name == "nil"

		return valid
	})

	return found && valid
}

// infallibleFormatter verifies the exact fmt entrypoint delegates its sole returned error to Write
// on one registered concrete nil-error sink. An [io.Writer] interface never satisfies this premise.
func infallibleFormatter(symbol string, read SourceReader) error {
	var writer string

	switch symbol {
	case "fmt.Fprintf(*strings.Builder)":
		writer = "(*strings.Builder).Write"
	case "fmt.Fprintf(*bytes.Buffer)":
		writer = "(*bytes.Buffer).Write"
	default:
		return fmt.Errorf("%w: formatter does not have a reviewed concrete sink: %s", ErrLintConfig, symbol)
	}

	if err := infallibleMethod(writer, read); err != nil {
		return err
	}

	content, err := read("src/fmt/print.go")
	if err != nil {
		return fmt.Errorf("read Go SDK formatter: %w", err)
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), "src/fmt/print.go", content, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parse Go SDK formatter: %w", err)
	}

	const reviewed = "p:=newPrinter()p.doPrintf(format,a)n,err=w.Write(p.buf)p.free()return"

	for _, decl := range parsed.Decls {
		function, isFunction := decl.(*ast.FuncDecl)
		if !isFunction || function.Name.Name != "Fprintf" || function.Body == nil {
			continue
		}

		body := content[int(function.Body.Pos()) : int(function.Body.End())-2]
		if strings.Join(strings.Fields(string(body)), "") == reviewed {
			return nil
		}
	}

	return fmt.Errorf("%w: fmt.Fprintf implementation changed; review its returned-error sources", ErrLintConfig)
}
