// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Command client exercises the application without importing any frontend or model.
package main

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"

	"github.com/resoltico/pdfconcat/internal/core"
)

func main() {
	if len(os.Args) != 6 {
		panic("client MODE PLAN WORKINGDIR OUTPUT REPORT")
	}
	mode, path, cwd, output, report := os.Args[1], os.Args[2], os.Args[3], os.Args[4], os.Args[5]
	var input core.Input
	switch mode {
	case "file":
		input = core.FromPlan(core.FilePlan(path))
	case "document":
		document, ds := core.ReadPlan(context.Background(), core.FilePlan(path), cwd)
		if len(ds) > 0 {
			panic(ds)
		}
		input = core.FromDocument(document)
	case "bytes", "loader":
		data, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}
		source := core.BytesPlan(data, path, filepath.Dir(path))
		if mode == "loader" {
			source = core.LoadPlan(path, filepath.Dir(path), func(context.Context, int64) ([]byte, error) { return data, nil })
		}
		input = core.FromPlan(source)
	default:
		panic("unknown mode")
	}
	outcome := core.Execute(context.Background(), core.NewRequest(core.Build, input, core.Options{WorkingDir: cwd, Output: output, Report: report}))
	data, err := json.Marshal(outcome)
	if err != nil {
		panic(err)
	}
	if _, err = fmt.Fprintln(os.Stdout, string(data)); err != nil {
		panic(err)
	}
	os.Exit(core.ExitCode(outcome))
}
