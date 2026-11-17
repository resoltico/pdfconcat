// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package architecture_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var dependencies = map[string][]string{
	"core": {"core"}, "typeset": {}, "compose": {"core", "typeset"}, "plan": {"core"}, "files": {"core"}, "pdf": {"core", "compose", "typeset"}, "wire": {"core"}, "app": {"core", "compose", "plan", "files", "pdf", "wire"}, "cli": {"app", "core", "wire", "legal"},
}

func allowed(owner, path string) error {
	if strings.HasPrefix(path, "github.com/resoltico/pdfconcat/internal/") {
		target := strings.Split(strings.TrimPrefix(path, "github.com/resoltico/pdfconcat/internal/"), "/")[0]
		for _, dependency := range dependencies[owner] {
			if target == dependency {
				return nil
			}
		}
		return fmt.Errorf("%s must not import %s", owner, target)
	}
	if strings.HasPrefix(path, "github.com/pdfcpu/pdfcpu") && owner != "pdf" {
		return errors.New("pdfcpu belongs to PDF adapter")
	}
	if strings.HasPrefix(path, "github.com/go-text/typesetting") && owner != "typeset" {
		return errors.New("shaping library belongs to typeset")
	}
	if strings.HasPrefix(path, "golang.org/x/sys") && owner != "files" {
		return errors.New("native system calls belong to files")
	}
	if owner != "files" && (path == "os" || strings.HasPrefix(path, "os/") || path == "syscall" || path == "net" || strings.HasPrefix(path, "net/") && path != "net/url") {
		return fmt.Errorf("native/process/network I/O forbidden in %s", owner)
	}
	if (owner == "core" || owner == "typeset" || owner == "compose" || owner == "plan" || owner == "wire") && (path == "runtime" || path == "runtime/debug" || path == "path/filepath" && owner == "core") {
		return fmt.Errorf("ambient host discovery forbidden in %s", owner)
	}
	if owner == "core" && strings.HasPrefix(path, "encoding/json") {
		return errors.New("JSON belongs to adapters")
	}
	return nil
}
func TestProductionDependencies(t *testing.T) {
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel("..", path)
		if err != nil {
			return err
		}
		owner := strings.Split(filepath.ToSlash(relative), "/")[0]
		if _, ok := dependencies[owner]; !ok {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			name, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if err = allowed(owner, name); err != nil {
				t.Errorf("%s: %v", path, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestForbiddenDependencyControls(t *testing.T) {
	for _, tc := range [][2]string{{"core", "os"}, {"core", "encoding/json/v2"}, {"core", "runtime"}, {"core", "path/filepath"}, {"core", "github.com/resoltico/pdfconcat/internal/app"}, {"plan", "github.com/resoltico/pdfconcat/internal/files"}, {"typeset", "github.com/resoltico/pdfconcat/internal/core"}, {"cli", "github.com/resoltico/pdfconcat/internal/pdf"}, {"app", "github.com/pdfcpu/pdfcpu/pkg/api"}, {"app", "github.com/resoltico/pdfconcat/internal/typeset"}, {"pdf", "github.com/resoltico/pdfconcat/internal/files"}, {"wire", "github.com/resoltico/pdfconcat/internal/app"}, {"app", "github.com/resoltico/pdfconcat/internal/pdffixture"}, {"app", "golang.org/x/sys/unix"}, {"compose", "github.com/go-text/typesetting/shaping"}} {
		if allowed(tc[0], tc[1]) == nil {
			t.Errorf("gate admitted forbidden edge %v", tc)
		}
	}
}

// Product adapters may emit technical evidence but cannot author product text.
func hasUnownedProductConstructor(source string) bool {
	file, err := parser.ParseFile(token.NewFileSet(), "adapter.go", source, 0)
	if err != nil {
		return true
	}
	unowned := false
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if ok && id.Name == "diag" && sel.Sel.Name == "New" {
			unowned = true
		}
		return true
	})
	return unowned
}
func TestProductExplanationsAreCoreOwned(t *testing.T) {
	for _, owner := range []string{"app", "compose", "files", "pdf"} {
		paths, err := filepath.Glob("../" + owner + "/*.go")
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if hasUnownedProductConstructor(string(data)) {
				t.Errorf("%s authors product wording outside core", path)
			}
		}
	}
	if !hasUnownedProductConstructor(`package adapter;func f(){diag.New(code,"unowned product explanation")}`) {
		t.Fatal("vocabulary gate missed a forbidden construction")
	}
	if hasUnownedProductConstructor(`package adapter;func f(){diag.Report(code,diag.ReportAndOutputMustBeDifferentFiles())}`) {
		t.Fatal("vocabulary gate rejected its owner")
	}
}

func TestPlanConstructionHasOneCheckedProductionEntry(t *testing.T) {
	paths, err := filepath.Glob("../core/*.go")
	if err != nil {
		t.Fatal(err)
	}
	constructors := []string{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() || fn.Type.Results == nil {
				continue
			}
			for _, result := range fn.Type.Results.List {
				if name, ok := result.Type.(*ast.Ident); ok && name.Name == "Plan" {
					constructors = append(constructors, fn.Name.Name)
				}
			}
		}
	}
	if len(constructors) != 1 || constructors[0] != "AdmitDeclaration" {
		t.Fatalf("unchecked prepared-state entry exposed: %v", constructors)
	}
}
