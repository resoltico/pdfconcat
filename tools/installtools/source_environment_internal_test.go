// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"encoding/json"
	"os"
	"runtime"
	"testing"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

func TestSourceInstallEnvironmentOverridesDisabledVerification(t *testing.T) {
	t.Parallel()

	binDir := t.TempDir()

	hostile := append(os.Environ(),
		"GOSUMDB=off", "GONOSUMDB=*", "GOPRIVATE=*", "GONOPROXY=*",
		"GOPROXY=off", "GOTOOLCHAIN=auto", "GOWORK=untrusted-workspace", "GOFLAGS=-tags=untrusted",
	)
	process := exectest.Command(t.Context(), "go", "env", "-json",
		"GOSUMDB", "GONOSUMDB", "GOPRIVATE", "GONOPROXY", "GOPROXY", "GOTOOLCHAIN", "GOWORK", "GOFLAGS", "GOBIN", "GOOS", "GOARCH")

	hostile = append(hostile, sourceInstallEnv(binDir)...)
	process.Env = hostile

	output, err := process.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect real Go install environment: %v\n%s", err, output)
	}

	var actual map[string]string
	if err = json.Unmarshal(output, &actual); err != nil {
		t.Fatal(err)
	}

	for key, want := range map[string]string{
		"GOSUMDB": "sum.golang.org", "GONOSUMDB": "", "GOPRIVATE": "", "GONOPROXY": "",
		"GOPROXY": "https://proxy.golang.org", "GOTOOLCHAIN": "local", "GOWORK": "off",
		"GOFLAGS": "-mod=mod", "GOBIN": binDir, "GOOS": runtime.GOOS, "GOARCH": runtime.GOARCH,
	} {
		if actual[key] != want {
			t.Fatalf("Go install setting %s=%q, want %q", key, actual[key], want)
		}
	}
}
