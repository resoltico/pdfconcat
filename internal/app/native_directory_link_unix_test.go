//go:build unix

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"os"
	"testing"
)

func nativeDirectoryLink(_ *testing.T, target, link string) error { return os.Symlink(target, link) }
