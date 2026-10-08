// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// releaseState is the GitHub release state accompanying downloaded, untrusted asset bytes.
type (
	releaseState struct {
		TagName string         `json:"tag_name"`
		Assets  []releaseAsset `json:"assets"`
		IsDraft bool           `json:"is_draft"`
	}
	releaseAsset struct {
		Name string `json:"name"`
	}
)

// verifyRelease compares remote bytes to the local manifest whose subjects were already attested.
// It never trusts the checksum manifest downloaded from the release.
func verifyRelease(_ context.Context, args []string) error {
	set := newFlags(releaseBytesCommand)
	tag := set.String(releaseTagFlag, "", "exact expected release tag")
	stateFile := set.String("state", "", "JSON release state returned by gh release view")
	trusted := set.String("trusted", "", "verified and attested local artifact directory")

	remote := set.String("remote", "", "directory containing downloaded remote asset bytes")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf(parseFlagsError, err)
	}

	if *tag == "" || *stateFile == "" || *trusted == "" || *remote == "" || set.NArg() != 0 {
		return fmt.Errorf("%w: tag, state, trusted and remote are required", errGate)
	}

	state, err := os.ReadFile(*stateFile)
	if err != nil {
		return fmt.Errorf("read release state: %w", err)
	}

	problems, err := releaseByteProblems(*tag, state, *trusted, *remote)
	if err != nil {
		return err
	}

	return report(releaseBytesCommand, problems, "draft tag, exact asset allowlist and every remote byte match the attested artifacts")
}

func releaseByteProblems(tag string, data []byte, trusted, remote string) ([]string, error) {
	var state releaseState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse release state: %w", err)
	}

	expected, err := trustedAssets(trusted)
	if err != nil {
		return nil, err
	}

	problems := releaseInventoryProblems(tag, &state, expected)

	entries, err := os.ReadDir(remote)
	if err != nil {
		return nil, fmt.Errorf("read downloaded assets: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !expected[entry.Name()] {
			problems = append(problems, "unexpected downloaded asset "+entry.Name())
		}
	}

	for name := range expected {
		want, readErr := readInRoot(trusted, name)

		got, downloadErr := readInRoot(remote, name)
		if readErr != nil || downloadErr != nil || !bytes.Equal(want, got) {
			problems = append(problems, "remote bytes differ from attested artifact "+name)
		}
	}

	return problems, nil
}

// trustedAssets derives the deliberate allowlist from the previously inspected local manifest.
func trustedAssets(trusted string) (map[string]bool, error) {
	sums, err := readChecksums(trusted)
	if err != nil {
		return nil, err
	}

	expected := map[string]bool{checksumFile: true}
	targets := map[string]bool{}

	for name := range sums {
		match := archiveName.FindStringSubmatch(name)
		if match == nil {
			return nil, fmt.Errorf("%w: unexpected trusted asset %s", errGate, name)
		}

		target := match[2] + "/" + match[3]
		if targets[target] {
			return nil, fmt.Errorf("%w: duplicate trusted target %s", errGate, target)
		}

		targets[target] = true

		expected[name] = true
		if problems := checksumProblems(filepath.Join(trusted, name), sums); len(problems) != 0 {
			return nil, fmt.Errorf("%w: %v", errGate, problems)
		}
	}

	if len(targets) != len(archiveTargets()) {
		return nil, fmt.Errorf("%w: trusted manifest must contain all six targets", errGate)
	}

	return expected, nil
}

func releaseInventoryProblems(tag string, state *releaseState, expected map[string]bool) []string {
	var problems []string
	if !state.IsDraft || state.TagName != tag {
		problems = append(problems, "release must be a draft with the exact expected tag")
	}

	seen := map[string]bool{}
	for _, asset := range state.Assets {
		if seen[asset.Name] || !expected[asset.Name] {
			problems = append(problems, "duplicate or unexpected remote asset "+asset.Name)
		}

		seen[asset.Name] = true
	}

	for name := range expected {
		if !seen[name] {
			problems = append(problems, "missing remote asset "+name)
		}
	}

	return problems
}
