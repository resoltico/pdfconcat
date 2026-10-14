// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type campaignDiskUsage struct{ bytes, entries int64 }

type campaignDiskSample struct {
	usage      campaignDiskUsage
	nonregular string
	incomplete error
}

func incompleteCampaignInspection(err error) bool {
	// The pinned Go Root implementation keeps this sentinel private. Match only
	// its exact error, qualified by a real Root escape control; unknown errors
	// retain fail-closed behavior when the toolchain changes.
	var pathError *os.PathError
	if errors.As(err, &pathError) && pathError.Err.Error() == "path escapes from parent" {
		return true
	}
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.ELOOP) || campaignPlatformInspectionRace(err)
}

// Count visible logical data and entries in bounded batches. Links and other
// nonregular nodes contribute only their own metadata and are never followed or
// opened. Known live permission/topology races leave an explicitly partial
// observation; callers must require a complete inspection before claiming one.
func countCampaignDiskTree(root *os.Root, path string, limit campaignDiskUsage) (campaignDiskSample, error) {
	var sample campaignDiskSample
	pending := []string{path}
	markPartial := func(err error) {
		if sample.incomplete == nil {
			sample.incomplete = err
		}
	}
	addEntry := func(name string, info os.FileInfo) error {
		sample.usage.entries++
		if sample.usage.entries > limit.entries {
			return fmt.Errorf("more than %d entries", limit.entries)
		}
		if info.IsDir() {
			pending = append(pending, name)
			return nil
		}
		if !info.Mode().IsRegular() && sample.nonregular == "" {
			sample.nonregular = name
		}
		if info.Size() > limit.bytes-sample.usage.bytes {
			return fmt.Errorf("more than %d logical bytes", limit.bytes)
		}
		sample.usage.bytes += info.Size()
		return nil
	}
	// A selected report path may be a single file. The owned tree root itself is
	// already leased and does not consume its own child-entry allowance.
	if path != "." {
		info, err := root.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return sample, nil
		}
		if err != nil {
			if incompleteCampaignInspection(err) {
				sample.incomplete = err
				return sample, nil
			}
			return sample, err
		}
		pending = nil
		if err = addEntry(path, info); err != nil {
			return sample, err
		}
	}
	for len(pending) > 0 {
		directory := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		before, err := root.Lstat(directory)
		if err != nil {
			if incompleteCampaignInspection(err) {
				markPartial(err)
				continue
			}
			return sample, err
		}
		if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			markPartial(fmt.Errorf("directory topology changed: %s", directory))
			if !before.Mode().IsRegular() {
				sample.nonregular = directory
			}
			continue
		}
		file, err := root.Open(directory)
		if err != nil {
			if incompleteCampaignInspection(err) {
				markPartial(err)
				continue
			}
			return sample, err
		}
		opened, statErr := file.Stat()
		current, currentErr := root.Lstat(directory)
		if statErr != nil {
			return sample, errors.Join(statErr, file.Close())
		}
		if currentErr != nil || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, current) {
			closeErr := file.Close()
			if closeErr != nil {
				return sample, closeErr
			}
			if currentErr != nil && !incompleteCampaignInspection(currentErr) {
				return sample, currentErr
			}
			markPartial(fmt.Errorf("directory changed while opening: %s", directory))
			continue
		}
		// Nested renames/substitutions are legitimate scratch activity. The opened
		// handle must match the current in-root directory, not its earlier name.
		scanErr := func() error {
			for {
				entries, readErr := file.ReadDir(64)
				for _, entry := range entries {
					name := filepath.Join(directory, entry.Name())
					info, err := root.Lstat(name)
					if err != nil {
						if incompleteCampaignInspection(err) {
							markPartial(err)
							continue
						}
						return err
					}
					if err = addEntry(name, info); err != nil {
						return err
					}
				}
				if errors.Is(readErr, io.EOF) {
					return nil
				}
				if readErr != nil {
					if incompleteCampaignInspection(readErr) {
						markPartial(readErr)
						return nil
					}
					return readErr
				}
			}
		}()
		if err = errors.Join(scanErr, file.Close()); err != nil {
			return sample, err
		}
	}
	return sample, nil
}
