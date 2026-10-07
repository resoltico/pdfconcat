// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture_test

import (
	"errors"
	"path/filepath"
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
