// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package scale

import "time"

// DescriptorCollectorIdentity identifies a compiled test-only descriptor query.
// Its observed counts are sampled lower bounds, independent of kernel ceilings.
type DescriptorCollectorIdentity struct {
	NativeCompilerIdentity

	ReadinessStarted  time.Time `json:"readiness_started"`
	ReadinessFinished time.Time `json:"readiness_finished"`
	ReadinessFrame    string    `json:"readiness_frame"`

	SourceSHA256 string `json:"source_sha256"`
	BinarySHA256 string `json:"binary_sha256"`
	ReadinessPID int    `json:"readiness_pid"`
}
