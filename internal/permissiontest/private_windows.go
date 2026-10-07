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

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		tb.Fatal(err)
	}

	sddl := descriptor.String()

	sid := user.User.Sid.String()
	if strings.Count(sddl, "(") != 1 || !strings.Contains(sddl, "(A;") || !strings.Contains(sddl, ";;;"+sid+")") {
		tb.Fatalf("private object permits another principal: %s", sddl)
	}

	return sddl
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
