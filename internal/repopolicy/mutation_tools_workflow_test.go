// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type mutationToolStep struct {
	Uses string
	Run  string
	ID   string
	With map[string]string
	Env  map[string]string
}
type mutationToolJob struct {
	RunsOn   string `yaml:"runs-on"`
	Uses     string
	Needs    []string
	If       string
	Continue any `yaml:"continue-on-error"`
	With     map[string]string
	Env      map[string]string
	Strategy struct {
		Matrix struct {
			OS        []string
			Partition []string
			Include   []struct {
				OS       string
				Producer string
			}
		}
	}
	Steps []mutationToolStep
}

func mutationToolWorkflowContract(campaign, publisher []byte) error {
	var source, build struct{ Jobs map[string]mutationToolJob }
	if err := yaml.Unmarshal(campaign, &source); err != nil {
		return err
	}
	if err := yaml.Unmarshal(publisher, &build); err != nil {
		return err
	}
	consumer := source.Jobs["mutation"]
	includes := consumer.Strategy.Matrix.Include
	if len(includes) == 0 || len(consumer.Needs) != len(includes) || len(consumer.Strategy.Matrix.OS) != len(includes) || consumer.If != "" || consumer.Continue != nil {
		return fmt.Errorf("consumer can bypass native producer outcomes")
	}
	aggregate := source.Jobs["approve"]
	if aggregate.If != "always()" || len(aggregate.Needs) != len(source.Jobs)-1 {
		return fmt.Errorf("aggregate can omit workflow outcomes")
	}
	for name := range source.Jobs {
		if name != "approve" && !slices.Contains(aggregate.Needs, name) {
			return fmt.Errorf("aggregate omits %s", name)
		}
	}
	var approval mutationToolStep
	for _, step := range aggregate.Steps {
		if step.ID == "approve" {
			approval = step
		}
	}
	seen := map[string]bool{}
	for _, item := range includes {
		job, found := source.Jobs[item.Producer]
		if !found || seen[item.OS] || !slices.Contains(consumer.Strategy.Matrix.OS, item.OS) || job.Uses != "./.github/workflows/mutation-tools.yml" || job.With["os"] != item.OS || job.If != "" || job.Continue != nil || !slices.Contains(consumer.Needs, item.Producer) {
			return fmt.Errorf("producer %s is not mandatory native source", item.Producer)
		}
		seen[item.OS] = true
		suffix := strings.ToUpper(strings.TrimPrefix(item.Producer, "mutation-tools-"))
		for _, field := range []string{"RESULT", "SHA"} {
			variable := "TOOLS_" + suffix + "_" + field
			expected := "${{ needs." + item.Producer + ".result }}"
			if field == "SHA" {
				expected = "${{ needs." + item.Producer + ".outputs.source_sha }}"
			}
			if approval.Env[variable] != expected || !strings.Contains(approval.Run, `"$`+variable+`"`) {
				return fmt.Errorf("aggregate omits producer result/source")
			}
		}
	}
	selected, downloaded, verified := false, false, false
	for _, step := range consumer.Steps {
		if step.Uses == "./.github/actions/prepare" && step.With["tools"] != "" {
			return fmt.Errorf("consumer rebuilds prepared tools")
		}
		if step.ID == "tools" {
			selected = strings.Contains(step.Run, `[[ "$ARTIFACT_ID" =~ ^[1-9][0-9]*$ ]]`) && strings.Contains(step.Run, `test -n "$RECEIPT"`) && step.Env["ARTIFACT_ID"] == "${{ needs[matrix.producer].outputs.artifact_id }}" && step.Env["RECEIPT"] == "${{ needs[matrix.producer].outputs.receipt }}"
		}
		if strings.HasPrefix(step.Uses, "actions/download-artifact@") {
			if !selected || step.With["artifact-ids"] != "${{ steps.tools.outputs.artifact_id }}" || step.With["path"] != ".tools/bin/" || step.With["digest-mismatch"] != "error" || step.With["name"] != "" || step.With["github-token"] != "" || step.With["run-id"] != "" || step.With["repository"] != "" {
				return fmt.Errorf("artifact download is not exact same-run identity")
			}
			downloaded = true
		}
		if strings.Contains(step.Run, "qualitygate verify-mutation-tools") {
			verified = downloaded && step.Env["PDFCONCAT_MUTATION_TOOLS"] == "${{ steps.tools.outputs.receipt }}"
		}
		if strings.Contains(step.Run, "qualitygate mutation ") && !verified {
			return fmt.Errorf("native tool executes before trusted byte verification")
		}
	}
	if !verified {
		return fmt.Errorf("native handoff verification missing")
	}
	producer := build.Jobs["build"]
	if producer.If != "" || producer.Continue != nil || producer.RunsOn != "${{ inputs.os }}" {
		return fmt.Errorf("native producer may skip or mask failure")
	}
	installed, recorded, uploaded := false, false, false
	for _, step := range producer.Steps {
		if step.Uses == "./.github/actions/prepare" {
			installed = step.With["tools"] == "gremlins golangci-lint" && step.With["cache"] == "false"
		}
		if strings.Contains(step.Run, "qualitygate record-mutation-tools") {
			recorded = installed
		}
		if strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
			uploaded = recorded && step.With["path"] == ".tools/bin/" && step.With["include-hidden-files"] == "true" && step.With["if-no-files-found"] == "error"
		}
	}
	if !uploaded {
		return fmt.Errorf("native build/control/record publication chain incomplete")
	}
	return nil
}

func TestNativeMutationToolDeliveryRequiresEveryVerifiedProducer(t *testing.T) {
	t.Parallel()
	campaign, publisher := readRepoFile(t, ".github/workflows/campaigns.yml"), readRepoFile(t, ".github/workflows/mutation-tools.yml")
	if err := mutationToolWorkflowContract(campaign, publisher); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{
		{"needs: [mutation-tools-linux, mutation-tools-macos, mutation-tools-windows]", "needs: []"},
		{"producer: mutation-tools-linux", "producer: mutation-tools-windows"},
		{"artifact-ids: ${{ steps.tools.outputs.artifact_id }}", "name: arbitrary"},
		{`test -n "$RECEIPT"`, "true"},
		{`[[ "$ARTIFACT_ID" =~ ^[1-9][0-9]*$ ]]`, "true"},
		{"qualitygate verify-mutation-tools", "qualitygate unrelated"},
		{"TOOLS_LINUX_RESULT:", "OMITTED_RESULT:"},
	} {
		if !strings.Contains(string(campaign), pair[0]) {
			t.Fatalf("negative control anchor absent: %s", pair[0])
		}
		changed := []byte(strings.Replace(string(campaign), pair[0], pair[1], 1))
		if err := mutationToolWorkflowContract(changed, publisher); err == nil {
			t.Fatalf("incomplete native handoff accepted: %s", pair[0])
		}
	}
	changed := []byte(strings.Replace(string(publisher), "tools: gremlins golangci-lint", "tools: golangci-lint", 1))
	if err := mutationToolWorkflowContract(campaign, changed); err == nil {
		t.Fatal("producer without SDK controls accepted")
	}
}
