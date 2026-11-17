// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

// Operation is a supported packet use case.
type Operation string

// Supported packet operations.
const (
	Check Operation = "check"
	Build Operation = "build"
)
