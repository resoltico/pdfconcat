// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Command pdfconcat supplies process lifetime and standard streams to the CLI.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/resoltico/pdfconcat/internal/cli"
)

var version string

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() { <-ctx.Done(); stop() }()
	code := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, cli.Host{WorkingDir: os.Getwd, ReleaseOverride: version})
	stop()
	os.Exit(code)
}
