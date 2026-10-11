// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import "testing"

// These tuples describe the trim stage after native decomposition. They do not
// establish Windows filepath.VolumeName behavior or actual filesystem traversal.
func TestNativeParentTrimPreservesRootsAndUnresolvedComponents(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name, parent, volume, want string
		separator                  byte
	}{
		{"POSIX single root", "/", "", "/", '/'},
		{"POSIX repeated root", "///", "", "/", '/'},
		{"POSIX backslash name", `/work/name\/`, "", `/work/name\`, '/'},
		{"POSIX unresolved traversal", "/work/link/../", "", "/work/link/..", '/'},
		{"Windows single forward drive root", "C:/", "C:", "C:/", '\\'},
		{"Windows single backslash drive root", `C:\`, "C:", `C:\`, '\\'},
		{"Windows repeated drive root", `C:\\`, "C:", `C:\`, '\\'},
		{"Windows repeated forward drive root", "C://", "C:", `C:\`, '\\'},
		{"Windows single forward current-drive root", "/", "", "/", '\\'},
		{"Windows UNC repeated root", `\\server\share\\`, `\\server\share`, `\\server\share\`, '\\'},
		{"Windows device repeated drive root", `\\.\C:\\`, `\\.\C:`, `\\.\C:\`, '\\'},
		{"Windows verbatim repeated drive root", `\\?\C:\\`, `\\?\C:`, `\\?\C:\`, '\\'},
		{"Windows verbatim UNC repeated root", `\\?\UNC\server\share\\`, `\\?\UNC\server\share`, `\\?\UNC\server\share\`, '\\'},
		{"Windows unresolved traversal", `C:\work\link\..\`, "C:", `C:\work\link\..`, '\\'},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := trimNativeParent(row.parent, row.volume, row.separator); got != row.want {
				t.Fatalf("native trim %q: got=%q want=%q", row.parent, got, row.want)
			}
		})
	}
}
