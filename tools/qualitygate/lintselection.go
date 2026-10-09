// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

// diagnosticTargetFiles uses the same compiler-selected owned-package discovery as architecture.
func diagnosticTargetFiles(ctx context.Context, root string) (map[string]map[string]bool, error) {
	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}

	dirs, err := repopolicy.OwnedGoDirectories(ctx, root)
	if err != nil {
		return nil, err
	}

	if len(dirs) == 0 {
		return nil, fmt.Errorf("%w: no owned package selection", errGate)
	}

	patterns := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		patterns = append(patterns, "./"+dir)
	}

	targets := map[string]map[string]bool{}

	for _, variant := range supportedSourceVariants() {
		target := variant.name

		env := variant.env
		if target == runtime.GOOS+"/"+runtime.GOARCH {
			env = []string{readonlyGoFlags}
		}

		discovery := &command{
			dir: root, name: goTool,
			args: append([]string{goListVerb, "-e", jsonFlag}, patterns...),
			env:  env,
		}

		output, loadErr := discovery.output(ctx)
		if loadErr != nil {
			return nil, fmt.Errorf("diagnostic source discovery for %s: %w", target, loadErr)
		}

		selected := map[string]bool{}
		if collectErr := collectArchitectureFiles(output, module, selected); collectErr != nil {
			return nil, fmt.Errorf("diagnostic source discovery for %s: %w", target, collectErr)
		}

		targets[target] = selected
	}

	return targets, nil
}

func applicableDiagnosticEntries(ctx context.Context, root string, entries []*repopolicy.Entry) ([]*repopolicy.Entry, error) {
	if problems := repopolicy.RepositoryIssues(entries, fileReader(root)); len(problems) > 0 {
		return nil, fmt.Errorf("%w: registry source defects: %s", errGate, strings.Join(problems, "; "))
	}

	targets, err := diagnosticTargetFiles(ctx, root)
	if err != nil {
		return nil, err
	}

	nativeFiles := targets[runtime.GOOS+"/"+runtime.GOARCH]
	if runtime.GOOS == nativeProgressOS {
		for file := range targets[runtime.GOOS+"/"+runtime.GOARCH+"/"+nativeProgressTag] {
			nativeFiles[file] = true
		}
	}

	selected, problems := selectDiagnosticEntries(entries, targets, nativeFiles)
	if len(problems) > 0 {
		return nil, fmt.Errorf("%w: %s", errGate, strings.Join(problems, "; "))
	}

	for _, entry := range entries {
		if entry.Tool == repopolicy.ToolLint && entry.Effect == repopolicy.EffectExcludeDiagnostic &&
			!targets[runtime.GOOS+"/"+runtime.GOARCH][entry.Path] && !moduleDiagnosticEntry(entry) {
			log.Printf("lint-stale: %s: source not compiled on native target; not adjudicated", entry.ID)
		}
	}

	return selected, nil
}

func selectDiagnosticEntries(
	entries []*repopolicy.Entry,
	targets map[string]map[string]bool,
	nativeFiles map[string]bool,
) ([]*repopolicy.Entry, []string) {
	var (
		selected []*repopolicy.Entry
		problems []string
	)

	for _, entry := range entries {
		if entry.Tool != repopolicy.ToolLint || entry.Effect != repopolicy.EffectExcludeDiagnostic {
			selected = append(selected, entry)
			continue
		}

		if problem := diagnosticPlatformScopeIssue(entry, targets); problem != "" {
			problems = append(problems, problem)
		}

		if nativeFiles[entry.Path] || moduleDiagnosticEntry(entry) {
			selected = append(selected, entry)
		}
	}

	return selected, problems
}

// diagnosticPlatformScopeIssue verifies compiler ownership can express the registry's OS predicate.
func diagnosticPlatformScopeIssue(entry *repopolicy.Entry, targets map[string]map[string]bool) string {
	if moduleDiagnosticEntry(entry) {
		if entry.GOOS != "" {
			return entry.ID + ": module declaration diagnostics apply to every supported target"
		}

		return ""
	}

	possible, broader := false, false

	for target, files := range targets {
		goos, _, _ := strings.Cut(target, "/")

		if !files[entry.Path] {
			continue
		}

		if entry.GOOS == "" || entry.GOOS == goos {
			possible = true
		} else {
			broader = true
		}
	}

	if !possible {
		return entry.ID + ": diagnostic source is not compiled on any applicable supported target"
	} else if broader {
		return entry.ID + ": unconditional lint exclusion also applies outside its registered goos scope"
	}

	return ""
}

// Module declaration diagnostics inspect the exact repository module file, not a compiler Go file.
// RepositoryIssues still proves its physical source predicate before this selection runs.
func moduleDiagnosticEntry(entry *repopolicy.Entry) bool {
	return entry.Tool == repopolicy.ToolLint && entry.Effect == repopolicy.EffectExcludeDiagnostic &&
		entry.Linter == "gomoddirectives" && entry.Path == moduleFileName
}
