// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func stagedPackagesWithTests(ctx context.Context, root string, patterns []string, options testOptions) ([]string, error) {
	if options.stage == nil {
		return packagesWithTests(ctx, root, patterns)
	}

	args := append(
		[]string{goListVerb, "-f", "{{.ImportPath}} {{len .TestGoFiles}} {{len .XTestGoFiles}}"},
		options.stage.flags(options)...)
	request := goCommand(root, append(args, patterns...)...)
	request.env = options.stage.environment()

	output, err := request.output(ctx)
	if err != nil {
		return nil, fmt.Errorf("staged test discovery: %w", err)
	}

	var packages []string

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != listFields {
			return nil, fmt.Errorf("%w: malformed staged test discovery", errGate)
		}

		if fields[1] != "0" || fields[2] != "0" {
			packages = append(packages, fields[0])
		}
	}

	return packages, nil
}

func rejectForeignSkips(outcome *testOutcome, sources []repopolicy.ForeignSource) error {
	var problems []string

	for key, status := range outcome.completed {
		if status != actionSkip {
			continue
		}

		pkg, _, _ := strings.Cut(key, " ")

		for index := range sources {
			source := &sources[index]
			if pkg == source.Module || strings.HasPrefix(pkg, source.Module+"/") {
				problems = append(problems, "foreign test skipped: "+key)
			}
		}
	}

	return report("foreign test discovery", problems, "foreign selected tests did not skip")
}

func (stage *foreignTestStage) testSelection(ctx context.Context, root string, patterns []string, options testOptions) ([]string, error) {
	if len(patterns) == 0 {
		owned, err := testPackages(ctx, root)
		if err != nil {
			return nil, err
		}

		_, foreign, err := nativeForeignGraph(ctx, root, owned, stage, options, "")
		if err != nil {
			return nil, err
		}

		if len(stage.sources) > 0 && len(foreign) == 0 {
			return nil, fmt.Errorf("%w: native discovery omitted foreign packages", errGate)
		}

		patterns = slices.Concat(owned, foreign)
	}

	if err := stage.compilerEquivalent(ctx, root, patterns, options); err != nil {
		return nil, err
	}

	request := goCommand(root, append(append([]string{"vet"}, stage.flags(options)...), patterns...)...)

	request.env = stage.environment()
	if _, err := request.output(ctx); err != nil {
		return nil, fmt.Errorf("vet native staged graph: %w", err)
	}

	return patterns, nil
}
