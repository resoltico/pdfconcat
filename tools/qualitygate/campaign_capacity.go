// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"fmt"
)

// This sampled cancellation threshold is headroom, not reserved capacity or a
// guarantee that all finalization work fits after concurrent writes continue.
var errCampaignCapacityQuery = errors.New("campaign volume query unavailable")

var errCampaignResourceStop = errors.New("campaign resource policy stopped execution")

const campaignFinalizationBytes uint64 = 1 << 30

func requireCampaignCapacity(path string, reserve uint64) (uint64, error) {
	available, err := availableCampaignBytes(path)
	if err != nil {
		return 0, errors.Join(errCampaignCapacityQuery, fmt.Errorf("%w: query campaign volume capacity at %q: %w", errGate, path, err))
	}
	if available < reserve {
		return available, fmt.Errorf("%w: campaign volume at %q has %d available bytes, below the %d-byte finalization threshold", errGate, path, available, reserve)
	}
	return available, nil
}
