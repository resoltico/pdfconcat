// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package permissiontest

import (
	"runtime"
	"testing"

	"golang.org/x/sys/windows"
)

// Directory-specific access bits from WinNT.h (the x/sys package names only FILE_WRITE_DATA for bit2).
const (
	directoryAddFile         windows.ACCESS_MASK = 0x0002
	directoryAddSubdirectory windows.ACCESS_MASK = 0x0004
	directoryDeleteChild     windows.ACCESS_MASK = 0x0040
)

// RequireEnforcement rejects missing prerequisites in the native denial operation itself.
func RequireEnforcement(tb testing.TB) { tb.Helper() }

// DenyDirectoryChanges denies namespace creation/deletion by the current SID in this owned test directory.
// Its returned closure restores the original DACL, including whether inheritance was protected.
func DenyDirectoryChanges(tb testing.TB, dir string) func() {
	tb.Helper()

	descriptor, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil || descriptor == nil {
		tb.Fatalf("required owned-directory DACL prerequisite unavailable: %v", err)
	}

	original, _, err := descriptor.DACL()
	if err != nil {
		tb.Fatal(err)
	}

	control, _, err := descriptor.Control()
	if err != nil {
		tb.Fatal(err)
	}

	deny := namespaceDenyACL(tb, original)

	setErr := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, deny, nil)
	if setErr != nil {
		tb.Fatal(setErr)
	}

	return registerDACLRestore(tb, dir, original, control)
}

func namespaceDenyACL(tb testing.TB, original *windows.ACL) *windows.ACL {
	tb.Helper()

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		tb.Fatal(err)
	}

	var pinner runtime.Pinner

	pinner.Pin(user.User.Sid)
	defer pinner.Unpin()

	deny, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: directoryAddFile | directoryAddSubdirectory | directoryDeleteChild | windows.DELETE,
			AccessMode:        windows.DENY_ACCESS,
			Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
			},
		},
	}, original)
	if err != nil {
		tb.Fatal(err)
	}

	return deny
}

func registerDACLRestore(tb testing.TB, dir string, original *windows.ACL, control windows.SECURITY_DESCRIPTOR_CONTROL) func() {
	tb.Helper()

	protection := windows.SECURITY_INFORMATION(windows.UNPROTECTED_DACL_SECURITY_INFORMATION)
	if control&windows.SE_DACL_PROTECTED != 0 {
		protection = windows.PROTECTED_DACL_SECURITY_INFORMATION
	}

	restore := func() {
		restoreErr := windows.SetNamedSecurityInfo(
			dir,
			windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION|protection,
			nil,
			nil,
			original,
			nil,
		)
		if restoreErr != nil {
			tb.Error(restoreErr)
		}
	}
	tb.Cleanup(restore)

	return restore
}
