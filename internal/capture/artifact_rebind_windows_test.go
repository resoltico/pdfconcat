// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/capture"
)

func TestRetireReportRejectsInvalidNativePathWithoutChangingClaims(t *testing.T) {
	t.Parallel()

	registry := capture.NewRegistry()

	output := filepath.Join(t.TempDir(), "reserved-output.pdf")
	if _, err := registry.Add(capture.RoleOutput, output); err != nil {
		t.Fatal(err)
	}

	if err := registry.RetireReport("invalid\x00report"); err == nil {
		t.Fatal("unrepresentable native report path accepted")
	}

	_, err := registry.Add(capture.RoleReport, output)

	var alias *capture.AliasError
	if !errors.As(err, &alias) || alias.OtherRole != capture.RoleOutput {
		t.Fatalf("invalid retirement changed prior claim: %v", err)
	}
}

func TestArtifactRejectsNativePathFailuresWithoutChangingClaims(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	registry := capture.NewRegistry()

	output := filepath.Join(dir, "promised.pdf")
	if _, err := registry.Add(capture.RoleOutput, output); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"invalid\x00artifact", filepath.Join(dir, strings.Repeat("x", 256))} {
		_, err := registry.Add(capture.RoleReport, path)

		failure, isSource := errors.AsType[*capture.SourceError](err)
		if !isSource || failure.Path != path {
			t.Fatalf("invalid native path lost typed rejection: %v", err)
		}
	}

	_, err := registry.Add(capture.RoleReport, output)

	var alias *capture.AliasError
	if !errors.As(err, &alias) || alias.OtherRole != capture.RoleOutput {
		t.Fatalf("failed path registration changed earlier claim: %v", err)
	}
}
