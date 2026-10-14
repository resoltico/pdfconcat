// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"errors"
	"fmt"
	"path/filepath"
)

// Fuzz corpus failures and raw events remain outside disposable scratch.
type fuzzStorage struct{ *campaignScratch }

func newFuzzStorage(sourceRoot string) (*fuzzStorage, error) {
	scratch, err := newCampaignScratch(sourceRoot, "qualitygate-fuzz-")
	if err != nil {
		return nil, err
	}
	storage := &fuzzStorage{scratch}
	for _, name := range []string{"cache", "tmp"} {
		if err = scratch.root.Mkdir(name, dirMode); err != nil {
			return nil, errors.Join(err, scratch.close())
		}
	}
	return storage, nil
}

func (s *fuzzStorage) environment() []string {
	tmp := filepath.Join(s.path, "tmp")
	return []string{"GOCACHE=" + filepath.Join(s.path, "cache"), "GOCACHEPROG=", "GOTMPDIR=" + tmp, "TMPDIR=" + tmp, "TMP=" + tmp, "TEMP=" + tmp}
}

func (s *fuzzStorage) finish(result error) error {
	if errors.Is(result, errFuzzCleanupUncertain) {
		return errors.Join(result, fmt.Errorf("retain unsettled fuzz storage %q: %w", s.path, errFuzzCleanupUncertain), s.retain())
	}
	return errors.Join(result, s.close())
}
