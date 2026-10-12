/*
Copyright 2026 The pdfcpu Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package model

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestHexStringIgnoresPDFWhitespaceBetweenNibbles(t *testing.T) {
	for _, test := range []struct {
		input, expected string
		valid           bool
	}{
		{"", "", true},
		{"abcdef0123456789", "ABCDEF0123456789", true},
		{"ABCDEF0123456789", "ABCDEF0123456789", true},
		{"ab cd ef", "ABCDEF", true},
		{"a b c", "ABC0", true},
		{"a\tb\nc\rd\fe f", "ABCDEF", true},
		{" \t\n\f\r ", "", true},
		{"123", "1230", true},
		{"A\x00B", "AB", true},
		{"A\vB", "", false},
		{"A\u00a0B", "", false},
		{"ＡＢ", "", false},
		{"ł", "", false},
		{"\xc0\xaf", "", false},
		{"\xe2\x82", "", false},
		{"\uFFFD", "", false},
		{"ß", "", false},
		{"\xff", "", false},
		{"0G", "", false},
	} {
		result, valid := hexString(test.input)
		if valid != test.valid || valid && (result == nil || *result != test.expected) || !valid && result != nil {
			t.Fatalf("input %q: value=%v valid=%v expected=%q valid=%v", test.input, result, valid, test.expected, test.valid)
		}
	}
}

func TestHexStringLargeCanonicalAndFormattedValues(t *testing.T) {
	canonical := strings.Repeat("ABCDEF0123456789", 2048)
	for _, test := range []struct {
		name, input, expected string
		valid                 bool
	}{
		{"canonical", canonical, canonical, true},
		{"formatted", strings.Repeat("a b C\td\n", 4096), strings.Repeat("ABCD", 4096), true},
		{"late-invalid-digit", canonical + "G", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, valid := hexString(test.input)
			if valid != test.valid || valid && (result == nil || *result != test.expected) || !valid && result != nil {
				t.Fatalf("valid=%t, want %t; normalized value differs", valid, test.valid)
			}
		})
	}
}

func TestHexLiteralRetainsCallerRemainderAndInvalidSkip(t *testing.T) {
	for _, test := range []struct {
		input, value, remainder string
		skip                    bool
	}{
		{"<>tail", "", "tail", false},
		{"<a b> next", "AB", " next", false},
		{"<A> >tail", "A0", " >tail", false},
		{"<\vA\v>tail", "A0", "tail", false},
		{"<\u00a0ab\u00a0>tail", "AB", "tail", false},
		{"<G>tail", "", "tail", true},
		{"<\xff>tail", "", "tail", true},
	} {
		line := test.input
		value, err := parseHexLiteral(&line)
		if err != nil || line != test.remainder {
			t.Fatalf("input %q: %v remainder %q", test.input, err, line)
		}
		if test.skip {
			if value != nil {
				t.Fatalf("invalid literal returned %v", value)
			}
			continue
		}
		if value != types.HexLiteral(test.value) {
			t.Fatalf("input %q returned %v expected %q", test.input, value, test.value)
		}
	}
	for _, line := range []string{"<AB", ""} {
		original := line
		if _, err := parseHexLiteral(&line); err == nil || line != original {
			t.Fatalf("invalid input %q advanced or accepted", original)
		}
	}
}

func TestCanceledHexObjectRetainsContextRefusal(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	line := "<abcdef> remainder"
	if _, err := ParseObject(ctx, &line, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
