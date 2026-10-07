// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestGremlinsSourceSelectionRetainsCriticalSubpackageControls(t *testing.T) {
	t.Parallel()

	for _, platform := range []struct{ os, arch string }{
		{darwinOS, arm64Arch},
		{darwinOS, amd64Arch},
		{linuxOS, arm64Arch},
		{linuxOS, amd64Arch},
		{windowsOS, amd64Arch},
		{windowsOS, arm64Arch},
	} {
		args := gremlinsSourceTestArgs(platform.os, platform.arch)
		for _, critical := range []string{
			"./internal/execution", "./internal/coverage", "./internal/engine",
			"./internal/engine/workerpool", "./internal/engine/workdir", "./internal/report",
		} {
			if !slices.Contains(args, critical) {
				t.Fatalf("%s/%s omitted %s controls: %v", platform.os, platform.arch, critical, args)
			}
		}

		wantRace := platform.os != windowsOS || platform.arch != arm64Arch
		if slices.Contains(args, "-race") != wantRace || args[0] != sourceTestVerb {
			t.Fatalf("%s/%s wrong native test mode: %v", platform.os, platform.arch, args)
		}
	}
}

func TestGremlinsSourceSelectionDetectsRealFailingWorkdirControl(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGremlinsSelectionFixture(t, dir)

	if buildErr := runSourceGo(t.Context(), dir, "build", "./..."); buildErr != nil {
		t.Fatalf("selection fixture must compile before testing rejection: %v", buildErr)
	}

	if testErr := runSourceGo(t.Context(), dir, gremlinsSourceTestArgs(runtime.GOOS, runtime.GOARCH)...); testErr == nil {
		t.Fatal("mandatory failing work-directory control was omitted")
	}
}

func writeGremlinsSelectionFixture(t *testing.T, dir string) {
	t.Helper()

	files := map[string]string{sourceModuleFile: "module example.org/selection\n"}
	for _, pkg := range []string{"execution", "coverage", "engine", "engine/workerpool", "engine/workdir", "report"} {
		files["internal/"+pkg+"/source.go"] = "package fixture\n"
	}

	files["internal/engine/workdir/control_test.go"] = `package fixture
import "testing"
func TestDescriptorControl(t *testing.T){t.Fatal("negative control")}
`
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if mkdirErr := os.MkdirAll(filepath.Dir(path), dirMode); mkdirErr != nil {
			t.Fatal(mkdirErr)
		}

		if writeErr := os.WriteFile(path, []byte(content), privateMode); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
}
