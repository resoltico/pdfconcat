// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

type architectureControl struct {
	file     string
	source   string
	owner    string
	imported string
	target   string
}

const (
	productionAssemblyRule         = "production-assembly"
	assemblyControlPackage         = "assembly"
	planControlPackage             = "plan"
	architectureImportControlCount = 17
)

func architectureControls(ctx context.Context, root, binary string) ([]string, error) {
	scratch, createErr := os.MkdirTemp("", "qualitygate-architecture-")
	if createErr != nil {
		return nil, fmt.Errorf("create architecture controls: %w", createErr)
	}
	defer removeAll(scratch)

	if err := prepareArchitectureControls(root, scratch); err != nil {
		return nil, err
	}

	if err := architecturePositiveControls(ctx, scratch, binary); err != nil {
		return nil, err
	}

	var problems []string

	for _, control := range architectureImportControls() {
		detected, err := architectureImportControl(ctx, scratch, binary, control)
		if err != nil {
			return nil, err
		}

		if !detected {
			problems = append(problems, "import negative control escaped: "+control.owner+" -> "+control.imported)
		}
	}

	classification, err := architectureClassificationControls(ctx, scratch)
	if err != nil {
		return nil, err
	}

	loader, err := architectureLoaderControls(ctx, scratch)
	if err != nil {
		return nil, err
	}

	structure, structureErr := architectureStructureControls(ctx, scratch, binary)
	if structureErr != nil {
		return nil, structureErr
	}

	return append(append(append(problems, classification...), loader...), structure...), nil
}

func prepareArchitectureControls(root, scratch string) error {
	for _, file := range []string{moduleFileName, "go.sum", lintConfigFileName} {
		data, err := readInRoot(root, file)
		if err != nil {
			return err
		}

		if writeErr := writeArchitectureSource(scratch, file, string(data)); writeErr != nil {
			return writeErr
		}
	}

	for _, dir := range []string{
		assemblyControlPackage, "app", "capture", planControlPackage,
		"pdfengine", "exectest", "pdffixture", "assemblySibling", "repopolicy",
	} {
		source := "package " + dir + "\nfunc Value() int { return 1 }\n"
		if err := writeArchitectureSource(scratch, "internal/"+dir+"/stub.go", source); err != nil {
			return err
		}
	}

	if err := prepareArchitectureToolControls(scratch); err != nil {
		return err
	}

	// The sibling exists solely as an imported package; classify it temporarily with its own deliberate rule.
	config, configErr := readInRoot(scratch, lintConfigFileName)
	if configErr != nil {
		return configErr
	}

	needle := "        production-assembly:"
	sibling := `        production-control-sibling:
          list-mode: lax
          files: ["**/internal/assemblySibling/*.go", "!$test"]
          deny:
            - pkg: github.com/resoltico/pdfconcat
              desc: control sibling has no internal dependencies
`

	config = []byte(strings.Replace(string(config), needle, sibling+needle, 1))
	if err := writeArchitectureSource(scratch, lintConfigFileName, string(config)); err != nil {
		return err
	}

	valid := "package plan\nimport \"github.com/resoltico/pdfconcat/internal/assembly\"\nvar Checked = assembly.Value()\n"
	if err := writeArchitectureSource(scratch, "internal/plan/legal.go", valid); err != nil {
		return err
	}

	oracle := `package assembly_test
import ("testing";"github.com/resoltico/pdfconcat/internal/pdffixture")
func TestIndependentOracle(t *testing.T) { if pdffixture.Value()!=1 { t.Fatal("oracle") } }
`

	return writeArchitectureSource(scratch, "internal/assembly/oracle_test.go", oracle)
}

func prepareArchitectureToolControls(scratch string) error {
	for _, tool := range []string{"installtools", "qualitygate"} {
		imports := "import _ \"github.com/resoltico/pdfconcat/internal/repopolicy\"\n"
		if tool == "qualitygate" {
			imports += "import _ \"github.com/resoltico/pdfconcat/internal/capture\"\n"
		}

		if err := writeArchitectureSource(scratch, "tools/"+tool+"/stub.go", "package main\n"+imports+"func main(){}\n"); err != nil {
			return err
		}
	}

	return nil
}

