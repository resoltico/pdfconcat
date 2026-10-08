// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build unix

package publish

import (
	"fmt"
	"os"

	"github.com/resoltico/pdfconcat/internal/capture"
)

func openReportLease(path string) (*os.File, error) {
	file, err := capture.OpenRegular(path)
	if err != nil {
		return nil, fmt.Errorf("open report identity pin: %w", err)
	}

	return file, nil
}

// Unix Lstat metadata already contains the namespace object's device and inode.
func checkReportNamespace(live, current os.FileInfo, path string) error {
	return compareReportObjects(live, current, path)
}
