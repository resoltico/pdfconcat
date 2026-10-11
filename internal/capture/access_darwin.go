// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
//go:build darwin

package capture

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"
)

// removeInheritedAccess clears the extended ACL on a newly created empty owned object.
// ATTR_CMN_EXTENDED_SECURITY is an attrreference followed by kauth_filesec (sys/attr.h,
// sys/kauth.h); only its ACL is changed. Owner/group metadata is not transplanted.
// This is done before any sensitive bytes are written and requires no runtime subprocess.
func removeInheritedAccess(path string) error {
	const referenceSize = 8
	const securitySize = 44
	const filesecMagic = 0x012cc16d
	const noACL = 0xffffffff
	buffer := make([]byte, referenceSize+securitySize)
	binary.NativeEndian.PutUint32(buffer[0:4], referenceSize)
	binary.NativeEndian.PutUint32(buffer[4:8], securitySize)
	binary.NativeEndian.PutUint32(buffer[8:12], filesecMagic)
	binary.NativeEndian.PutUint32(buffer[44:48], noACL)
	attributes := unix.Attrlist{Bitmapcount: 5, Commonattr: unix.ATTR_CMN_EXTENDED_SECURITY}
	if err := unix.Setattrlist(path, &attributes, buffer, unix.FSOPT_NOFOLLOW); err != nil {
		return fmt.Errorf("remove inherited access on private object: %w", err)
	}
	return nil
}
