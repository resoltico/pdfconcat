// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	architectureModule      = "example.org/project"
	productionConfiguration = `linters:
  settings:
    depguard:
      rules:
        production-model:
          list-mode: lax
          files: ["**/internal/model/*.go", "!$test"]
          allow: ["example.org/project/internal/value$"]
          deny:
            - pkg: example.org/project
              desc: internal direction is explicit
`
)

func TestProductionClassificationComesOnlyFromNativeBoundaryRules(t *testing.T) {
	t.Parallel()

	owners, err := repopolicy.ProductionOwners([]byte(productionConfiguration), architectureModule)
	if err != nil {
		t.Fatal(err)
	}

	files := []string{
		"internal/model/value.go",
		"internal/model/oracle_test.go",
		"internal/modelSibling/new.go",
		"internal/model/tagged.go",
	}
	compiled := map[string]bool{files[0]: true, files[1]: true, files[2]: true}

	issues := repopolicy.ProductionClassificationIssues(owners, files, compiled)
	if len(issues) != 2 || !strings.Contains(strings.Join(issues, "\n"), "unclassified production file: "+files[2]) ||
		!strings.Contains(strings.Join(issues, "\n"), "excluded from every supported target: "+files[3]) {
		t.Fatalf("wrong classification: %v", issues)
	}

	issues = repopolicy.ProductionClassificationIssues(owners, []string{"hidden_test.go"}, nil)
	if len(issues) != 2 || !strings.Contains(strings.Join(issues, "\n"), "owned file is excluded") {
		t.Fatalf("test excluded from every supported target accepted: %v", issues)
	}

	issues = repopolicy.ProductionClassificationIssues(owners, []string{"only_test.go"}, map[string]bool{"only_test.go": true})
	if len(issues) != 1 || !strings.Contains(issues[0], "no owned production") {
		t.Fatalf("empty evidence accepted: %v", issues)
	}
}

func TestProductionClassificationRejectsWeakenedDeclarations(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ name, old, replacement string }{
		{"global dependency allowlist", "production-model:", "allowed-imports:"},
		{"missing project denial", "pkg: example.org/project", "pkg: other.org/module"},
		{"tests mixed with production", ", \"!$test\"", ""},
		{"internal prefix permission", "internal/value$", "internal/value"},
		{"standard override", "example.org/project/internal/value$", "$gostd"},
		{"unbounded file glob", "internal/model/*.go", "internal/**/*.go"},
		{"parent traversal", "internal/model/*.go", "../model/*.go"},
		{"unknown mode", "list-mode: lax", "list-mode: permissive"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := repopolicy.ProductionOwners(
				[]byte(strings.Replace(productionConfiguration, test.old, test.replacement, 1)),
				architectureModule,
			)
			if err == nil {
				t.Fatal("weakened classification accepted")
			}
		})
	}

	duplicate := productionConfiguration + `        production-other:
          list-mode: lax
          files: ["**/internal/model/*.go", "!$test"]
          deny: [{pkg: example.org/project}]
`

	_, duplicateErr := repopolicy.ProductionOwners([]byte(duplicate), architectureModule)
	if duplicateErr == nil || !strings.Contains(duplicateErr.Error(), "ambiguous") {
		t.Fatalf("duplicate ownership accepted: %v", duplicateErr)
	}

	for _, bad := range []string{"linters: [", "{}", productionConfiguration + "\n---\n{}\n"} {
		if _, err := repopolicy.ProductionOwners([]byte(bad), architectureModule); err == nil {
			t.Fatal("malformed/empty config accepted")
		}
	}
}
