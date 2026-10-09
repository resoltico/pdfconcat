// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build unix

package scale

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

func resolveNativeCompiler(ctx context.Context) (NativeCompilerIdentity, error) {
	var identity NativeCompilerIdentity

	compiler, err := exec.LookPath("cc")
	if err != nil {
		return NativeCompilerIdentity{}, fmt.Errorf("native descriptor verification requires a C compiler: %w", err)
	}

	if runtime.GOOS == darwinPlatform {
		output, resolveErr := exectest.Command(ctx, "xcrun", "--find", "clang").Output()
		if resolveErr != nil {
			return NativeCompilerIdentity{}, fmt.Errorf("resolve native compiler: %w", resolveErr)
		}

		compiler = strings.TrimSpace(string(output))

		sdk, sdkErr := exectest.Command(ctx, "xcrun", "--show-sdk-path").Output()
		if sdkErr != nil {
			return NativeCompilerIdentity{}, fmt.Errorf("resolve native SDK: %w", sdkErr)
		}

		identity.SDK = strings.TrimSpace(string(sdk))
	}

	identity.CompilerPath, err = filepath.EvalSymlinks(compiler)
	if err != nil {
		return NativeCompilerIdentity{}, fmt.Errorf(descriptorPrerequisiteError, err)
	}

	identity.CompilerSHA256, err = fileDigest(identity.CompilerPath)
	if err != nil {
		return NativeCompilerIdentity{}, fmt.Errorf(descriptorPrerequisiteError, err)
	}

	version, err := exectest.Command(ctx, identity.CompilerPath, "--version").Output()
	if err != nil {
		return NativeCompilerIdentity{}, fmt.Errorf(descriptorPrerequisiteError, err)
	}

	identity.Compiler = strings.TrimSpace(string(version))

	return identity, nil
}
