// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/plan"
)

func TestOutputRepairUsesActualDeclaration(t *testing.T) {
	t.Parallel()

	current := &pipeline{command: &cli.Command{Output: "target"}, env: Env{Arguments: []string{buildName, "--output=target"}}}

	location := current.outputLocation()
	if location == nil || location.ArgvIndex == nil || *location.ArgvIndex != 1 {
		t.Fatalf("long option declaration: %+v", location)
	}

	job, err := plan.Decode(
		t.Context(),
		plan.Input{Name: "located.json", BaseDir: t.TempDir()},
		strings.NewReader(`{"version":1,"output":"target.pdf","items":[{"blank":{"size":"A4"}}]}`),
	)
	if err != nil {
		t.Fatal(err)
	}

	current = &pipeline{job: job, command: &cli.Command{}}

	location = current.outputLocation()
	if location == nil || location.Pointer != "/output" || location.File != "located.json" {
		t.Fatalf("plan output declaration: %+v", location)
	}

	if (&pipeline{command: &cli.Command{}}).outputLocation() != nil {
		t.Fatal("fabricated output declaration")
	}
}
