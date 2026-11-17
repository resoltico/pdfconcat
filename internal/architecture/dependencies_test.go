// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package architecture_test

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var dependencies = map[string][]string{
	"core": {"core"}, "typeset": {}, "compose": {"core", "typeset"}, "plan": {"core"}, "files": {"core"}, "pdf": {"core", "compose", "typeset"}, "wire": {"core"}, "app": {"core", "compose", "plan", "files", "pdf", "wire", "typeset"}, "cli": {"app", "core", "wire", "legal"},
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
	for _, tc := range [][2]string{{"core", "os"}, {"core", "encoding/json/v2"}, {"core", "runtime"}, {"core", "path/filepath"}, {"core", "github.com/resoltico/pdfconcat/internal/app"}, {"plan", "github.com/resoltico/pdfconcat/internal/files"}, {"typeset", "github.com/resoltico/pdfconcat/internal/core"}, {"cli", "github.com/resoltico/pdfconcat/internal/pdf"}, {"app", "github.com/pdfcpu/pdfcpu/pkg/api"}, {"pdf", "github.com/resoltico/pdfconcat/internal/files"}, {"wire", "github.com/resoltico/pdfconcat/internal/app"}, {"app", "github.com/resoltico/pdfconcat/internal/pdffixture"}, {"app", "golang.org/x/sys/unix"}, {"compose", "github.com/go-text/typesetting/shaping"}} {
		if allowed(tc[0], tc[1]) == nil {
			t.Errorf("gate admitted forbidden edge %v", tc)
		}
	}
}
