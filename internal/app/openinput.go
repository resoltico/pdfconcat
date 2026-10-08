// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/resoltico/pdfconcat/internal/capture"
)

// openInput opens only regular named plan/report inputs; stdin streaming is handled separately.
func openInput(ctx context.Context, path string) (*os.File, error) {
	return openInputWith(ctx, path, capture.OpenRegular)
}

// openInputWith keeps ownership of a handle if cancellation arrives while the checked open runs.
func openInputWith(ctx context.Context, path string, open func(string) (*os.File, error)) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	file, err := open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	if contextErr := ctx.Err(); contextErr != nil {
		return nil, errors.Join(contextErr, file.Close())
	}

	return file, nil
}
