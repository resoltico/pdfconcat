// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/report"
)

const backendPolicyOutput = "policy-output.pdf"

func TestCheckAndBuildRejectSecondLegacyDeclaration(t *testing.T) {
	t.Parallel()

	for _, names := range []string{"repeated", "colliding", "distinct"} {
		t.Run(names, func(t *testing.T) {
			t.Parallel()
			dir := workDir(t)

			secondPath := writeLegacySources(t, dir, names)
			for _, command := range []string{commandCheck, commandBuild} {
				res := execute(
					t.Context(),
					t,
					nativeInventoryApp(),
					dir,
					command,
					outputFlag,
					backendPolicyOutput,
					sourceA,
					secondPath,
					reportFlag,
					reportFile,
					overwriteFlag,
				)
				res.requireCode(t, 2, string(pdfengine.CodeLegacyDestsRepeated))
				saved := decodedInventoryReport(t, dir)

				got := saved.Diagnostics[0]
				if got.Location == nil || got.Location.ArgvIndex == nil || *got.Location.ArgvIndex != 4 ||
					got.Path != filepath.Join(dir, secondPath) {
					t.Fatalf("second occurrence provenance: %+v", got)
				}

				if _, err := os.Stat(filepath.Join(dir, backendPolicyOutput)); !os.IsNotExist(err) {
					t.Fatalf("refused job published output: %v", err)
				}
			}
			// Real backend positive control: the same source is supported once.
			execute(t.Context(), t, nativeInventoryApp(), dir, commandBuild, outputFlag, backendPolicyOutput, sourceA).requireCode(t, 0, "")
		})
	}
}

// writeLegacySources constructs colliding and distinct-name catalogs at the real PDF boundary.
func writeLegacySources(t *testing.T, dir, names string) string {
	t.Helper()

	if err := pdffixture.LegacyDests("FIRST").WriteFile(filepath.Join(dir, sourceA)); err != nil {
		t.Fatal(err)
	}

	if names == "repeated" {
		return sourceA
	}

	second := pdffixture.LegacyDests("SECOND")
	if names == "distinct" {
		// Change dictionary keys and link references before serializing cross-reference offsets.
		for index, object := range second.Objs {
			second.Objs[index] = bytes.ReplaceAll(object, []byte("/chap"), []byte("/other"))
		}
	}

	if err := second.WriteFile(filepath.Join(dir, sourceB)); err != nil {
		t.Fatal(err)
	}

	return sourceB
}

func TestBackendPageLimitPrecedesGeneratedRenderingAndMerge(t *testing.T) {
	t.Parallel()

	for _, command := range []string{commandCheck, commandBuild} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			dir := workDir(t)
			writePDF(t, dir, sourceA)
			fake := newFake(t)
			fake.inspect = func(ctx context.Context, path string) (pdfengine.SourceInfo, error) {
				info, err := fake.realInspect(ctx, path)
				info.Pages = pdfengine.MaxOutputPages

				return info, err
			}
			fake.assemble = func(context.Context, *pdfengine.AssembleRequest) error {
				t.Fatal("over-cap instructions reached merge")
				return nil
			}
			plan := `{"version":1,"items":["a.pdf",{"blank":{}},{"blank":{}}]}`
			res := execute(t.Context(), t, appOf(fake), dir, command, inlinePlanFlag, plan, outputFlag, backendPolicyOutput)
			res.requireCode(t, 2, "assemble_request_invalid")

			var summary struct {
				Diagnostics []report.Diagnostic `json:"diagnostics"`
			}
			if err := json.Unmarshal([]byte(res.stdout), &summary); err != nil {
				t.Fatal(err)
			}

			if len(summary.Diagnostics) != 1 || summary.Diagnostics[0].Location == nil ||
				summary.Diagnostics[0].Location.Pointer != "/items/1" {
				t.Fatalf("crossing contribution: %+v", summary.Diagnostics)
			}
		})
	}
}
