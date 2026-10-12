// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func requireCompleteMutationPartitions(partitions []string) error {
	seen := map[int]bool{}
	for _, value := range partitions {
		left, right, found := strings.Cut(value, "/")
		index, indexErr := strconv.Atoi(left)
		count, countErr := strconv.Atoi(right)
		if !found || indexErr != nil || countErr != nil || count != len(partitions) || index < 0 || index >= count || seen[index] {
			return fmt.Errorf("missing, duplicate or inconsistent mutation partition: %v", partitions)
		}
		seen[index] = true
	}
	if len(seen) == 0 {
		return fmt.Errorf("empty mutation partitions")
	}
	return nil
}

func TestHostedMutationMatrixRequiresEveryPartitionWithFullIntegration(t *testing.T) {
	t.Parallel()
	var workflow struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Partition []string `yaml:"partition"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
			Steps []struct {
				Run  string            `yaml:"run"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(readRepoFile(t, ".github/workflows/campaigns.yml"), &workflow); err != nil {
		t.Fatal(err)
	}
	job := workflow.Jobs["mutation"]
	if err := requireCompleteMutationPartitions(job.Strategy.Matrix.Partition); err != nil {
		t.Fatal(err)
	}
	command, receipt := false, false
	for _, step := range job.Steps {
		if strings.Contains(step.Run, "qualitygate mutation") {
			command = strings.Contains(step.Run, "-partition=${{ matrix.partition }}") && !strings.Contains(step.Run, "-integration=false") && !strings.Contains(step.Run, "-only")
		}
		if strings.Contains(step.With["path"], "mutation-report.json.scope.json") {
			receipt = strings.Contains(step.With["name"], "${{ strategy.job-index }}")
		}
	}
	if !command || !receipt {
		t.Fatal("partition execution or unique source receipt upload is missing")
	}
	for _, invalid := range [][]string{nil, {"0/3", "1/3"}, {"0/2", "0/2"}, {"0/2", "2/2"}, {"0/1", "1/2"}} {
		if err := requireCompleteMutationPartitions(invalid); err == nil {
			t.Fatalf("incomplete matrix accepted: %v", invalid)
		}
	}
}
