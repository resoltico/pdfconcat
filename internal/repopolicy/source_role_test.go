// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const architectureModelGlob = "internal/model/*.go"

func TestNativeTestSupportRoleStillRequiresCompiledSource(t *testing.T) {
	t.Parallel()

	config := productionConfiguration + `        test-support-native:
          list-mode: lax
          files: ["**/internal/nativefixture/*.go", "!$test"]
          deny: [{pkg: example.org/project}]
`

	owners, err := repopolicy.SourceOwners([]byte(config), architectureModule)
	if err != nil {
		t.Fatal(err)
	}

	files := []string{"internal/model/value.go", "internal/nativefixture/native_darwin.go"}

	compiled := map[string]bool{files[0]: true, files[1]: true}
	if issues := repopolicy.SourceClassificationIssues(owners, files, compiled); len(issues) != 0 {
		t.Fatal(issues)
	}

	delete(compiled, files[1])

	issues := repopolicy.SourceClassificationIssues(owners, files, compiled)
	if len(issues) != 1 || !strings.Contains(issues[0], "excluded from every supported target") {
		t.Fatalf("native source silently excluded: %v", issues)
	}
}

func TestNativeTestSupportRoleRejectsBroadSelectorAndAmbiguousOwner(t *testing.T) {
	t.Parallel()

	config := productionConfiguration + `        test-support-native:
          list-mode: lax
          files: ["**/internal/**/*.go", "!$test"]
          deny: [{pkg: example.org/project}]
`
	if _, err := repopolicy.SourceOwners([]byte(config), architectureModule); err == nil {
		t.Fatal("broad test-support ownership accepted")
	}

	config = strings.Replace(config, "internal/**/*.go", architectureModelGlob, 1)
	if _, err := repopolicy.SourceOwners([]byte(config), architectureModule); err == nil {
		t.Fatal("ambiguous production/test-support ownership accepted")
	}
}
