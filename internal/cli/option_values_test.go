// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package cli

import "testing"

func TestRequiredValuesStopBeforeRecognizedOptions(t *testing.T) {
	var boundaries []string
	for _, spec := range options {
		boundaries = append(boundaries, spec.name, spec.name+"=value")
		if spec.alias != "" {
			boundaries = append(boundaries, spec.alias)
		}
	}
	boundaries = append(boundaries, "--blank")
	for _, spec := range options {
		if spec.value == "" {
			continue
		}
		names := []string{spec.name}
		if spec.alias != "" {
			names = append(names, spec.alias)
		}
		for _, name := range names {
			for _, boundary := range boundaries {
				t.Run(name+"/"+boundary, func(t *testing.T) {
					_, failure := Parse([]string{"check", name, boundary, "source.pdf"})
					if failure == nil || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code.String() != "usage_missing_value" || len(failure.Diagnostics[0].Items) != 1 || failure.Diagnostics[0].Items[0] != "argv:1" {
						t.Fatalf("missing value at owning option: %+v", failure)
					}
				})
			}
		}
	}
}

func TestSeparateValueLiterals(t *testing.T) {
	for _, value := range []string{"--", "./--format", "--not-an-option", "=literal"} {
		c, failure := Parse([]string{"check", "--report", value, "source.pdf"})
		if failure != nil || c.Report != value || len(c.Operands) != 1 || c.Operands[0].Path != "source.pdf" {
			t.Fatalf("literal %q: %+v %+v", value, c, failure)
		}
	}
	c, failure := Parse([]string{"check", "--report=--format", "source.pdf"})
	if failure != nil || c.Report != "--format" {
		t.Fatalf("explicit literal: %+v %+v", c, failure)
	}
	_, failure = Parse([]string{"check", "--report", "", "--format", "text"})
	if failure == nil || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code.String() != "usage_invalid_value" || failure.Diagnostics[0].Items[0] != "argv:2" {
		t.Fatalf("empty value classification: %+v", failure)
	}
}
