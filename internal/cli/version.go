// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package cli

import (
	"errors"
	"strings"

	result "github.com/resoltico/pdfconcat/internal/wire"
)

// BuildMetadata is captured by the process host, independent of execution.
type BuildMetadata struct{ Version, Commit, Go, Platform string }

// Version projects explicit build metadata into the version wire contract.
func Version(info BuildMetadata, override string) (result.Version, error) {
	for _, value := range []string{info.Version, info.Commit, info.Go, info.Platform, override} {
		if !validInvocationText(value) {
			return result.Version{}, errors.New("invalid version metadata")
		}
	}
	value := info.Version
	if value == "" || value == "(devel)" {
		value = "devel"
	}
	if override != "" {
		value = override
	}
	value = strings.TrimPrefix(value, "v")
	return result.Version{FormatVersion: result.WireVersion, Command: "version", Version: value, Commit: info.Commit, Go: info.Go, Platform: info.Platform}, nil
}
