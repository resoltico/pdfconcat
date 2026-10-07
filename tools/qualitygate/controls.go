// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

type (
	// controlSet is tools/mutation-controls.yml: deliberate defects the test suite must detect.
	controlSet struct {
		Controls []control `yaml:"controls"`
		Version  int       `yaml:"version"`
	}

	// control is one text-anchored deliberate defect.
	control struct {
		// ID names the property under test.
		ID string `yaml:"id"`
		// Package is the go test target that must fail once the defect is applied.
		Package string `yaml:"package"`
		// File is the source file the defect is applied to.
		File string `yaml:"file"`
		// Anchor is the trimmed source line replaced; it must occur exactly once in File.
		Anchor string `yaml:"anchor"`
		// Replacement is the line that takes its place, indentation preserved.
		Replacement string `yaml:"replacement"`
	}
)

const controlsFile = "tools/mutation-controls.yml"

var failureMarker = regexp.MustCompile(`(?m)^(--- FAIL|panic:|FAIL\s)`)

func loadControls(root string) (*controlSet, error) {
	content, err := readInRoot(root, controlsFile)
	if err != nil {
		return nil, err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)

	var set controlSet

	err = decoder.Decode(&set)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", controlsFile, err)
	}

	var trailing any
	if endErr := decoder.Decode(&trailing); endErr == nil {
		return nil, fmt.Errorf("%w: controls need exactly one YAML document", errGate)
	} else if !errors.Is(endErr, io.EOF) {
		return nil, fmt.Errorf("%w: controls require EOF: %w", errGate, endErr)
	}

	if validationErr := validateControls(&set); validationErr != nil {
		return nil, validationErr
	}

	return &set, nil
}

// runControls applies each deliberate defect to a snapshot, one at a time, and requires the package's
// tests to fail. A defect that still compiles and still passes is a hole in the tests; an anchor
// that no longer matches the code, or a defect that does not compile, is a broken control and is
// reported as such instead of being counted as detection.
func runControls(ctx context.Context, args []string) error {
	err := newFlags(controlsCommand).Parse(args)
	if err != nil {
		return fmt.Errorf(parseFlagsError, err)
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	set, err := loadControls(root)
	if err != nil {
		return err
	}

	snapshot, err := snapshotTree(ctx, root)
	if err != nil {
		return err
	}

	defer removeAll(snapshot)

	var problems []string

	baseline := map[string]bool{}

	for _, item := range set.Controls {
		problem := runControl(ctx, snapshot, &item, baseline)
		if problem != "" {
			problems = append(problems, item.ID+": "+problem)

			continue
		}

		log.Printf("controls: %s: detected (package %s failed as it must)", item.ID, item.Package)
	}

	return report(controlsCommand, problems, fmt.Sprintf("all %d deliberate defects were detected", len(set.Controls)))
}

// runControl returns a description of what went wrong, or "" when the defect was detected.
func runControl(ctx context.Context, snapshot string, item *control, baseline map[string]bool) string {
	if !baseline[item.Package] {
		out, err := (&command{dir: snapshot, name: goTool, args: []string{testVerb, countOnce, item.Package}}).output(ctx)
		if err != nil {
			return fmt.Sprintf(
				"the package's tests fail before the defect is applied, so detection cannot be attributed: %v\n%s",
				err,
				indent(out),
			)
		}

		baseline[item.Package] = true
	}

	target := filepath.Join(snapshot, filepath.FromSlash(item.File))

	original, err := readInRoot(snapshot, item.File)
	if err != nil {
		return err.Error()
	}

	mutated, err := replaceAnchoredLine(item.File, original, item.Anchor, item.Replacement)
	if err != nil {
		return err.Error()
	}

	err = os.WriteFile(target, mutated, fileMode)
	if err != nil {
		return fmt.Sprintf("write %s: %v", item.File, err)
	}

	defer restore(target, original)

	_, err = goCommand(snapshot, "build", allPackages).output(ctx)
	if err != nil {
		return "the deliberate defect does not compile, so it proves nothing:\n" + indent(err.Error())
	}

	out, err := (&command{dir: snapshot, name: goTool, args: []string{testVerb, countOnce, item.Package}}).output(ctx)
	if err == nil {
		return fmt.Sprintf("NOT DETECTED: the tests of %s still pass with the defect applied in %s", item.Package, item.File)
	}

	if !failureMarker.MatchString(out) && !strings.Contains(err.Error(), "FAIL") {
		return "go test failed without reporting a test failure, so detection is not established:\n" + indent(out)
	}

	return ""
}

// restore puts a file's original content back after a control.
func restore(target string, original []byte) {
	err := os.WriteFile(target, original, fileMode)
	if err != nil {
		log.Printf("controls: cannot restore %s: %v", target, err)
	}
}

// validateControls requires a nonempty set of complete deliberate mutations.
func validateControls(set *controlSet) error {
	if set.Version != 1 || len(set.Controls) == 0 {
		return fmt.Errorf("%w: %s needs version 1 and at least one control", errGate, controlsFile)
	}

	for _, item := range set.Controls {
		if item.ID == "" || item.Package == "" || item.File == "" || item.Anchor == "" || item.Replacement == "" {
			return fmt.Errorf("%w: %s: control %q needs id, package, file, anchor and replacement", errGate, controlsFile, item.ID)
		}
	}

	return nil
}
