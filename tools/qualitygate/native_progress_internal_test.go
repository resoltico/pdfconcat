// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"maps"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const nativeExecutionCountControl = "execution-count"

func TestNativeProgressProfileRejectsChangedInstrumentation(t *testing.T) {
	t.Parallel()

	baseline := &repopolicy.CoverageProfile{
		Mode:   "atomic",
		Blocks: []repopolicy.Block{{File: "app/progress.go", StartLine: 1, StartCol: 1, EndLine: 2, EndCol: 2, Stmts: 1}},
	}

	changes := map[string]func(*repopolicy.CoverageProfile){
		nativeExecutionCountControl: func(p *repopolicy.CoverageProfile) { p.Blocks[0].Count = 3 },
		"mode":                      func(p *repopolicy.CoverageProfile) { p.Mode = "set" },
		"file":                      func(p *repopolicy.CoverageProfile) { p.Blocks[0].File = "app/other.go" },
		"coordinate":                func(p *repopolicy.CoverageProfile) { p.Blocks[0].EndCol++ },
		"statements":                func(p *repopolicy.CoverageProfile) { p.Blocks[0].Stmts++ },
		"omitted-production-block":  func(p *repopolicy.CoverageProfile) { p.Blocks = nil },
		"additional-production-block": func(p *repopolicy.CoverageProfile) {
			p.Blocks = append(p.Blocks, repopolicy.Block{File: "extra.go", Stmts: 1})
		},
		"duplicate-production-block": func(p *repopolicy.CoverageProfile) { p.Blocks = append(p.Blocks, p.Blocks[0]) },
		"negative-execution-count":   func(p *repopolicy.CoverageProfile) { p.Blocks[0].Count = -1 },
	}
	for change, mutate := range changes {
		t.Run(change, func(t *testing.T) {
			t.Parallel()

			native := &repopolicy.CoverageProfile{Mode: baseline.Mode, Blocks: append([]repopolicy.Block(nil), baseline.Blocks...)}
			mutate(native)

			err := equivalentNativeProfile(baseline, native)
			if (err == nil) != (change == nativeExecutionCountControl) {
				t.Fatalf("instrumentation control %s: %v", change, err)
			}
		})
	}
}

func TestNativeProgressRejectsProductionFixtureDependency(t *testing.T) {
	t.Parallel()

	const module = "example.org/product"

	output := `{"ImportPath":"example.org/product/internal/progressnativefixture","Module":{"Path":"example.org/product"}}`
	if _, err := decodeProductSources(output, module); err == nil {
		t.Fatal("native fault fixture accepted in production closure")
	}
}

func TestNativeProgressRejectsMissingScenarioAndSkips(t *testing.T) {
	t.Parallel()

	const module = "example.org/product"

	outcome := newTestOutcome()

	outcome.completed[module+"/cmd/pdfconcat "+nativeProgressSignal+"/stimulus"] = actionPass
	if err := nativeProgressCases(outcome, module); err == nil {
		t.Fatal("missing retry accepted")
	}

	outcome.completed[module+"/cmd/pdfconcat "+nativeProgressSignal+"/retry"] = actionPass
	if err := nativeProgressCases(outcome, module); err != nil {
		t.Fatal(err)
	}

	outcome.skipped = []string{"skipped fixture"}
	if err := nativeProgressCases(outcome, module); err == nil {
		t.Fatal("skipped native fixture accepted")
	}
}

func TestNativeProgressPhysicalCImportBoundary(t *testing.T) {
	t.Parallel()

	owners := map[string]string{nativeProgressFixture: nativeProgressOwner}

	const source = "package fixture\n/* static int value(void){return 1;} */\nimport \"C\"\n"

	for _, file := range []string{
		nativeProgressFixture + "/signal_darwin.go", "internal/app/native.go",
		nativeProgressFixture + "Sibling/native.go",
	} {
		t.Run(file, func(t *testing.T) {
			t.Parallel()

			issue, err := nativeCSourceIssue(file, []byte(source), owners)
			if err != nil {
				t.Fatal(err)
			}

			if (issue == "") != (file == nativeProgressFixture+"/signal_darwin.go") {
				t.Fatalf("physical C import boundary failed: %s", issue)
			}
		})
	}

	issue, err := nativeCSourceIssue(nativeProgressFixture+"/signal_darwin.go", []byte(source), nil)
	if err != nil || issue == "" {
		t.Fatalf("unclassified C fixture accepted: %s %v", issue, err)
	}
}

func TestNativeProgressChildLayoutRejectsMissingDuplicateAndForeignData(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	sandbox := directory
	stimulus := filepath.Join(directory, "native-signal-stimulus-owned")
	retry := filepath.Join(directory, "native-signal-retry-owned")

	valid := map[string]bool{sandbox: true, stimulus: true, retry: true}
	if err := nativeCoverageLayout(directory, valid); err != nil {
		t.Fatal(err)
	}

	for _, change := range []string{"missing-retry-data", "duplicate-retry-data", "foreign-child-data", "nested-child-data"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()

			inputs := maps.Clone(valid)

			switch change {
			case "missing-retry-data":
				delete(inputs, retry)
			case "duplicate-retry-data":
				inputs[filepath.Join(directory, "native-signal-retry-second")] = true
			case "foreign-child-data":
				inputs[filepath.Join(directory, "unowned")] = true
			case "nested-child-data":
				inputs[filepath.Join(retry, "native-signal-retry-nested")] = true
			default:
				t.Fatal("unknown layout control")
			}

			if err := nativeCoverageLayout(directory, inputs); err == nil {
				t.Fatal("invalid child coverage credited")
			}
		})
	}
}

func TestNativeProgressRetryCounterRequiresActualSourceMatchedChildBlock(t *testing.T) {
	t.Parallel()

	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}

	content, err := readInRoot(root, nativeRetrySource)
	if err != nil {
		t.Fatal(err)
	}

	line, err := nativeRetryContinueLine(content)
	if err != nil {
		t.Fatal(err)
	}

	profile := &repopolicy.CoverageProfile{
		Mode:   "atomic",
		Blocks: []repopolicy.Block{{File: "example.org/project/" + nativeRetrySource, StartLine: line, EndLine: line, Stmts: 1, Count: 1}},
	}
	if counterErr := nativeRetryCounter(root, profile); counterErr != nil {
		t.Fatal(counterErr)
	}

	profile.Blocks[0].Count = 0
	if counterErr := nativeRetryCounter(root, profile); counterErr == nil {
		t.Fatal("zero retry child counter credited")
	}

	profile.Blocks[0].Count = 1

	profile.Blocks[0].File = "another-child.go"
	if counterErr := nativeRetryCounter(root, profile); counterErr == nil {
		t.Fatal("misattributed retry counter credited")
	}
}
