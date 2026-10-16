// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"errors"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/resoltico/pdfconcat/internal/plan"
	"github.com/resoltico/pdfconcat/internal/result"
)

// SchemaKind selects a supported wire contract.
type SchemaKind string

// Schema selectors are the supported editor and result contracts.
const (
	PlanSchema   SchemaKind = "plan"
	ResultSchema SchemaKind = "result"
)

// Schema returns an owned copy of a supported wire contract.
func Schema(kind SchemaKind) ([]byte, error) {
	switch kind {
	case PlanSchema:
		return slices.Clone(plan.Schema), nil
	case ResultSchema:
		return slices.Clone(result.Schema), nil
	default:
		return nil, errors.New("unknown schema")
	}
}

// Version interprets build metadata and validates the host release override.
func Version(releaseOverride string) (VersionInfo, error) {
	if !metadata(releaseOverride) {
		return VersionInfo{}, errors.New("invalid version metadata")
	}
	value, commit := "devel", ""
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			value = strings.TrimPrefix(info.Main.Version, "v")
		}
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				commit = s.Value
			}
		}
	}
	if releaseOverride != "" {
		value = strings.TrimPrefix(releaseOverride, "v")
	}
	if !metadata(value) || !metadata(commit) || !metadata(runtime.Version()) {
		return VersionInfo{}, errors.New("invalid version metadata")
	}
	return VersionInfo{FormatVersion: result.WireVersion, Command: "version", Version: value, Commit: commit, Go: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH}, nil
}
