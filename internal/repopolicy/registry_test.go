// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
package repopolicy_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	mutationEntry = rationaleComment + `  - id: mutation-example
    tool: mutation
    kind: equivalent-mutant
    path: internal/example/file.go
    operator: CONDITIONALS_BOUNDARY
    anchor: if count > 0 {
    column: 10
    statuses: [lived]
    retained_property: both sides behave identically
`
)

func TestLoadRegistryReportsMissingFile(t *testing.T) {
	t.Parallel()

	_, err := repopolicy.LoadRegistry(filepath.Join(t.TempDir(), "absent.yml"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v, want a not-exist error", err)
	}
}

func TestRegistryCannotAcceptUnjudgedMutationStatus(t *testing.T) {
	t.Parallel()

	content := registryHeader + mustReplace(t, mutationEntry, "statuses: [lived]", "statuses: [not-covered]")

	_, err := repopolicy.ParseRegistry([]byte(content))
	if !errors.Is(err, repopolicy.ErrRegistry) || !strings.Contains(err.Error(), `status "not-covered" is not one`) {
		t.Fatalf("unjudged registry acceptance allowed: %v", err)
	}
}
