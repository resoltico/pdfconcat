// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

type foreignCompilerGraph map[string]string

const (
	foreignDependencyFlag = "-deps"
	foreignTestsFlag      = "-test"
	foreignTestMain       = "main"
	foreignTestSuffix     = ".test"
)

// nativeForeignGraph uses precisely the native race, CGO and build-tag environment
// of the following vet/test command, including an explicit test dependency closure.
func nativeForeignGraph(
	ctx context.Context,
	root string,
	patterns []string,
	stage *foreignTestStage,
	options testOptions,
	modfile string,
) (foreignCompilerGraph, []string, error) {
	args := slices.Concat(
		[]string{goListVerb, foreignDependencyFlag, foreignTestsFlag, "-e", jsonFlag},
		nativeTestFlags(options, modfile),
		patterns,
	)
	request := goCommand(root, args...)
	request.env = stage.environment()

	output, err := request.output(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("native foreign compiler discovery: %w", err)
	}

	return decodeForeignCompilerGraph(root, output, stage, modfile)
}

func decodeForeignCompilerGraph(root, output string, stage *foreignTestStage, modfile string) (foreignCompilerGraph, []string, error) {
	graph := foreignCompilerGraph{}
	foreign := map[string]bool{}
	decoder := json.NewDecoder(strings.NewReader(output))

	for {
		var descriptor map[string]any
		if err := decoder.Decode(&descriptor); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, nil, fmt.Errorf("decode native compiler graph: %w", err)
		}

		pkg, normalized, err := stage.compilerDescriptor(root, descriptor, modfile)
		if err != nil {
			return nil, nil, err
		}

		if _, exists := graph[pkg.ImportPath]; exists {
			return nil, nil, fmt.Errorf("%w: duplicate compiler package %s", errGate, pkg.ImportPath)
		}

		graph[pkg.ImportPath] = normalized
		if stage.isForeignTestPackage(pkg) {
			foreign[pkg.ImportPath] = true
		}
	}

	if len(graph) == 0 {
		return nil, nil, fmt.Errorf("%w: empty native compiler graph", errGate)
	}

	return graph, slices.Sorted(maps.Keys(foreign)), nil
}

func (stage *foreignTestStage) normalizeDescriptor(root string, value any) {
	switch container := value.(type) {
	case map[string]any:
		for key, child := range container {
			if text, ok := child.(string); ok {
				container[key] = stage.canonicalGraphPath(root, text)
			} else {
				stage.normalizeDescriptor(root, child)
			}
		}
	case []any:
		for index, child := range container {
			if text, ok := child.(string); ok {
				container[index] = stage.canonicalGraphPath(root, text)
			} else {
				stage.normalizeDescriptor(root, child)
			}
		}
	default:
	}
}

func (stage *foreignTestStage) canonicalGraphPath(root, text string) string {
	if text == stage.modfile && stage.modfile != "" {
		return filepath.Join(root, moduleFileName)
	}

	for canonical, staged := range stage.roots {
		if text == staged {
			return canonical
		}

		if strings.HasPrefix(text, staged+string(filepath.Separator)) {
			return canonical + strings.TrimPrefix(text, staged)
		}

		if text == "./"+filepath.ToSlash(strings.TrimPrefix(canonical, root+string(filepath.Separator))) {
			return canonical
		}
	}

	return text
}

func (stage *foreignTestStage) checkCompilerInputs(root string, pkg *discoveredPackage, descriptor map[string]any, modfile string) error {
	if pkg.Module == nil {
		return nil
	}

	for index := range stage.sources {
		source := &stage.sources[index]
		if pkg.Module.Path != source.Module {
			continue
		}

		canonical := filepath.Join(root, filepath.FromSlash(source.Root))

		physical := canonical
		if modfile != "" {
			physical = stage.roots[canonical]
		}

		if err := validateNativeForeignModule(pkg, source, physical); err != nil {
			return err
		}

		if pkg.Name == foreignTestMain && strings.HasSuffix(pkg.ImportPath, foreignTestSuffix) {
			return stage.validateSyntheticTestMain(pkg)
		}

		return checkSelectedForeignFiles(pkg, descriptor, canonical, physical)
	}

	return nil
}

func validateNativeForeignModule(pkg *discoveredPackage, source *repopolicy.ForeignSource, physical string) error {
	module := pkg.Module
	if module.Version != source.Version || module.Replace == nil {
		return fmt.Errorf("%w: foreign compiler version/replacement differs: %s", errGate, pkg.ImportPath)
	}

	if module.Replace.Version != "" || filepath.Clean(module.Dir) != physical || filepath.Clean(module.Replace.Dir) != physical {
		return fmt.Errorf("%w: foreign compiler module escaped replacement: %s (%s)", errGate, pkg.ImportPath, module.Dir)
	}

	name, _, _ := strings.Cut(pkg.ImportPath, " [")
	if pkg.Name == foreignTestMain {
		name = strings.TrimSuffix(name, foreignTestSuffix)
	}

	if name != source.Module && !strings.HasPrefix(name, source.Module+"/") {
		return fmt.Errorf("%w: foreign compiler import path escaped declared module", errGate)
	}

	relative, err := filepath.Rel(physical, pkg.Dir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: foreign package directory escaped declared root", errGate)
	}

	return nil
}

