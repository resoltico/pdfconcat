// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan_test

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/plan"
	"github.com/resoltico/pdfconcat/internal/core/internal/result"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func resultSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	var value any
	if err := json.Unmarshal(result.Schema, &value); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("result.json", value); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("result.json")
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestEveryDocumentedJSONBlock(t *testing.T) {
	docs, err := filepath.Glob("../../../../docs/*.md")
	if err != nil {
		t.Fatal(err)
	}
	docs = append(docs, "../../../../README.md")
	schema := resultSchema(t)
	blocks := 0
	for _, path := range docs {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		chunks := strings.Split(string(data), "```json\n")
		for i, chunk := range chunks[1:] {
			block, _, ok := strings.Cut(chunk, "```")
			if !ok {
				t.Fatalf("unclosed JSON block in %s", path)
			}
			blocks++
			var fields map[string]any
			if err = json.Unmarshal([]byte(block), &fields); err != nil {
				t.Fatalf("%s JSON block %d: %v", path, i+1, err)
			}
			if _, isResult := fields["format_version"]; isResult {
				if err = schema.Validate(fields); err != nil {
					t.Fatalf("%s result block %d: %v", path, i+1, err)
				}
			} else {
				if _, ds := plan.Decode([]byte(block), path, filepath.Dir(path)); len(ds) > 0 {
					t.Fatalf("%s plan block %d: %+v", path, i+1, ds)
				}
			}
		}
	}
	if blocks < 5 {
		t.Fatalf("unexpectedly few documented JSON examples: %d", blocks)
	}
}

func TestDocumentedCodesEqualRegistry(t *testing.T) {
	data, err := os.ReadFile("../../../../docs/CLI.md")
	if err != nil {
		t.Fatal(err)
	}
	found := map[diag.Code]diag.Definition{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		columns := strings.Split(line, "|")
		if len(columns) != 6 {
			continue
		}
		severity := strings.TrimSpace(columns[2])
		if severity != "error" && severity != "warning" {
			continue
		}
		var code diag.Code
		if err := code.UnmarshalText([]byte(strings.Trim(strings.TrimSpace(columns[1]), "`"))); err != nil {
			t.Fatal(err)
		}
		class := strings.TrimSpace(columns[3])
		if class == "—" {
			class = ""
		}
		if _, duplicate := found[code]; duplicate {
			t.Fatalf("duplicate code row %s", code)
		}
		found[code] = diag.Definition{Severity: diag.Severity(severity), Class: diag.Class(class), Description: strings.TrimSpace(columns[4])}
	}
	registry := diag.Registry()
	if len(found) != len(registry) {
		t.Fatalf("documented codes=%d registry=%d", len(found), len(registry))
	}
	for code, definition := range registry {
		documented, ok := found[code]
		if !ok || documented.Severity != definition.Severity || documented.Class != definition.Class || documented.Description != strings.ReplaceAll(definition.Description, "|", " / ") {
			t.Fatalf("registry/table mismatch %s: %+v / %+v", code, definition, documented)
		}
	}
}

func TestNoticesMatchLinkedTargetModules(t *testing.T) {
	data, err := os.ReadFile("../../../../THIRD_PARTY_NOTICES.md")
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "| `") {
			columns := strings.Split(line, "|")
			name := strings.Trim(strings.TrimSpace(columns[1]), "`")
			if documented[name] {
				t.Fatalf("duplicate notice %s", name)
			}
			documented[name] = true
		}
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	linked := map[string]bool{}
	for _, target := range []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"} {
		goos, goarch, _ := strings.Cut(target, "/")
		command := exec.CommandContext(t.Context(), "go", "list", "-deps", "-f", "{{if .Module}}{{.Module.Path}}{{end}}", "./cmd/pdfconcat")
		command.Dir = root
		command.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("target %s modules: %v %s", target, err, output)
		}
		for _, module := range strings.Fields(string(output)) {
			if module != "github.com/resoltico/pdfconcat" {
				linked[module] = true
			}
		}
	}
	if len(linked) != len(documented) {
		t.Fatalf("linked/noticed module count %d/%d", len(linked), len(documented))
	}
	for module := range linked {
		if !documented[module] {
			t.Fatalf("missing notice %s", module)
		}
	}
}

func TestDocumentedPythonRecipeRuns(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	data, err := os.ReadFile("../../../../docs/CLI.md")
	if err != nil {
		t.Fatal(err)
	}
	_, recipe, ok := strings.Cut(string(data), "```python\n")
	if !ok {
		t.Fatal("missing Python recipe")
	}
	recipe, _, ok = strings.Cut(recipe, "```")
	if !ok {
		t.Fatal("unclosed Python recipe")
	}
	dir := t.TempDir()
	chapterDir := filepath.Join(dir, "chapters")
	if err = os.Mkdir(chapterDir, 0700); err != nil {
		t.Fatal(err)
	}
	names := []string{"chapter a.pdf", "Pielikums Ā.pdf"}
	if runtime.GOOS != "windows" {
		names = append(names, "quote\".pdf", "line\nbreak.pdf")
	}
	for _, name := range names {
		if err = pdffixture.Plain(name).WriteFile(filepath.Join(chapterDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	binDir := filepath.Join(dir, "bin")
	if err = os.Mkdir(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(binDir, "pdfconcat")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./cmd/pdfconcat")
	build.Dir = "../../../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("recipe binary: %v %s", err, output)
	}
	script := filepath.Join(dir, "recipe.py")
	if err = os.WriteFile(script, []byte(recipe), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), python, script)
	command.Dir = dir
	command.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err = command.Run(); err != nil {
		t.Fatalf("Python recipe: %v\n%s\n%s", err, stdout.String(), stderr.String())
	}
	generated, err := os.ReadFile(filepath.Join(dir, "job.json"))
	if err != nil {
		t.Fatal(err)
	}
	j, ds := plan.Decode(generated, "job.json", dir)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	slices.Sort(names)
	if len(j.Parts) != len(names) {
		t.Fatalf("recipe lost files: %s", generated)
	}
	for i, name := range names {
		if j.Parts[i].Source != filepath.Join(chapterDir, name) {
			t.Fatalf("recipe filename/order changed: %s", generated)
		}
	}
	var outcome result.Result
	if err = json.Unmarshal(stdout.Bytes(), &outcome); err != nil {
		t.Fatal(err)
	}
	if outcome.Status != "ok" || !outcome.Output.Published || *outcome.Pages.Total != int64(len(names)) {
		t.Fatalf("recipe outcome %+v", outcome)
	}
	if _, err = os.Stat(filepath.Join(dir, "book.pdf")); err != nil {
		t.Fatal(fmt.Errorf("recipe output: %w", err))
	}
}
