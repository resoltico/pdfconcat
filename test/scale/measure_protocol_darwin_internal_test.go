// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package scale

import "testing"

func TestCollectorDescriptorProtocol(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		reply string
		count int64
		valid bool
	}{
		{"numbered descriptors and mappings", "p42\nfcwd\nftxt\nfmem\nfrtd\nf0\nf1\nf10\n", 3, true},
		{"exit edge known zero", "p42\nfcwd\n", 0, true},
		{"empty reply", "", 0, false},
		{"missing process", "fcwd\n", 0, false},
		{"wrong process", "p43\nfcwd\n", 0, false},
		{"second process", "p42\nf0\np42\nf1\n", 0, false},
		{"signed process", "p+42\nfcwd\n", 0, false},
		{"no fields", "p42\n", 0, false},
		{"unterminated", "p42\nfcwd", 0, false},
		{"unknown field", "p42\nfx\n", 0, false},
		{"error field", "p42\nferr\n", 0, false},
		{"empty field", "p42\nf\n", 0, false},
		{"negative descriptor", "p42\nf-1\n", 0, false},
		{"positive signed descriptor", "p42\nf+1\n", 0, false},
		{"unicode descriptor", "p42\nf１\n", 0, false},
		{"overflow descriptor", "p42\nf18446744073709551616\n", 0, false},
		{"duplicate descriptor", "p42\nf0\n" + "f0\n", 0, false},
		{"duplicate numeric identity", "p42\nf0\nf00\n", 0, false},
		{"blank extra record", "p42\nfcwd\n\n", 0, false},
		{"trailing garbage", "p42\nfcwd\ngarbage\n", 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			count, valid := collectorDescriptorCount(tc.reply, 42)
			if valid != tc.valid || count != tc.count {
				t.Fatalf("reply %q: count=%d valid=%v; want count=%d valid=%v", tc.reply, count, valid, tc.count, tc.valid)
			}
		})
	}
}
