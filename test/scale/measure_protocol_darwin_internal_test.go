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
		{"observed descriptors", "p42\nc3\ne0\ng0\n", 3, true},
		{"native zero", "p42\nc0\ne0\ng0\n", 0, true},
		{"native errno", "p42\nc0\ne1\ng0\n", 0, true},
		{"buffer grown", "p42\nc40\ne0\ng2\n", 40, true},
		{"empty reply", "", 0, false},
		{"missing process", "c3\ne0\ng0\n", 0, false},
		{"wrong process", "p43\nc3\ne0\ng0\n", 0, false},
		{"second process", "p42\nc3\ne0\ng0\np42\n", 0, false},
		{"signed process", "p+42\nc3\ne0\ng0\n", 0, false},
		{"unterminated", "p42\nc3\ne0\ng0", 0, false},
		{"unknown field", "p42\nx3\ne0\ng0\n", 0, false},
		{"empty count", "p42\nc\ne0\ng0\n", 0, false},
		{"negative count", "p42\nc-1\ne0\ng0\n", 0, false},
		{"signed count", "p42\nc+1\ne0\ng0\n", 0, false},
		{"unicode count", "p42\nc１\ne0\ng0\n", 0, false},
		{"overflow count", "p42\nc18446744073709551616\ne0\ng0\n", 0, false},
		{"duplicate count", "p42\nc3\n" + "c3\ng0\n", 0, false},
		{"errored count", "p42\nc3\ne1\ng0\n", 0, false},
		{"out of native errno domain", "p42\nc0\ne2147483648\ng0\n", 0, false},
		{"growth budget", "p42\nc0\ne0\ng13\n", 0, false},
		{"full buffer", "p42\nc16\ne0\ng0\n", 0, false},
		{"blank extra record", "p42\nc3\ne0\ng0\n\n", 0, false},
		{"trailing garbage", "p42\nc3\ne0\ng0\ngarbage\n", 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reply, valid := parseDescriptorCollectorReply(tc.reply, 42)

			count := reply.count
			if valid != tc.valid || count != tc.count {
				t.Fatalf("reply %q: count=%d valid=%v; want count=%d valid=%v", tc.reply, count, valid, tc.count, tc.valid)
			}
		})
	}
}
