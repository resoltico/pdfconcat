// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"context"
	"fmt"
	"log"
	"runtime"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func buildLinterSource(ctx context.Context, sourceDir, output string, identity repopolicy.SourceBuildIdentity) error {
	dependencies, err := snapshotSourceDependencies(sourceDir)
	if err != nil {
		return err
	}

	for _, args := range [][]string{{sourceModVerb, sourceDownloadVerb}, {sourceModVerb, "verify"}, linterParserTestArgs()} {
		if stepErr := runSourceGo(ctx, sourceDir, args...); stepErr != nil {
			return stepErr
		}

		if verifyErr := dependencies.verify(sourceDir); verifyErr != nil {
			return verifyErr
		}
	}

	flags := fmt.Sprintf("-X main.version=%s -X main.commit=%s -X main.date=%s", identity.Version, identity.Commit, identity.Date)

	buildErr := runSourceGo(
		ctx,
		sourceDir,
		"build",
		"-trimpath",
		"-buildvcs=false",
		"-ldflags",
		flags,
		"-o",
		output,
		"./cmd/golangci-lint",
	)
	if buildErr != nil {
		return buildErr
	}

	return dependencies.verify(sourceDir)
}

// The Windows arm64 SDK has no race detector; ordinary parser tests remain mandatory there.
func linterParserTestArgs() []string {
	if runtime.GOOS == windowsOS && runtime.GOARCH == arm64Arch {
		log.Print("physical parser unit tests run without race instrumentation: Go does not support the Windows/arm64 race detector")
		return []string{sourceTestVerb, "./internal/physicalparse"}
	}

	return []string{sourceTestVerb, "-race", "./internal/physicalparse"}
}
