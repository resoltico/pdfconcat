// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"maps"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"
)

type (
	nativeWorkflowMatrix struct {
		OS []string `yaml:"os"`
	}

	nativeWorkflowStrategy struct {
		Matrix nativeWorkflowMatrix `yaml:"matrix"`
	}

	nativeWorkflowJob struct {
		Strategy nativeWorkflowStrategy `yaml:"strategy"`
	}
)

const nativeStaleJob = "lint-stale"

func TestNativeLintUsesEveryHostedNativeTestPlatform(t *testing.T) {
	t.Parallel()

	jobs := nativeWorkflowJobs(t)
	if problems := nativeLintPlatformProblems(jobs); len(problems) != 0 {
		t.Fatalf("native lint platform coverage differs from hosted tests: %v", problems)
	}

	for _, job := range []string{"lint", nativeStaleJob} {
		t.Run("missing target in "+job, func(t *testing.T) {
			t.Parallel()

			changed := maps.Clone(jobs)
			matrix := changed[job]
			matrix.Strategy.Matrix.OS = matrix.Strategy.Matrix.OS[1:]
			changed[job] = matrix

			if problems := nativeLintPlatformProblems(changed); len(problems) != 1 || problems[0] != job {
				t.Fatalf("missing native lint target escaped: %v", problems)
			}
		})
	}
}

func nativeWorkflowJobs(t *testing.T) map[string]nativeWorkflowJob {
	t.Helper()

	var document struct {
		Jobs map[string]nativeWorkflowJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(readRepoFile(t, ".github/workflows/ci.yml"), &document); err != nil {
		t.Fatal(err)
	}

	if len(document.Jobs["test"].Strategy.Matrix.OS) == 0 {
		t.Fatal("hosted native test platform selection is empty")
	}

	return document.Jobs
}

func nativeLintPlatformProblems(jobs map[string]nativeWorkflowJob) []string {
	want := slices.Clone(jobs["test"].Strategy.Matrix.OS)
	slices.Sort(want)

	var problems []string

	for _, name := range []string{"lint", nativeStaleJob} {
		got := slices.Clone(jobs[name].Strategy.Matrix.OS)
		slices.Sort(got)

		if !slices.Equal(got, want) {
			problems = append(problems, name)
		}
	}

	return problems
}
