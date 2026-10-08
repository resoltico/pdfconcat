// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package doccontract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/exectest"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/plan"
)

type (
	// examplePages is the page total of a build summary; nil when unknown.
	examplePages struct {
		Total *int64 `json:"total_pages"`
	}

	// examplePublication says whether the PDF became visible.
	examplePublication struct {
		Published bool `json:"published"`
	}

	// exampleSummary is the part of the build summary the examples are judged by.
	exampleSummary struct {
		Counts      examplePages       `json:"counts"`
		Status      string             `json:"status"`
		Publication examplePublication `json:"publication"`
	}
)

const (
	examplesPattern = "examples/plans/*.json"
	defaultFont     = "internal/typeset/fontdata/NotoSans-Regular.ttf"
	exampleTimeout  = 2 * time.Minute
	minimumExample  = 4
)

func TestMain(m *testing.M) {
	m.Run() // the test runner exits with its result once TestMain returns

	exectest.Cleanup()
}

// openRoot opens dir as a root that is closed when the test ends.
func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		closeErr := root.Close()
		if closeErr != nil {
			t.Error(closeErr)
		}
	})

	return root
}

// TestExamplePlansBuild builds every plan in examples/plans with the real executable. Each example is copied
// into a temporary directory; the source PDFs and font files it names are created there (one-page fixture
// PDFs, and the embedded default font standing in for a font of your own); and the page count of the
// published PDF must equal the PDFs plus the generated pages the plan lists.
func TestExamplePlansBuild(t *testing.T) {
	t.Parallel()

	repository := openRoot(t, moduleRoot(t))

	examples, err := fs.Glob(repository.FS(), examplesPattern)
	if err != nil {
		t.Fatal(err)
	}

	if len(examples) < minimumExample {
		t.Fatalf("found %d plans matching %s, want at least %d: the examples have gone missing",
			len(examples), examplesPattern, minimumExample)
	}

	font, err := repository.ReadFile(defaultFont)
	if err != nil {
		t.Fatal(err)
	}

	executable := exectest.Build(t, "./cmd/pdfconcat")

	for _, example := range examples {
		text, readErr := repository.ReadFile(example)
		if readErr != nil {
			t.Fatal(readErr)
		}

		t.Run(path.Base(example), func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			named := filepath.Join(dir, path.Base(example))
			want := stageExample(t, openRoot(t, dir), named, text, font)

			assertBuilt(t, runExampleBuild(t, executable, dir, named), want)
		})
	}
}

// stageExample writes the plan, creates the sources and fonts it names inside the root, and returns the page
// total its items add up to.
func stageExample(t *testing.T, work *os.Root, named string, text, font []byte) int64 {
	t.Helper()

	err := work.WriteFile(filepath.Base(named), text, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	job, err := plan.Decode(t.Context(), plan.Input{Name: named, BaseDir: work.Name()}, bytes.NewReader(text))
	if err != nil {
		t.Fatalf("%s does not decode: %v", named, err)
	}

	flat, err := assembly.Flatten(job)
	if err != nil {
		t.Fatalf("%s does not flatten: %v", named, err)
	}

	for _, source := range flat.Files {
		writeExampleFile(t, work, source.Path, pdffixture.Plain(filepath.Base(source.Path)).Bytes())
	}

	for _, used := range flat.Fonts {
		writeExampleFile(t, work, used.Path, font)
	}

	var pages int64

	for _, contribution := range flat.Contributions {
		pages += contribution.Count
	}

	return pages
}

// assertBuilt requires a published PDF of exactly want pages.
func assertBuilt(t *testing.T, summary exampleSummary, want int64) {
	t.Helper()

	if summary.Status != "ok" || !summary.Publication.Published || summary.Counts.Total == nil {
		t.Fatalf("the example did not build: %+v", summary)
	}

	if *summary.Counts.Total != want {
		t.Errorf("the example published %d pages, its items add up to %d", *summary.Counts.Total, want)
	}
}

// writeExampleFile creates the file, and its directory, that an example names, inside the working root.
func writeExampleFile(t *testing.T, work *os.Root, absolute string, content []byte) {
	t.Helper()

	relative, err := filepath.Rel(work.Name(), absolute)
	if err != nil {
		t.Fatal(err)
	}

	err = work.MkdirAll(filepath.Dir(relative), 0o750)
	if err != nil {
		t.Fatal(err)
	}

	err = work.WriteFile(relative, content, 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

// runExampleBuild runs the executable on one named plan and decodes its summary.
func runExampleBuild(t *testing.T, executable, dir, named string) exampleSummary {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), exampleTimeout)
	defer cancel()

	command := exectest.Command(ctx, executable, "build", "--plan", named)
	command.Dir = dir

	var stdout bytes.Buffer

	command.Stdout = &stdout

	err := command.Run()
	if err != nil {
		t.Fatalf("pdfconcat build --plan %s: %v\n%s", named, err, stdout.String())
	}

	var summary exampleSummary

	err = json.Unmarshal(stdout.Bytes(), &summary)
	if err != nil {
		t.Fatalf("the summary is not JSON: %v\n%s", err, stdout.String())
	}

	return summary
}

// TestReadmePlanIsTheBasicExample keeps the plan shown in the README identical to examples/plans/basic.json,
// so the quickstart is the example that the build test runs.
func TestReadmePlanIsTheBasicExample(t *testing.T) {
	t.Parallel()

	blocks := fencedBlocks(readDocument(t, "README.md"), "json")
	if len(blocks) == 0 {
		t.Fatal("README.md has no json example")
	}

	var shown, example any

	err := json.Unmarshal([]byte(blocks[0]), &shown)
	if err != nil {
		t.Fatalf("the README plan is not JSON: %v", err)
	}

	err = json.Unmarshal([]byte(readText(t, "examples", "plans", "basic.json")), &example)
	if err != nil {
		t.Fatalf("examples/plans/basic.json is not JSON: %v", err)
	}

	if !reflect.DeepEqual(shown, example) {
		t.Error("the first json block of README.md differs from examples/plans/basic.json")
	}
}
