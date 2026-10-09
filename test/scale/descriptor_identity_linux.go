// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package scale

import "syscall"

func descriptorArtifactDevice(stat *syscall.Stat_t) uint64 { return stat.Dev }
