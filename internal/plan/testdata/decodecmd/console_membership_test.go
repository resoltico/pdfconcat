// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import "testing"

func TestPrivateConsoleMembershipRejectsOtherParticipants(t *testing.T) {
	t.Parallel()

	const (
		target uint32 = 101
		sender uint32 = 202
	)
	for _, members := range [][]uint32{{target, sender}, {sender, target}} {
		if !privateConsoleMembership(members, 2, target, sender) {
			t.Fatal("private target/sender pair rejected")
		}
	}

	for _, row := range []struct {
		members []uint32
		count   uintptr
	}{
		{[]uint32{target, sender}, 0},
		{[]uint32{target, sender}, 3},
		{[]uint32{target, 303}, 2},
		{[]uint32{sender, sender}, 2},
		{[]uint32{target}, 1},
	} {
		if privateConsoleMembership(row.members, row.count, target, sender) {
			t.Fatal("non-private console broadcast allowed")
		}
	}

	if privateConsoleMembership([]uint32{target, sender}, 2, target, target) {
		t.Fatal("same process used as both target and sender")
	}
}
