// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
//go:build unix && !darwin

package capture

// POSIX access ACLs are constrained by the group mask from the owner-only creation mode.
func removeInheritedAccess(_ string) error { return nil }
