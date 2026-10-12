// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
	"go.yaml.in/yaml/v3"
)

func TestRequiredCIAggregateRejectsEveryNonSuccessAndOmittedJob(t *testing.T) {
	t.Parallel()
	source := readRepoFile(t, ".github/workflows/ci.yml")
	var workflow struct {
		Jobs map[string]any `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(source, &workflow); err != nil {
		t.Fatal(err)
	}
	good := map[string]map[string]string{}
	for name := range workflow.Jobs {
		if name != "required" {
			good[name] = map[string]string{"result": "success"}
		}
	}
	serialize := func() []byte {
		data, err := json.Marshal(good)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if err := repopolicy.RequireCIOutcomes(source, serialize()); err != nil {
		t.Fatal(err)
	}
	for name := range good {
		for _, outcome := range []string{"failure", "cancelled", "skipped", "", "neutral"} {
			good[name]["result"] = outcome
			if err := repopolicy.RequireCIOutcomes(source, serialize()); err == nil {
				t.Fatalf("accepted %s=%s", name, outcome)
			}
		}
		good[name]["result"] = "success"
	}
	delete(good, "test")
	if err := repopolicy.RequireCIOutcomes(source, serialize()); err == nil {
		t.Fatal("missing matrix outcome accepted")
	}
	good["test"] = map[string]string{"result": "success"}
	extended := append(append([]byte{}, source...), []byte("\n  omitted-mandatory:\n    runs-on: ubuntu-latest\n    steps: []\n")...)
	if err := repopolicy.RequireCIOutcomes(extended, serialize()); err == nil {
		t.Fatal("omitted mandatory job accepted")
	}
	if err := repopolicy.RequireCIOutcomes([]byte(strings.Replace(string(source), "if: always()\n    needs:", "if: success()\n    needs:", 1)), serialize()); err == nil {
		t.Fatal("skippable aggregate accepted")
	}
}
