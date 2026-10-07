// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const sourceVariantFixture = "0.6.0+executor.fixture"

func TestSourceBuiltGremlinsRequiresExactNativeVariantAndModule(t *testing.T) {
	t.Parallel()

	for _, fault := range []string{"none", "wrong-version", "wrong-module"} {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()
			assertNativeGremlinsVariant(t, fault)
		})
	}
}

func assertNativeGremlinsVariant(t *testing.T, fault string) {
	t.Helper()
	dir := t.TempDir()
	module := gremlinsSourceModule
	version := sourceVariantFixture

	if fault == "wrong-module" {
		module = "example.org/wrongtool"
	}

	if fault == "wrong-version" {
		version = "unreviewed"
	}

	files := map[string]string{
		sourceModuleFile: "module " + module + "\n",
		"cmd/gremlins/main.go": "package main\nimport(\"fmt\";\"runtime\")\nvar version string\n" +
			"func main(){fmt.Printf(\"gremlins version %s %s/%s\\n\",version,runtime.GOOS,runtime.GOARCH)}\n",
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), privateMode); err != nil {
			t.Fatal(err)
		}
	}

	output := filepath.Join(dir, "gremlins"+executableSuffix(runtime.GOOS))

	buildErr := runSourceGo(
		t.Context(),
		dir,
		"build",
		"-trimpath",
		"-buildvcs=false",
		"-ldflags",
		"-X main.version="+version,
		"-o",
		output,
		"./cmd/gremlins",
	)
	if buildErr != nil {
		t.Fatal(buildErr)
	}

	err := verifyBuiltGremlins(t.Context(), output, sourceVariantFixture)
	if (err == nil) != (fault == "none") {
		t.Fatalf("fault=%s verification=%v", fault, err)
	}
}

func TestGremlinsRejectedSourceIdentityPreservesInstalledBinary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "gremlins"+executableSuffix(runtime.GOOS))
	original := []byte("existing owned executable")

	if writeErr := os.WriteFile(path, original, execMode); writeErr != nil {
		t.Fatal(writeErr)
	}

	if installErr := installGremlins(t.Context(), dir, map[string]string{}, dir); installErr == nil {
		t.Fatal("incomplete source identity accepted")
	}

	actual, readErr := readFile(dir, filepath.Base(path))
	if readErr != nil || !bytes.Equal(actual, original) {
		t.Fatalf("rejected installation changed existing binary: %q %v", actual, readErr)
	}

	files, listErr := os.ReadDir(dir)
	if listErr != nil || len(files) != 1 {
		t.Fatalf("rejected installation left staged resources: %v %v", files, listErr)
	}
}
