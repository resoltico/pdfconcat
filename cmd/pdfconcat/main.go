// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/diag"
	"github.com/resoltico/pdfconcat/internal/pipeline"
	"github.com/resoltico/pdfconcat/internal/plan"
	"github.com/resoltico/pdfconcat/internal/result"
)

var version string

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() { <-ctx.Done(); stop() }()
	code := runContext(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, stdin, stdout, stderr)
}

func runContext(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	command, ds := cli.Parse(args)
	if len(ds) > 0 {
		name := command.Name
		if name != "build" && name != "check" {
			name = ""
		}
		r := result.New(name)
		r.Diagnostics = ds
		if err := result.Render(stdout, r, command.Format); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		_, exit := diag.Outcome(ds)
		return exit
	}
	if command.Help {
		_, err := io.WriteString(stdout, cli.Help(command.Name))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if command.Name == "schema" {
		schema := plan.Schema
		if command.Schema == "result" {
			schema = result.Schema
		}
		_, err := stdout.Write(append(append([]byte{}, schema...), '\n'))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if command.Name == "version" {
		v := buildVersion()
		var err error
		if command.Format == "text" {
			_, err = fmt.Fprintf(stdout, "pdfconcat %s (%s, %s, %s)\n", v.Version, v.Commit, v.Go, v.Platform)
		} else {
			err = result.JSON(stdout, v)
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	cwd, err := os.Getwd()
	if err != nil {
		r := result.New(command.Name)
		r.Diagnostics = []diag.Diagnostic{diag.New("plan_unreadable", "Cannot determine working directory: "+err.Error())}
		_ = result.Render(stdout, r, command.Format)
		return 1
	}
	r := pipeline.Run(ctx, command, cwd, stdin)
	if err = result.Render(stdout, r, command.Format); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	_, exit := diag.Outcome(r.Diagnostics)
	return exit
}

func buildVersion() result.Version {
	value, commit := "devel", ""
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			value = strings.TrimPrefix(info.Main.Version, "v")
		}
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				commit = setting.Value
			}
		}
	}
	if version != "" {
		value = strings.TrimPrefix(version, "v")
	}
	return result.Version{FormatVersion: 3, Command: "version", Version: value, Commit: commit, Go: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH}
}
