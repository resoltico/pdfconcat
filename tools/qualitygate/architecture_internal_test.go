// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"strings"
	"testing"
)

func TestArchitectureDiscoveryRejectsMissingAndIncompleteEvidence(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ name, output, want string }{
		{"empty", "", "empty architecture"},
		{"malformed", "{", "decode architecture"},
		{"package error", `{"ImportPath":"example.org/project/internal/a","Error":{"Err":"missing path"}}`, "missing path"},
		{"dependency error", `{"ImportPath":"example.org/project/internal/a","DepsErrors":[{"Err":"missing import"}]}`, "missing import"},
		{"foreign module", `{"ImportPath":"example.org/projectSibling/internal/a"}`, "outside module"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := collectArchitectureFiles(test.output, "example.org/project", map[string]bool{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("missing evidence accepted: %v", err)
			}
		})
	}
}