func checkSelectedForeignFiles(pkg *discoveredPackage, descriptor map[string]any, canonical, physical string) error {
	rel, err := filepath.Rel(physical, pkg.Dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: foreign package directory escaped: %s", errGate, pkg.Dir)
	}

	for _, name := range selectedForeignCompilerFiles(descriptor) {
		if filepath.IsAbs(name) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(name))) != name || name == ".." ||
			strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%w: unsafe foreign compiler input: %s", errGate, name)
		}

		canonicalInput := filepath.Join(canonical, rel, name)

		physicalInput := filepath.Join(physical, rel, name)
		if inputErr := equalRegularCompilerInput(canonicalInput, physicalInput); inputErr != nil {
			return inputErr
		}
	}

	return nil
}

func (stage *foreignTestStage) validateSyntheticTestMain(pkg *discoveredPackage) error {
	if len(pkg.GoFiles) != 1 || !filepath.IsAbs(pkg.GoFiles[0]) ||
		!strings.HasPrefix(pkg.GoFiles[0], stage.goCache+string(filepath.Separator)) {
		return fmt.Errorf("%w: unexpected synthetic test-main inputs: %s", errGate, pkg.ImportPath)
	}

	info, err := os.Lstat(pkg.GoFiles[0])
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: nonregular generated test main", errGate)
	}

	data, err := os.ReadFile(pkg.GoFiles[0])
	if err != nil {
		return fmt.Errorf("read generated test main: %w", err)
	}

	if !strings.HasPrefix(strings.TrimSpace(string(data)), "// Code generated by 'go test'. DO NOT EDIT.") {
		return fmt.Errorf("%w: unexpected generated test main content", errGate)
	}

	return nil
}

func equalRegularCompilerInput(canonical, physical string) error {
	for _, name := range []string{canonical, physical} {
		info, err := os.Lstat(name)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("%w: selected foreign compiler input not canonical regular file: %s", errGate, name)
		}
	}

	left, err := readInRoot(filepath.Dir(canonical), filepath.Base(canonical))
	if err != nil {
		return fmt.Errorf("read canonical compiler input: %w", err)
	}

	right, err := readInRoot(filepath.Dir(physical), filepath.Base(physical))
	if err != nil {
		return fmt.Errorf("read staged compiler input: %w", err)
	}

	if !bytes.Equal(left, right) {
		return fmt.Errorf("%w: staged compiler bytes differ: %s", errGate, physical)
	}

	return nil
}

func (stage *foreignTestStage) compilerEquivalent(ctx context.Context, root string, patterns []string, options testOptions) error {
	canonical, _, err := nativeForeignGraph(ctx, root, patterns, stage, options, "")
	if err != nil {
		return err
	}

	staged, _, err := nativeForeignGraph(ctx, root, patterns, stage, options, stage.modfile)
	if err != nil {
		return err
	}

	if !maps.Equal(canonical, staged) {
		return fmt.Errorf("%w: staged native compiler/module/source/embed graph differs", errGate)
	}

	return nil
}

func selectedForeignCompilerFiles(descriptor map[string]any) []string {
	var files []string

	compilerFields := []string{
		"GoFiles",
		"CgoFiles",
		"TestGoFiles",
		"XTestGoFiles",
		"EmbedFiles",
		"TestEmbedFiles",
		"XTestEmbedFiles",
		"CFiles",
		"CXXFiles",
		"MFiles",
		"HFiles",
		"FFiles",
		"SFiles",
		"SwigFiles",
		"SwigCXXFiles",
		"SysoFiles",
	}
	for _, field := range compilerFields {
		if values, ok := descriptor[field].([]any); ok {
			for _, value := range values {
				if name, valid := value.(string); valid {
					files = append(files, name)
				}
			}
		}
	}

	return files
}

func (stage *foreignTestStage) compilerDescriptor(
	root string,
	descriptor map[string]any,
	modfile string,
) (*discoveredPackage, string, error) {
	data, err := json.Marshal(descriptor)
	if err != nil {
		return nil, "", fmt.Errorf("encode compiler descriptor: %w", err)
	}

	pkg := &discoveredPackage{}
	if operationErr := json.Unmarshal(data, pkg); operationErr != nil {
		return nil, "", fmt.Errorf("decode package identity: %w", operationErr)
	}

	incomplete, validIncomplete := descriptor["Incomplete"].(bool)
	if _, present := descriptor["Incomplete"]; present && !validIncomplete {
		return nil, "", fmt.Errorf("%w: invalid compiler completeness field", errGate)
	}

	if pkg.Error != nil || len(pkg.DepsErrors) > 0 || incomplete {
		return nil, "", fmt.Errorf("%w: erroneous native compiler graph: %s", errGate, pkg.ImportPath)
	}

	if operationErr := stage.checkCompilerInputs(root, pkg, descriptor, modfile); operationErr != nil {
		return nil, "", operationErr
	}
	// Cache freshness is not compiler selection and differs by physical tree.
	delete(descriptor, "Stale")
	delete(descriptor, "StaleReason")
	stage.normalizeDescriptor(root, descriptor)

	data, err = json.Marshal(descriptor)
	if err != nil {
		return nil, "", fmt.Errorf("encode normalized compiler graph: %w", err)
	}

	return pkg, string(data), nil
}

func (stage *foreignTestStage) isForeignTestPackage(pkg *discoveredPackage) bool {
	if pkg.Module == nil || pkg.ForTest != "" || pkg.Name == foreignTestMain && strings.HasSuffix(pkg.ImportPath, foreignTestSuffix) {
		return false
	}

	for index := range stage.sources {
		if stage.sources[index].Module == pkg.Module.Path {
			return true
		}
	}

	return false
}
