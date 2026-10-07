// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy_test

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type (
	coverageArtifactStep struct {
		With map[string]string `yaml:"with"`
		Uses string            `yaml:"uses"`
		If   string            `yaml:"if"`
	}
	coverageArtifactWorkflow struct {
		Jobs map[string]struct {
			Steps []coverageArtifactStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
)

func TestCoverageFailureRetainsDiagnosticProfile(t *testing.T) {
	t.Parallel()

	var workflow coverageArtifactWorkflow
	if err := yaml.Unmarshal(readRepoFile(t, ".github/workflows/ci.yml"), &workflow); err != nil {
		t.Fatal(err)
	}

	uploads := 0

	for _, step := range workflow.Jobs["coverage"].Steps {
		if !strings.HasPrefix(step.Uses, "actions/upload-artifact@") || !strings.Contains(step.With["path"], "coverage.out") {
			continue
		}

		uploads++
		// GitHub otherwise skips this step after a failed threshold, erasing the blocks needed for diagnosis.
		if step.If != "always()" {
			t.Fatalf("coverage profile upload must run after failure, got condition %q", step.If)
		}
	}

	if uploads != 1 {
		t.Fatalf("expected one diagnostic profile upload, discovered %d", uploads)
	}
}
