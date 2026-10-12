// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

func TestNativeHelperCoverageDirectoriesHaveIndependentOwnership(t *testing.T) {
	root := filepath.Join(t.TempDir(), "completed")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(exectest.EnvCoverDir, root)
	first := progressHelperArgs("-test.run=first")
	second := progressHelperArgs("-test.run=second")
	directory := func(args []string) string { return strings.TrimPrefix(args[len(args)-1], "-test.gocoverdir=") }
	left, right := directory(first), directory(second)
	if left == right || filepath.Dir(left) != root || filepath.Dir(right) != root {
		t.Fatal("parallel children share metadata ownership")
	}
	for _, path := range []string{left, right} {
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Fatalf("owned child directory absent: %s/%v", path, err)
		}
	}
	control, environment := progressControlArgs("-test.run=expected-kill")
	partial := directory(control)
	if filepath.Dir(filepath.Dir(partial)) != filepath.Dir(root) || filepath.Dir(partial) == root || environment != exectest.EnvCoverDir+"="+partial {
		t.Fatal("expected-kill instrumentation escaped attempt ownership or completed-profile separation")
	}
}
