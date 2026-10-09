// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const goEnvVerb = "env"

func nativeCompilerReceipt(ctx context.Context, root, directory string) error {
	run := goCommand(root, goEnvVerb, jsonFlag, "GOVERSION", "GOOS", "GOARCH", "CC", "CGO_ENABLED")
	run.env = nativeProgressEnv()

	output, err := run.output(ctx)
	if err != nil {
		return fmt.Errorf("read native compiler configuration: %w", err)
	}

	var selected map[string]string
	if decodeErr := json.Unmarshal([]byte(output), &selected); decodeErr != nil {
		return fmt.Errorf("decode native compiler configuration: %w", decodeErr)
	}

	if selected["GOVERSION"] != runtime.Version() || selected["GOOS"] != runtime.GOOS || selected["GOARCH"] != runtime.GOARCH ||
		selected["CGO_ENABLED"] != "1" {
		return fmt.Errorf("%w: native fixture compiler differs from gate SDK or host", errGate)
	}

	compiler := strings.Fields(selected["CC"])
	if len(compiler) != 1 {
		return fmt.Errorf("%w: native fixture requires a directly selected C compiler executable", errGate)
	}

	version, err := (&command{dir: root, name: compiler[0], args: []string{"--version"}}).output(ctx)
	if err != nil {
		return fmt.Errorf("identify native C compiler: %w", err)
	}

	if strings.TrimSpace(version) == "" {
		return fmt.Errorf("%w: empty native C compiler identity", errGate)
	}

	content := output + "\nNative C compiler --version:\n" + version
	if writeErr := os.WriteFile(filepath.Join(directory, "compiler.txt"), []byte(content), fileMode); writeErr != nil {
		return fmt.Errorf("write native compiler receipt: %w", writeErr)
	}

	return nil
}
