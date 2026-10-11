// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"runtime"
	"strings"
	"time"
)

// SourceBuildIdentity is the reproducible metadata and cache namespace of the reviewed source build.
// Date describes the upstream source tag, and Commit names the patch digest rather than a VCS commit.
type SourceBuildIdentity struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// GremlinsBuildIdentity identifies the reviewed executor source variant without inventing VCS metadata.
func GremlinsBuildIdentity(versions map[string]string) (SourceBuildIdentity, error) {
	return sourceBuildIdentity(versions, "GREMLINS", "executor")
}

func sourceBuildIdentity(versions map[string]string, prefix, flavor string) (SourceBuildIdentity, error) {
	base := versions[prefix+"_VERSION"]
	if !exactVersion.MatchString(base) {
		return SourceBuildIdentity{}, fmt.Errorf("%w: invalid source tool version", ErrToolVersions)
	}

	for _, key := range []string{prefix + "_SOURCE_ZIP_SHA256", prefix + "_PATCH_SHA256"} {
		bytes, err := hex.DecodeString(versions[key])
		if err != nil || len(bytes) != sha256.Size || versions[key] != strings.ToLower(versions[key]) {
			return SourceBuildIdentity{}, fmt.Errorf("%w: %s must be a lower-case SHA-256 digest", ErrToolVersions, key)
		}
	}

	for _, key := range []string{prefix + "_SOURCE_SUM", prefix + "_SOURCE_MOD_SUM"} {
		value, found := strings.CutPrefix(versions[key], "h1:")

		bytes, err := base64.StdEncoding.DecodeString(value)
		if !found || err != nil || len(bytes) != sha256.Size {
			return SourceBuildIdentity{}, fmt.Errorf("%w: %s must be a module h1 checksum", ErrToolVersions, key)
		}
	}

	sourceTime := versions[prefix+"_SOURCE_TIME"]
	if _, err := time.Parse(time.RFC3339, sourceTime); err != nil {
		return SourceBuildIdentity{}, fmt.Errorf("%w: invalid source tool tag time: %w", ErrToolVersions, err)
	}

	patch := versions[prefix+"_PATCH_SHA256"]

	return SourceBuildIdentity{
		Version: strings.TrimPrefix(base, "v") + "+" + flavor + "." + patch,
		Commit:  "patch-sha256:" + patch,
		Date:    "upstream-source:" + sourceTime,
	}, nil
}

// VerifyGolangciBinaryMetadata rejects substituted commands and binaries for other native targets.
func VerifyGolangciBinaryMetadata(file string) error {
	info, err := buildinfo.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read linter build metadata: %w", err)
	}

	const module = "github.com/golangci/golangci-lint/v2"
	if info.Main.Path != module || info.Path != module+"/cmd/golangci-lint" {
		return fmt.Errorf("%w: linter module or main package identity differs", ErrToolVersions)
	}

	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}

	if settings["GOOS"] != runtime.GOOS || settings["GOARCH"] != runtime.GOARCH {
		return fmt.Errorf("%w: linter target differs from native host", ErrToolVersions)
	}

	return nil
}
