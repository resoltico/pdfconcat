// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

// Command pdfconcat supplies process lifetime and standard streams to the CLI.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"

	"github.com/resoltico/pdfconcat/internal/cli"
)

var version string

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() { <-ctx.Done(); stop() }()
	code := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, cli.Host{BuildMetadata: buildMetadata(), InspectionWorkers: runtime.GOMAXPROCS(0), SourceDateEpoch: func() (string, bool) { return os.LookupEnv("SOURCE_DATE_EPOCH") }, WorkingDir: os.Getwd, ReleaseOverride: version})
	stop()
	os.Exit(code)
}

func buildMetadata() cli.BuildMetadata {
	facts := cli.BuildMetadata{Go: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH}
	if info, ok := debug.ReadBuildInfo(); ok {
		facts.Version = info.Main.Version
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				facts.Commit = setting.Value
			}
		}
	}
	return facts
}
