//go:build windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package permissiontest

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// RequirePrivate inspects the real protected DACL, not Windows' synthetic Unix mode bits.
func requirePrivateACL(tb testing.TB, path string) string {
	tb.Helper()

	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		tb.Fatal(err)
	}

	acl, defaulted, err := descriptor.DACL()
	if err != nil || defaulted || acl == nil || acl.AceCount != 1 {
		tb.Fatalf("private object needs one owner ACE: %v", err)
	}

	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		tb.Fatalf("private object DACL is not protected: %v", err)
	}

	sddl := descriptor.String()

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		tb.Fatal(err)
	}

	if !aclNamesOwner(sddl, user.User.Sid) {
		tb.Fatalf("private object permits another principal: %s", sddl)
	}

	return sddl
}

// aclNamesOwner resolves SDK aliases such as LA and compares actual SID identities.
func aclNamesOwner(sddl string, owner *windows.SID) bool {
	if strings.Count(sddl, "(") != 1 || !strings.Contains(sddl, "(A;") {
		return false
	}

	_, principal, found := strings.Cut(sddl, ";;;")

	principal, terminated := strings.CutSuffix(principal, ")")
	if !found || !terminated {
		return false
	}

	descriptor, err := windows.SecurityDescriptorFromString("O:" + principal)
	if err != nil {
		return false
	}

	actual, defaulted, err := descriptor.Owner()

	return err == nil && !defaulted && actual != nil && actual.Equals(owner)
}

// RequirePrivateDirectory requires owner-only inheritable access to an actual directory.
func RequirePrivateDirectory(tb testing.TB, path string) {
	tb.Helper()

	sddl := requirePrivateACL(tb, path)
	if !strings.Contains(sddl, "OI") || !strings.Contains(sddl, "CI") {
		tb.Fatalf("private workspace ACE is not inheritable: %s", sddl)
	}
}

// RequirePrivateFile requires owner-only protected access to an actual file.
func RequirePrivateFile(tb testing.TB, path string) { tb.Helper(); requirePrivateACL(tb, path) }