func architectureImportControls() []architectureControl {
	module := "github.com/resoltico/pdfconcat/"

	controls := make([]architectureControl, 0, architectureImportControlCount)
	for _, item := range []struct{ dir, imported, owner string }{
		{assemblyControlPackage, module + "internal/app", productionAssemblyRule},
		{planControlPackage, module + "internal/exectest", "production-plan"},
		{"pdfengine", module + "internal/plan", "production-pdfengine"},
		{planControlPackage, module + "internal/assemblySibling", "production-plan"},
		{assemblyControlPackage, "github.com/pdfcpu/pdfcpu/pkg/api", "engine-boundary"},
		{assemblyControlPackage, "github.com/go-text/typesetting/shaping", "typesetting-boundary"},
		{assemblyControlPackage, "golang.org/x/text/unicode/bidi", "bidi-boundary"},
		{"capture", "golang.org/x/term", "terminal-boundary"},
		{assemblyControlPackage, "os", productionAssemblyRule},
		{assemblyControlPackage, "os/signal", productionAssemblyRule},
		{assemblyControlPackage, "log", productionAssemblyRule},
	} {
		controls = append(
			controls,
			architectureControl{
				file:     "internal/" + item.dir + "/violation.go",
				source:   "package " + item.dir + "\nimport _ \"" + item.imported + "\"\n",
				owner:    item.owner,
				imported: item.imported,
			},
		)
	}

	for _, target := range []string{"windows/amd64", "linux/amd64"} {
		goos, _, _ := strings.Cut(target, "/")
		controls = append(
			controls,
			architectureControl{
				file:     "internal/assembly/violation_" + goos + ".go",
				source:   "//go:build " + goos + "\n\npackage assembly\nimport _ \"" + module + "internal/app\"\n",
				owner:    productionAssemblyRule,
				imported: module + "internal/app",
				target:   target,
			},
		)
	}

	for _, target := range []string{"darwin/amd64", "darwin/arm64"} {
		_, arch, _ := strings.Cut(target, "/")
		controls = append(controls, architectureControl{
			file:   "internal/assembly/violation_" + arch + ".go",
			source: "//go:build " + arch + "\n\npackage assembly\nimport _ \"" + module + "internal/app\"\n",
			owner:  productionAssemblyRule, imported: module + "internal/app", target: target,
		})
	}

	return append(controls, architectureControl{
		file:   "internal/assembly/violation.go",
		source: "//line /tmp/unowned.go:1\npackage assembly\nimport _ \"" + module + "internal/app\"\n",
		owner:  productionAssemblyRule, imported: module + "internal/app",
	}, architectureControl{
		file:   "tools/installtools/violation.go",
		source: "package main\nimport _ \"" + module + "internal/capture\"\n",
		owner:  "production-installtools", imported: module + "internal/capture",
	})
}

func writeArchitectureSource(root, file, source string) error {
	target := filepath.Join(root, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
		return fmt.Errorf("create architecture fixture directory: %w", err)
	}

	if err := os.WriteFile(target, []byte(source), fileMode); err != nil {
		return fmt.Errorf("write architecture fixture: %w", err)
	}

	return nil
}

func architectureTargetEnv(target string) []string {
	env := []string{"CGO_ENABLED=0", readonlyGoFlags}

	if target != "" {
		parts := strings.Split(target, "/")
		env = append(env, "GOOS="+parts[0], "GOARCH="+parts[1])
	}

	return env
}

func compileArchitectureControl(ctx context.Context, root, target string) error {
	output, err := (&command{dir: root, name: goTool, args: []string{"build", allPackages}, env: architectureTargetEnv(target)}).output(ctx)
	if err != nil {
		return fmt.Errorf("architecture control did not compile for %s: %w\n%s", target, err, output)
	}

	return nil
}

func architectureControlIssues(ctx context.Context, root, binary, target string) ([]repopolicy.Issue, error, error) {
	return architectureAnalyzerIssues(ctx, root, binary, target, dependencyLinter)
}

func architectureAnalyzerIssues(ctx context.Context, root, binary, target, linters string) ([]repopolicy.Issue, error, error) {
	reportFile := filepath.Join(root, lintReportFileName)
	if err := prepareLintRunReport(root); err != nil {
		return nil, nil, err
	}

	output, runErr := (&command{
		dir: root, name: binary,
		args: []string{
			runVerb, serialLintRunners, lintNoFixFlag, configFlag, filepath.Join(root, lintConfigFileName), enableOnlyFlag, linters,
			"--output.json.path=" + reportFile, allPackages,
		},
		env: architectureTargetEnv(target),
	}).output(ctx)

	if runErr != nil && exitCode(runErr) != exitIssuesFound {
		return nil, runErr, fmt.Errorf("architecture checker infrastructure: %w\n%s", runErr, output)
	}

	data, err := readLintRunReport(root, runErr, output)
	if err != nil {
		return nil, runErr, err
	}

	issues, err := repopolicy.ParseIssues(data, root)

	return issues, runErr, err
}

func hasArchitectureIssue(issues []repopolicy.Issue, control architectureControl) bool {
	for _, issue := range issues {
		if issue.Linter == dependencyLinter && issue.File == control.file && strings.Contains(issue.Text, "'"+control.imported+"'") &&
			strings.Contains(issue.Text, "'"+control.owner+"'") {
			return true
		}
	}

	return false
}

