// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

type discoveredModule struct {
	Replace *discoveredModule `json:"replace"`
	Path    string            `json:"path"`
	Version string            `json:"version"`
	Dir     string            `json:"dir"`
}

func foreignReleaseTargets(ctx context.Context, root string) error {
	sources, err := repopolicy.ForeignSourcesContext(ctx, root)
	if err != nil || len(sources) == 0 {
		return err
	}

	for _, target := range archiveTargets() {
		if _, err = discoverForeignPackages(ctx, root, target, []string{"./cmd/pdfconcat"}, sources); err != nil {
			return err
		}
	}

	return nil
}

func discoverForeignPackages(
	ctx context.Context,
	root, target string,
	patterns []string,
	sources []repopolicy.ForeignSource,
) ([]string, error) {
	request := goCommand(root, append([]string{goListVerb, "-deps", "-test", "-e", jsonFlag}, patterns...)...)

	request.env = []string{readonlyGoFlags, "GOWORK=off"}
	if target != "" {
		request.env = append(architectureTargetEnv(target), "GOWORK=off")
	}

	output, err := request.output(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover effective foreign graph for %s: %w", target, err)
	}

	return parseForeignPackages(root, output, sources)
}

func parseForeignPackages(root, output string, sources []repopolicy.ForeignSource) ([]string, error) {
	expected := map[string]*repopolicy.ForeignSource{}
	for index := range sources {
		expected[sources[index].Module] = &sources[index]
	}

	seen, packages := map[string]bool{}, map[string]bool{}
	decoder := json.NewDecoder(strings.NewReader(output))

	for {
		var pkg discoveredPackage
		if err := decoder.Decode(&pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode foreign graph: %w", err)
		}

		moduleName, packageName, sourceErr := foreignGraphNames(root, &pkg, expected)
		if sourceErr != nil {
			return nil, sourceErr
		}

		if moduleName != "" {
			seen[moduleName] = true
		}

		if packageName != "" {
			packages[packageName] = true
		}
	}

	if len(seen) != len(expected) || len(packages) == 0 {
		return nil, fmt.Errorf("%w: effective foreign package discovery omitted declared dependencies", errGate)
	}

	return slices.Sorted(maps.Keys(packages)), nil
}

func validateForeignPackage(root string, pkg *discoveredPackage, source repopolicy.ForeignSource) error {
	expected, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(source.Root)))
	if err != nil {
		return fmt.Errorf("resolve declared foreign root: %w", err)
	}

	if validateErr := validateNativeForeignModule(pkg, &source, expected); validateErr != nil {
		return validateErr
	}

	if filepath.ToSlash(filepath.Clean(pkg.Module.Replace.Path)) != source.Root {
		return fmt.Errorf("%w: foreign replacement path differs from declared local root", errGate)
	}

	return nil
}

func foreignGraphNames(root string, pkg *discoveredPackage, expected map[string]*repopolicy.ForeignSource) (string, string, error) {
	if pkg.Error != nil || len(pkg.DepsErrors) > 0 {
		return "", "", fmt.Errorf("%w: erroneous dependency discovery for %s", errGate, pkg.ImportPath)
	}

	if pkg.Module == nil {
		return "", "", nil
	}

	source := expected[pkg.Module.Path]
	if source == nil {
		return "", "", nil
	}

	if err := validateForeignPackage(root, pkg, *source); err != nil {
		return "", "", err
	}

	if eligibleForeignPackage(pkg) {
		return source.Module, pkg.ImportPath, nil
	}

	return source.Module, "", nil
}

func eligibleForeignPackage(pkg *discoveredPackage) bool {
	return pkg.ForTest == "" && (pkg.Name != foreignTestMain || !strings.HasSuffix(pkg.ImportPath, foreignTestSuffix))
}
