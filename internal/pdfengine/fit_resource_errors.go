// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import "fmt"

// fitResourceError preserves cancellation/backend identity and nil success at native API boundaries.
func fitResourceError(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("fitting resource: %w", err)
}

// fitLocatedResourceError identifies the resolved resource even when its metadata fails before entry.
func fitLocatedResourceError(binding, identity string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("used %s object %s: %w", binding, identity, err)
}
