// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"go.yaml.in/yaml/v3"
)

// RequireCIOutcomes derives mandatory jobs from CI's sole workflow and checks both its dependency
// inventory and real GitHub outcomes. A missing/skipped aggregate cannot certify its dependencies.
func RequireCIOutcomes(workflow, outcomes []byte) error {
	var source struct {
		Jobs map[string]struct {
			Needs    []string `yaml:"needs"`
			If       string   `yaml:"if"`
			Continue any      `yaml:"continue-on-error"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(workflow, &source); err != nil {
		return fmt.Errorf("read CI job inventory: %w", err)
	}
	aggregate, found := source.Jobs["required"]
	if !found || aggregate.If != "always()" {
		return fmt.Errorf("required CI aggregate must run with always()")
	}
	expected := slices.Sorted(maps.Keys(source.Jobs))
	expected = slices.DeleteFunc(expected, func(name string) bool { return name == "required" })
	if len(expected) == 0 {
		return fmt.Errorf("mandatory CI inventory is empty")
	}
	needs := slices.Clone(aggregate.Needs)
	slices.Sort(needs)
	if !slices.Equal(expected, needs) {
		return fmt.Errorf("required CI needs %v, workflow mandates %v", needs, expected)
	}
	for name, job := range source.Jobs {
		if name != "required" && job.If != "" {
			return fmt.Errorf("mandatory CI job %s may skip", name)
		}
		if job.Continue != nil && job.Continue != false {
			return fmt.Errorf("mandatory CI job %s may mask failure", name)
		}
	}
	var results map[string]struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(outcomes, &results); err != nil {
		return fmt.Errorf("read actual CI outcomes: %w", err)
	}
	actual := slices.Sorted(maps.Keys(results))
	if !slices.Equal(actual, expected) {
		return fmt.Errorf("actual CI outcomes %v do not cover mandatory jobs %v", actual, expected)
	}
	for _, name := range expected {
		if results[name].Result != "success" {
			return fmt.Errorf("mandatory CI job %s concluded %q", name, results[name].Result)
		}
	}
	return nil
}
