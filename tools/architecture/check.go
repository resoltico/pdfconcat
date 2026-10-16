// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package architecture applies the repository depguard policy to physical source,
// independently of build constraints and golangci exclusions.
package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/OpenPeeDeeP/depguard/v2"
	"go.yaml.in/yaml/v3"
	"golang.org/x/tools/go/analysis"
)

type rule struct {
	Mode  string   `yaml:"list-mode"`
	Files []string `yaml:"files"`
	Allow []string `yaml:"allow"`
	Deny  []struct {
		Pkg  string `yaml:"pkg"`
		Desc string `yaml:"desc"`
	} `yaml:"deny"`
}
type config struct {
	Linters struct {
		Settings struct {
			Depguard struct {
				Rules map[string]rule `yaml:"rules"`
			} `yaml:"depguard"`
		} `yaml:"settings"`
	} `yaml:"linters"`
}

// Check fails closed on discovery, syntax, ownership, policy and import errors.
// It deliberately does not claim type correctness for inactive code.
func Check(root string) (int, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(filepath.Join(root, ".golangci.yml"))
	if err != nil {
		return 0, err
	}
	var conf config
	if err = yaml.Unmarshal(data, &conf); err != nil {
		return 0, err
	}
	settings := depguard.LinterSettings{}
	owners := map[string]string{}
	counts := map[string]int{}
	for name, r := range conf.Linters.Settings.Depguard.Rules {
		if strings.HasPrefix(name, "layer-") {
			if r.Mode != "strict" || len(r.Files) != 2 || r.Files[1] != "!$test" {
				return 0, fmt.Errorf("owner %s requires strict mode, one literal directory and !$test", name)
			}
			selector := r.Files[0]
			if !strings.HasPrefix(selector, "**/") || !strings.HasSuffix(selector, "/*.go") {
				return 0, fmt.Errorf("invalid owner selector %s", selector)
			}
			dir := strings.TrimSuffix(strings.TrimPrefix(selector, "**/"), "/*.go")
			if dir == "." || !fs.ValidPath(dir) || strings.ContainsAny(dir, "*!?[]{}\\:") || filepath.IsAbs(dir) || filepath.ToSlash(filepath.Clean(dir)) != dir {
				return 0, fmt.Errorf("owner directory must be canonical and module-relative: %q", dir)
			}
			if previous, ok := owners[dir]; ok {
				return 0, fmt.Errorf("overlapping owners %s and %s", previous, name)
			}
			owners[dir] = name
		}
		list := &depguard.List{ListMode: r.Mode, Files: r.Files, Allow: r.Allow, Deny: map[string]string{}}
		for _, d := range r.Deny {
			if _, exists := list.Deny[d.Pkg]; exists {
				return 0, fmt.Errorf("duplicate deny %s", d.Pkg)
			}
			list.Deny[d.Pkg] = d.Desc
		}
		settings[name] = list
	}
	if len(owners) == 0 {
		return 0, fmt.Errorf("missing architecture owners")
	}
	analyzer, err := depguard.NewAnalyzer(&settings)
	if err != nil {
		return 0, err
	}
	fset := token.NewFileSet()
	var files []*ast.File
	modules := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path == filepath.Join(root, ".git") {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(path)
			if strings.HasSuffix(path, ".go") || entry.Name() == "go.mod" || entry.Name() == "go.work" || err != nil || info.IsDir() {
				return fmt.Errorf("source link is unsupported: %s", rel)
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == "go.work" {
			return fmt.Errorf("unexpected workspace: %s", rel)
		}
		if entry.Name() == "go.mod" {
			if rel != "go.mod" && rel != "tools/go.mod" {
				return fmt.Errorf("unexpected module: %s", rel)
			}
			modules[rel] = true
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		owner, ok := owners[dir]
		if !ok {
			return fmt.Errorf("unclassified package directory: %s", dir)
		}
		counts[owner]++
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.AllErrors)
		if err != nil {
			return err
		}
		files = append(files, file)
		return nil
	})
	if err != nil {
		return len(files), err
	}
	if len(modules) != 2 {
		return len(files), fmt.Errorf("root and tools modules must both be present")
	}
	for dir, owner := range owners {
		if counts[owner] == 0 {
			return len(files), fmt.Errorf("stale owner %s for %s", owner, dir)
		}
	}
	var diagnostics []string
	_, err = analyzer.Run(&analysis.Pass{Analyzer: analyzer, Fset: fset, Files: files, Report: func(d analysis.Diagnostic) {
		diagnostics = append(diagnostics, fset.Position(d.Pos).String()+": "+d.Message)
	}})
	if err != nil {
		return len(files), err
	}
	if len(diagnostics) > 0 {
		sort.Strings(diagnostics)
		return len(files), fmt.Errorf("architecture violations:\n%s", strings.Join(diagnostics, "\n"))
	}
	return len(files), nil
}
