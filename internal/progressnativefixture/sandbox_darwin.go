// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin && cgo

package progressnativefixture

/*
#include <sandbox.h>
#include <stdlib.h>

static int progress_deny_getpath(char **detail) {
	// This deprecated public API is used only by a private native test process.
	// Applying the policy after Go startup avoids denying dyld's required F_GETPATH.
	return sandbox_init("(version 1)(allow default)(deny system-fcntl (fcntl-command F_GETPATH))", 0, detail);
}
*/
import "C"

import "fmt"

// DenyTerminalPath installs a permanent private-process policy denying F_GETPATH.
// It must never run in a normal process: the native policy cannot be removed.
func DenyTerminalPath() error {
	var detail *C.char

	result := C.progress_deny_getpath(&detail)
	if detail != nil {
		defer C.sandbox_free_error(detail)
	}
	if result == 0 {
		return nil
	}

	return fmt.Errorf("apply native F_GETPATH denial: %s", C.GoString(detail))
}
