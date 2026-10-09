// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package scale

import "syscall"

// descriptorArtifactDevice preserves Darwin's signed dev_t identity in the native C uintmax_t encoding.
func descriptorArtifactDevice(stat *syscall.Stat_t) uint64 { return uint64(stat.Dev) }
