//go:build windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package permissiontest

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestPrivateACLMatchesSIDIdentityAcrossAliasesAndRejectsOthers(t *testing.T) {
	t.Parallel()

	descriptor, err := windows.SecurityDescriptorFromString("O:LA")
	if err != nil {
		t.Fatal(err)
	}

	owner, _, err := descriptor.Owner()
	if err != nil {
		t.Fatal(err)
	}

	for _, principal := range []string{"LA", owner.String()} {
		if !aclNamesOwner("D:P(A;;FA;;;"+principal+")", owner) {
			t.Fatalf("same SID rejected: %s", principal)
		}
	}

	for _, principal := range []string{"BA", "WD", owner.String() + "-1", "not-a-sid"} {
		if aclNamesOwner("D:P(A;;FA;;;"+principal+")", owner) {
			t.Fatalf("different or invalid principal accepted: %s", principal)
		}
	}
}
