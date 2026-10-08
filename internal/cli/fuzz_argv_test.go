// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package cli_test

import (
	"slices"
	"strconv"
	"testing"
)

func TestFuzzArgumentEncodingPreservesOSArguments(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		nil,
		{},
		{""},
		{"", ""},
		{"", argBuild},
		{argBuild, ""},
		{argBuild, "control\x1f.pdf", "quotes\" and spaces", "Rīga"},
	}

	for index, args := range cases {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			t.Parallel()

			if got := splitArgs(joinArgs(args)); !slices.Equal(got, args) {
				t.Fatalf("fuzz encoding changed arguments: %q, want %q", got, args)
			}
		})
	}
}

func TestFuzzArgumentSeedRejectsEmbeddedNUL(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("embedded NUL is outside the OS argument contract")
		}
	}()

	joinArgs([]string{"not\x00an argument"})
}
