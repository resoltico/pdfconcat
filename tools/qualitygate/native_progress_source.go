// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func baselineGoCommand(root string, args ...string) *command {
	run := goCommand(root, args...)
	run.env = []string{readonlyGoFlags, baselineCGOEnv}

	return run
}

func nativeProgressEnv() []string {
	return []string{readonlyGoFlags + " -tags=" + nativeProgressTag, "CGO_ENABLED=1"}
}

// nativeProductionSources proves the fault fixture is outside the product dependency closure and
// that enabling its test lane does not change any selected product source.
func nativeProductionSources(ctx context.Context, root string) (map[string]string, error) {
	baseline, err := productSources(ctx, root, []string{readonlyGoFlags, baselineCGOEnv})
	if err != nil {
		return nil, err
	}

	native, err := productSources(ctx, root, nativeProgressEnv())
	if err != nil {
		return nil, err
	}

	if !maps.Equal(baseline, native) {
		return nil, fmt.Errorf("%w: native progress changes production source selection or bytes", errGate)
	}

	files, err := repopolicy.OwnedGoSources(ctx, root)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		content, readErr := readInRoot(root, file)
		if readErr != nil {
			return nil, readErr
		}

		baseline["owned-source/"+file] = fmt.Sprintf("%x", sha256.Sum256(content))
	}

	return baseline, nil
}

func productSources(ctx context.Context, root string, env []string) (map[string]string, error) {
	run := goCommand(root, goListVerb, foreignDependencyFlag, "-compiled", jsonFlag, productCommandPackage)
	run.env = env

	output, err := run.output(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover product source: %w", err)
	}

	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}

	return decodeProductSources(output, module)
}

func decodeProductSources(output, module string) (map[string]string, error) {
	result := map[string]string{}
	decoder := json.NewDecoder(strings.NewReader(output))

	for {
		var pkg discoveredPackage

		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("decode product source: %w", err)
		}

		if sourceErr := recordProductSources(result, &pkg, module); sourceErr != nil {
			return nil, sourceErr
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("%w: empty product source inventory", errGate)
	}

	return result, nil
}

func recordProductSources(result map[string]string, pkg *discoveredPackage, module string) error {
	if pkg.Error != nil || len(pkg.DepsErrors) != 0 {
		return fmt.Errorf("%w: product source discovery errors in %s", errGate, pkg.ImportPath)
	}

	if pkg.Module == nil {
		return nil
	}

	if pkg.ImportPath == module+"/"+nativeProgressFixture {
		return fmt.Errorf("%w: native fault fixture imported by production", errGate)
	}

	for _, name := range selectedProductInputs(pkg) {
		content, err := readInRoot(pkg.Dir, name)
		if err != nil {
			return fmt.Errorf("read product source: %w", err)
		}

		result[pkg.ImportPath+"/"+name] = fmt.Sprintf("%x", sha256.Sum256(content))
	}

	return nil
}

// lintNativeProgressSources explicitly analyzes tagged tests and their test-support package.
func lintNativeProgressSources(ctx context.Context, root, binary string) error {
	if runtime.GOOS != nativeProgressOS {
		return nil
	}

	run := &command{
		dir:  root,
		name: binary,
		env:  nativeProgressEnv(),
		args: []string{
			runVerb,
			serialLintRunners,
			lintNoFixFlag,
			configFlag,
			filepath.Join(root, lintConfigFileName),
			productCommandPackage,
			"./" + nativeProgressFixture,
		},
		stdout: log.Writer(),
		stderr: log.Writer(),
	}
	if err := run.run(ctx); err != nil {
		return fmt.Errorf("native progress source lint: %w", err)
	}

	return nil
}

func selectedProductInputs(pkg *discoveredPackage) []string {
	files := append([]string(nil), pkg.GoFiles...)
	for _, selected := range [][]string{pkg.CgoFiles, pkg.CFiles, pkg.HFiles, pkg.SFiles, pkg.SysoFiles, pkg.EmbedFiles} {
		files = append(files, selected...)
	}

	return files
}

func checkNativeSources(ctx context.Context, root string, expected map[string]string) error {
	after, err := nativeProductionSources(ctx, root)
	if err != nil {
		return err
	}

	if !maps.Equal(expected, after) {
		return fmt.Errorf("%w: production or fixture inputs changed during native progress verification", errGate)
	}

	return nil
}
