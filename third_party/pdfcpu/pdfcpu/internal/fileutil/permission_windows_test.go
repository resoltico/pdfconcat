/*
Copyright 2026 The pdfcpu Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package fileutil

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/sys/windows"
)

func denyFileCreation(t *testing.T, dir string) {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	original, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		t.Fatal(err)
	}
	protection := windows.SECURITY_INFORMATION(windows.UNPROTECTED_DACL_SECURITY_INFORMATION)
	if control&windows.SE_DACL_PROTECTED != 0 {
		protection = windows.PROTECTED_DACL_SECURITY_INFORMATION
	}
	t.Cleanup(func() {
		if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|protection, nil, nil, original, nil); err != nil {
			t.Error(err)
		}
	})
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	var pin runtime.Pinner
	pin.Pin(user.User.Sid)
	defer pin.Unpin()
	denied, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.FILE_WRITE_DATA, // FILE_ADD_FILE on a directory.
		AccessMode:        windows.DENY_ACCESS,
		Trustee:           windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_USER, TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid)},
	}}, original)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, denied, nil); err != nil {
		t.Fatal(err)
	}
	probe, err := windows.UTF16PtrFromString(filepath.Join(dir, "permission-probe"))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(probe, windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err == nil {
		if closeErr := windows.CloseHandle(handle); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal("native FILE_ADD_FILE denial did not reject creation")
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("native FILE_ADD_FILE denial returned %v", err)
	}
}
