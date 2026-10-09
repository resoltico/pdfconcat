// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestNativeProgressRejectsActuallyCompiledCOutsideFixture(t *testing.T) {
	t.Parallel()

	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}

	scratch := t.TempDir()

	config, err := readInRoot(root, lintConfigFileName)
	if err != nil {
		t.Fatal(err)
	}

	for file, content := range map[string]string{
		moduleFileName:     "module github.com/resoltico/pdfconcat\n\ngo " + strings.TrimPrefix(runtime.Version(), "go") + "\n",
		lintConfigFileName: string(config),
	} {
		if writeErr := writeArchitectureSource(scratch, file, content); writeErr != nil {
			t.Fatal(writeErr)
		}
	}

	control := nativeCImportControls()[0]
	if writeErr := writeArchitectureSource(scratch, control.file, control.source); writeErr != nil {
		t.Fatal(writeErr)
	}

	target := runtime.GOOS + "/" + runtime.GOARCH + "/" + nativeProgressTag
	if compileErr := compileArchitectureControl(t.Context(), scratch, target); compileErr != nil {
		t.Fatal(compileErr)
	}

	detected, err := nativeCImportControl(scratch, control)
	if err != nil || !detected {
		t.Fatalf("actual C import escaped source boundary: detected=%v err=%v", detected, err)
	}
}
