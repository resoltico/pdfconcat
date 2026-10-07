// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseBytesRejectsRemoteTamperingAndInvalidState(t *testing.T) {
	t.Parallel()

	for _, fault := range []string{"none", "tampered", "checksum", "extra", duplicateFixture, "missing", "published", "tag"} {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()
			state, trusted, remote := remoteFixture(t)
			tamperRemote(t, fault, &state, remote)

			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}

			problems, err := releaseByteProblems("v1.0.0", data, trusted, remote)
			if err != nil {
				t.Fatal(err)
			}

			if (len(problems) == 0) != (fault == "none") {
				t.Fatalf("fault %s: %v", fault, problems)
			}
		})
	}
}

func remoteFixture(t *testing.T) (releaseState, string, string) {
	t.Helper()
	trusted, remote := t.TempDir(), t.TempDir()
	state := releaseState{IsDraft: true, TagName: "v1.0.0"}

	var manifest strings.Builder

	for _, target := range archiveTargets() {
		name := "PDFConcat_1.0.0_" + strings.ReplaceAll(target, "/", "_") + ".tar.gz"
		data := []byte("verified local artifact " + target)
		digest := sha256.Sum256(data)
		manifest.WriteString(hex.EncodeToString(digest[:]) + "  " + name + "\n")

		for _, dir := range []string{trusted, remote} {
			writeRemoteFixture(t, dir, name, data)
		}

		state.Assets = append(state.Assets, releaseAsset{Name: name})
	}

	for _, dir := range []string{trusted, remote} {
		writeRemoteFixture(t, dir, checksumFile, []byte(manifest.String()))
	}

	state.Assets = append(state.Assets, releaseAsset{Name: checksumFile})

	return state, trusted, remote
}

func writeRemoteFixture(t *testing.T, dir, name string, data []byte) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), data, fileMode); err != nil {
		t.Fatal(err)
	}
}

func tamperRemote(t *testing.T, fault string, state *releaseState, remote string) {
	t.Helper()

	switch fault {
	case "tampered":
		writeRemoteFixture(t, remote, state.Assets[0].Name, []byte("same name, different bytes"))
	case "checksum":
		writeRemoteFixture(t, remote, checksumFile, []byte("different checksum manifest"))
	case "extra":
		state.Assets = append(state.Assets, releaseAsset{Name: "source.zip"})
	case duplicateFixture:
		state.Assets = append(state.Assets, state.Assets[0])
	case "missing":
		state.Assets = state.Assets[1:]
	case "published":
		state.IsDraft = false
	case "tag":
		state.TagName = "v0.0.0"
	default:
	}
}
