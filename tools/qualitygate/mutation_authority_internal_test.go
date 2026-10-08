// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func TestMutationUsesFrozenSourceAuthorityAfterLookupRootDrift(t *testing.T) {
	t.Parallel()

	root, rootErr := repoRoot()
	if rootErr != nil {
		t.Fatal(rootErr)
	}

	versions, versionsErr := readToolVersions(root)
	if versionsErr != nil {
		t.Fatal(versionsErr)
	}

	identity, identityErr := repopolicy.GremlinsBuildIdentity(versions)
	if identityErr != nil {
		t.Fatal(identityErr)
	}

	lookup := snapshotFixture(t, map[string]string{
		moduleFileName: "module " + repopolicy.GremlinsModule + "\ngo 1.27.1\n",
		"cmd/gremlins/main.go": fmt.Sprintf("package main\nimport(\"fmt\";\"runtime\")\n"+
			"func main(){fmt.Println(\"gremlins version %s \"+runtime.GOOS+\"/\"+runtime.GOARCH)}\n", identity.Version),
	})

	name := "gremlins"
	if runtime.GOOS == windowsOS {
		name += exeSuffix
	}

	binary := filepath.Join(lookup, ".tools", "bin", name)
	build := &command{dir: lookup, name: goTool, args: []string{"build", "-o", binary, "./cmd/gremlins"}}

	if buildErr := build.run(t.Context()); buildErr != nil {
		t.Fatal(buildErr)
	}

	snapshot := frozenMutationAuthorityFixture(t, root)

	writeMutationAuthorityFixture(t, lookup, toolVersionsFileName, []byte("broken pins"))
	writeMutationAuthorityFixture(t, lookup, registryName, []byte("broken registry"))

	if selected, selectErr := gremlinsBinary(t.Context(), lookup, snapshot); selectErr != nil || selected != binary {
		t.Fatalf("lookup-root edits changed frozen tool authority: %s %v", selected, selectErr)
	}

	verifyFrozenMutationAuthority(t, snapshot)
	writeMutationAuthorityFixture(t, snapshot, "tools/mutation-patches/gremlins-executor.patch", []byte("changed patch"))

	if _, selectErr := gremlinsBinary(t.Context(), lookup, snapshot); selectErr == nil {
		t.Fatal("snapshot patch no longer matches its pin")
	}
}

func frozenMutationAuthorityFixture(t *testing.T, root string) string {
	t.Helper()

	snapshot := t.TempDir()

	for _, file := range []string{toolVersionsFileName, "tools/mutation-patches/gremlins-executor.patch"} {
		data, readErr := readInRoot(root, file)
		if readErr != nil {
			t.Fatal(readErr)
		}

		writeMutationAuthorityFixture(t, snapshot, file, data)
	}

	writeMutationAuthorityFixture(t, snapshot, moduleFileName, []byte("module frozen.source\ngo 1.27.1\n"))
	writeMutationAuthorityFixture(t, snapshot, registryName, []byte("version: 1\ncoverage_threshold_percent: 100\nexceptions: []\n"))

	return snapshot
}

func writeMutationAuthorityFixture(t *testing.T, root, name string, data []byte) {
	t.Helper()

	file := filepath.Join(root, filepath.FromSlash(name))
	if dirErr := os.MkdirAll(filepath.Dir(file), dirMode); dirErr != nil {
		t.Fatal(dirErr)
	}

	if writeErr := os.WriteFile(file, data, fileMode); writeErr != nil {
		t.Fatal(writeErr)
	}
}

func verifyFrozenMutationAuthority(t *testing.T, snapshot string) {
	t.Helper()

	if module, moduleErr := modulePath(snapshot); moduleErr != nil || module != "frozen.source" {
		t.Fatalf("wrong frozen module: %s %v", module, moduleErr)
	}

	if _, registryErr := repopolicy.LoadRegistry(filepath.Join(snapshot, registryName)); registryErr != nil {
		t.Fatal(registryErr)
	}
}
