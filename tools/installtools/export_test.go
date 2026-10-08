// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

// Exports for the external test package.

// ChecksumCheck exposes verifyChecksum.
func ChecksumCheck(content []byte, sums, asset string) error {
	return verifyChecksum(content, sums, asset)
}

// MemberOf exposes extractMember.
func MemberOf(archive []byte, name, member string) ([]byte, error) {
	return extractMember(archive, name, member)
}

// ToolSelection exposes selectTools.
func ToolSelection(names []string) (map[string]bool, error) { return selectTools(names) }
