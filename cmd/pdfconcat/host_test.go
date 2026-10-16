// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
package main

import (
	"context"
	"io"
	"os"

	"github.com/resoltico/pdfconcat/internal/cli"
)

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, stdin, stdout, stderr)
}
func runContext(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return cli.Run(ctx, args, stdin, stdout, stderr, cli.Host{WorkingDir: os.Getwd, ReleaseOverride: version})
}
