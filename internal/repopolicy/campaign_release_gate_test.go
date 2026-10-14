// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestReleaseCampaignApprovalRejectsIncompleteEvidence(t *testing.T) {
	var workflow struct {
		Jobs map[string]struct {
			If    string   `yaml:"if"`
			Needs []string `yaml:"needs"`
			Steps []struct {
				Run string            `yaml:"run"`
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(readRepoFile(t, ".github/workflows/campaigns.yml"), &workflow); err != nil {
		t.Fatal(err)
	}
	gate := workflow.Jobs["approve"]
	if gate.If != "always()" || len(gate.Needs) != len(workflow.Jobs)-1 || len(gate.Steps) != 1 {
		t.Fatalf("incomplete approval graph: %+v", gate)
	}
	roles := make(map[string]string, len(gate.Needs))
	for _, job := range gate.Needs {
		if _, found := workflow.Jobs[job]; !found || job == "approve" || roles[job] != "" {
			t.Fatalf("invalid or repeated approval dependency %q", job)
		}
		for name, expression := range gate.Steps[0].Env {
			if strings.Join(strings.Fields(expression), "") == "${{needs."+job+".result}}" && strings.HasSuffix(name, "_RESULT") {
				if roles[job] != "" {
					t.Fatalf("ambiguous result binding for %q", job)
				}
				roles[job] = strings.TrimSuffix(name, "_RESULT")
			}
		}
		identity := gate.Steps[0].Env[roles[job]+"_SHA"]
		if roles[job] == "" || strings.Join(strings.Fields(identity), "") != "${{needs."+job+".outputs.source_sha}}" {
			t.Fatalf("missing exact result/source binding for %q", job)
		}
	}
	script := gate.Steps[0].Run
	for _, rejected := range gate.Needs {
		for _, scenario := range []string{"success", "failure", "skipped", "cancelled", "timeout", "empty", "wrong-sha", "missing-artifact"} {
			t.Run(rejected+"/"+scenario, func(t *testing.T) {
				dir := t.TempDir()
				receipt := filepath.Join(dir, "output")
				command := exec.CommandContext(t.Context(), "bash", "-c", script)
				sha := strings.Repeat("a", 40)
				command.Env = append(command.Environ(), "GITHUB_SHA="+sha, "GITHUB_OUTPUT="+receipt)
				for job, name := range roles {
					result, identity := "success", sha
					if job == rejected && scenario != "success" {
						switch scenario {
						case "wrong-sha":
							identity = strings.Repeat("b", 40)
						case "empty":
							identity = ""
						case "timeout", "missing-artifact":
							result = "failure"
						default:
							result = scenario
						}
					}
					command.Env = append(command.Env, name+"_RESULT="+result, name+"_SHA="+identity)
				}
				output, err := command.CombinedOutput()
				if scenario == "success" {
					if err != nil {
						t.Fatalf("positive control: %v %s", err, output)
					}
					data, readErr := os.ReadFile(receipt)
					if readErr != nil || string(data) != "sha="+sha+"\n" {
						t.Fatalf("missing source receipt: %s %v", data, readErr)
					}
				} else {
					if err == nil {
						t.Fatal("incomplete evidence passed")
					}
					if _, statErr := os.Stat(receipt); !os.IsNotExist(statErr) {
						t.Fatalf("failed gate issued receipt: %v", statErr)
					}
				}
			})
		}
	}
}
