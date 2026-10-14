// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import "testing"

func requireCampaignProducerExited(t *testing.T, pid int) {
	t.Helper()
	requireFuzzWorkerExited(t, pid)
}
