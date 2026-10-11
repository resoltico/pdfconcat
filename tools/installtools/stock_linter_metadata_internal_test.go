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

func TestLinterMetadataRejectsForeignTarget(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	main := filepath.Join(dir, "cmd", "golangci-lint")
	if err := os.MkdirAll(main, dirMode); err != nil {
		t.Fatal(err)
	}

	for file, content := range map[string]string{
		filepath.Join(dir, "go.mod"):   "module " + "github.com/golangci/golangci-lint/v2" + "\n",
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
	builder := exectest.Command(t.Context(), "go", "build", "-o", binary, "./cmd/golangci-lint")
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
