// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Command architecture checks all physical first-party Go source.
package main

import (
	"fmt"
	"os"

	"github.com/resoltico/pdfconcat/tools/architecture"
)

func main() {
	root := ".."
	if len(os.Args) == 2 {
		root = os.Args[1]
	} else if len(os.Args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: architecture [repository]")
		os.Exit(2)
	}
	count, err := architecture.Check(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Architecture: %d physical Go files checked\n", count)
}
