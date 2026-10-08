// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/exectest"
	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	linterValidFixture     = "valid"
	linterChangedFixture   = "changed"
	linterFixtureBuildVerb = "build"
)

func TestLinterIdentityRejectsSpoofedVersionAndProvenance(t *testing.T) {
	t.Parallel()

	for _, fault := range []string{linterValidFixture, "version", "commit", "date", "module", "main"} {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()
			assertLinterIdentity(t, fault)
		})
	}
}

func assertLinterIdentity(t *testing.T, fault string) {
	t.Helper()

	expected := repopolicy.SourceBuildIdentity{Version: "reviewed", Commit: "reviewed-patch", Date: "reviewed-source"}
	module := linterModule
	main := "cmd/golangci-lint"
	actual := expected
	applyLinterIdentityFault(fault, &module, &main, &actual)

	dir := t.TempDir()

	source := "package main\nimport \"fmt\"\nvar version,commit,date string\n" +
		"func main(){fmt.Printf(\"{\\\"version\\\":%q,\\\"commit\\\":%q,\\\"date\\\":%q}\\n\",version,commit,date)}\n"
	for name, content := range map[string]string{"go.mod": "module " + module + "\n", main + "/main.go": source} {
		file := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), dirMode); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(file, []byte(content), privateMode); err != nil {
			t.Fatal(err)
		}
	}

	binary := filepath.Join(dir, "golangci-lint"+executableSuffix(runtime.GOOS))

	flags := "-X main.version=" + actual.Version + " -X main.commit=" + actual.Commit + " -X main.date=" + actual.Date
	if err := runSourceGo(t.Context(), dir, linterFixtureBuildVerb, "-ldflags", flags, "-o", binary, "./"+main); err != nil {
		t.Fatal(err)
	}

	if err := verifyBuiltLinter(t.Context(), binary, expected); (err == nil) != (fault == linterValidFixture) {
		t.Fatalf("fault=%s verification=%v", fault, err)
	}
}

func applyLinterIdentityFault(fault string, module, main *string, identity *repopolicy.SourceBuildIdentity) {
	switch fault {
	case "version":
		identity.Version = linterChangedFixture
	case "commit":
		identity.Commit = linterChangedFixture
	case "date":
		identity.Date = linterChangedFixture
	case "module":
		*module = "example.org/substitute"
	case "main":
		*main = "cmd/substitute"
	default:
	}
}

func TestLinterMetadataRejectsForeignTarget(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	main := filepath.Join(dir, "cmd", "golangci-lint")
	if err := os.MkdirAll(main, dirMode); err != nil {
		t.Fatal(err)
	}

	for file, content := range map[string]string{
		filepath.Join(dir, "go.mod"):   "module " + linterModule + "\n",
		filepath.Join(main, "main.go"): "package main\nfunc main(){}\n",
	} {
		if err := os.WriteFile(file, []byte(content), privateMode); err != nil {
			t.Fatal(err)
		}
	}

	foreign := "linux"
	if runtime.GOOS == foreign {
		foreign = windowsOS
	}

	binary := filepath.Join(dir, "foreign-tool")
	builder := exectest.Command(t.Context(), "go", linterFixtureBuildVerb, "-o", binary, "./cmd/golangci-lint")
	builder.Dir = dir

	builder.Env = append(os.Environ(), sourceBuildEnv()...)

	builder.Env = append(builder.Env, "GOOS="+foreign, "CGO_ENABLED=0")
	if output, err := builder.CombinedOutput(); err != nil {
		t.Fatalf("compile target control: %v\n%s", err, output)
	}

	if err := repopolicy.VerifyGolangciBinaryMetadata(binary); err == nil || !strings.Contains(err.Error(), "target differs") {
		t.Fatalf("foreign target did not fail its native target predicate: %v", err)
	}
}