func architectureClassificationControls(ctx context.Context, root string) ([]string, error) {
	var problems []string

	for _, control := range []struct{ file, source, want string }{
		{"internal/unclassified/new.go", "package unclassified\n", "unclassified production file"},
		{
			"internal/assembly/never.go", "//go:build architecture_never_supported\n\npackage assembly\n",
			"excluded from every supported target",
		},
	} {
		if err := writeArchitectureSource(root, control.file, control.source); err != nil {
			return nil, err
		}

		issues, err := architectureScope(ctx, root)
		if err != nil {
			return nil, err
		}

		if !strings.Contains(strings.Join(issues, "\n"), control.want) {
			problems = append(problems, "architecture classification control escaped: "+control.file)
		}

		removeAll(filepath.Join(root, control.file))
	}

	return problems, nil
}

func architecturePositiveControls(ctx context.Context, root, binary string) error {
	for _, target := range archiveTargets() {
		if err := compileArchitectureControl(ctx, root, target); err != nil {
			return err
		}
	}

	output, testErr := goCommand(root, testVerb, runSelectionFlag, "^TestIndependentOracle$", allPackages).output(ctx)
	if testErr != nil {
		return fmt.Errorf("independent test oracle control: %w\n%s", testErr, output)
	}

	issues, runErr, err := architectureControlIssues(ctx, root, binary, "")
	if err != nil {
		return err
	}

	if runErr != nil {
		return fmt.Errorf("valid architecture fixture rejected: %w", runErr)
	}

	if len(issues) > 0 {
		return fmt.Errorf("%w: valid architecture fixture rejected: %v", errGate, issues)
	}

	problems, err := architectureScope(ctx, root)
	if err != nil {
		return err
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w: valid architecture classification rejected: %v", errGate, problems)
	}

	return nil
}

func architectureImportControl(ctx context.Context, root, binary string, control architectureControl) (bool, error) {
	if err := writeArchitectureSource(root, control.file, control.source); err != nil {
		return false, err
	}
	defer removeAll(filepath.Join(root, control.file))

	if err := compileArchitectureControl(ctx, root, control.target); err != nil {
		return false, err
	}

	issues, runErr, err := architectureControlIssues(ctx, root, binary, control.target)
	if err != nil {
		return false, err
	}

	detected := runErr != nil && hasArchitectureIssue(issues, control)
	if detected {
		log.Printf(
			"architecture control: compiled %s (%s); rejected %s importing %s",
			control.file,
			control.target,
			control.owner,
			control.imported,
		)
	}

	return detected, nil
}

// architectureLoaderControls proves package-level errors and unavailable evidence cannot pass discovery.
func architectureLoaderControls(ctx context.Context, root string) ([]string, error) {
	var problems []string

	missingImport := "package assembly\nimport _ \"github.com/resoltico/pdfconcat/internal/architecture_missing_import\"\n"

	file := "internal/assembly/missing_import.go"
	if err := writeArchitectureSource(root, file, missingImport); err != nil {
		return nil, err
	}

	_, loadErr := architectureScope(ctx, root)
	removeAll(filepath.Join(root, file))

	if loadErr == nil {
		problems = append(problems, "package loader accepted a missing dependency")
	}

	output, commandErr := goCommand(root, goListVerb, "-e", jsonFlag, "./architecture_missing_directory").output(ctx)
	if commandErr != nil {
		return nil, fmt.Errorf("missing-path loader control infrastructure: %w", commandErr)
	}

	if err := collectArchitectureFiles(output, "github.com/resoltico/pdfconcat", map[string]bool{}); err == nil {
		problems = append(problems, "package loader accepted a missing directory")
	}

	availability, err := architectureAvailabilityControls(ctx, root)
	if err != nil {
		return nil, err
	}

	return append(problems, availability...), nil
}

func architectureAvailabilityControls(ctx context.Context, root string) ([]string, error) {
	var problems []string

	empty, createErr := os.MkdirTemp("", "qualitygate-architecture-empty-")
	if createErr != nil {
		return nil, fmt.Errorf("create empty architecture control: %w", createErr)
	}
	defer removeAll(empty)

	for _, name := range []string{moduleFileName, lintConfigFileName} {
		data, err := readInRoot(root, name)
		if err != nil {
			return nil, err
		}

		if writeErr := writeArchitectureSource(empty, name, string(data)); writeErr != nil {
			return nil, writeErr
		}
	}

	if _, err := architectureScope(ctx, empty); err == nil {
		problems = append(problems, "architecture accepted no package selections")
	}

	if _, err := architectureScope(ctx, filepath.Join(empty, "missing-root")); err == nil {
		problems = append(problems, "architecture accepted a missing repository root")
	}

	if err := writeArchitectureSource(empty, lintConfigFileName, "linters: ["); err != nil {
		return nil, err
	}

	if _, err := architectureScope(ctx, empty); err == nil {
		problems = append(problems, "architecture accepted a malformed configuration")
	}

	return problems, nil
}
