// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
)

// ForeignSource identifies one complete immutable module ZIP plus the reviewed local patch.
// Its identity is validated against reconstructed file bytes, separately from behavioral checks.
type (
	ForeignSource struct {
		TestInputs     ForeignTestInputs `json:"test_inputs,omitzero"`
		Root           string            `json:"root"`
		Module         string            `json:"module"`
		Version        string            `json:"version"`
		Repository     string            `json:"repository"`
		Revision       string            `json:"revision"`
		SourceArtifact string            `json:"source_artifact"`
		SourceSHA256   string            `json:"source_sha256"`
		ModuleSum      string            `json:"module_sum"`
		GoModSum       string            `json:"go_mod_sum"`
		Patch          string            `json:"patch"`
		PatchSHA256    string            `json:"patch_sha256"`
		License        string            `json:"license"`
		Completeness   string            `json:"completeness"`
	}

	// ForeignTestInputs pins omitted upstream fixtures for isolated development
	// tests; it does not change the canonical module ZIP source contract.
	ForeignTestInputs struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
	}
)

// ForeignSourceRecord is the sole source declaration used by ownership and dependency gates.
const (
	ForeignSourceRecord   = "third_party/pdf-sources.json"
	maxForeignRecordBytes = 1 << 16
	goModuleFile          = "go.mod"
	foreignLicenseFile    = "LICENSE"
	foreignReadFailure    = "read foreign input %s: %w"
)

var errForeignSource = errors.New("foreign source identity")

// ForeignSourcesContext validates the three exact declared foreign roots before granting their
// ownership boundary, under the calling operation lifetime. Roots without either foreign tree or
// the record retain ordinary source discovery.
func ForeignSourcesContext(ctx context.Context, root string) ([]ForeignSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("foreign source verification canceled: %w", err)
	}

	var sources []ForeignSource

	err := withRoot(root, func(tree *os.Root) error {
		var readErr error

		sources, readErr = foreignSourcesInRoot(ctx, tree)

		return readErr
	})
	if err != nil {
		return nil, err
	}

	return sources, nil
}

func foreignSourcesInRoot(ctx context.Context, tree *os.Root) ([]ForeignSource, error) {
	present, err := presentForeignRoots(tree)
	if err != nil {
		return nil, err
	}

	recordInfo, statErr := tree.Lstat(ForeignSourceRecord)
	if statErr == nil && recordInfo.Size() > maxForeignRecordBytes {
		return nil, fmt.Errorf("%w: source record exceeds %d bytes", errForeignSource, maxForeignRecordBytes)
	}

	record, err := foreignRegularBytes(tree, ForeignSourceRecord)
	if errors.Is(err, fs.ErrNotExist) && len(present) == 0 {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("%w: source record: %w", errForeignSource, err)
	}

	sources, err := decodeForeignSources(record, present)
	if err != nil {
		return nil, err
	}

	for index := range sources {
		if verifyErr := verifyForeignSource(ctx, tree, sources[index]); verifyErr != nil {
			return nil, fmt.Errorf("%w: %s: %w", errForeignSource, sources[index].Root, verifyErr)
		}
	}

	return sources, nil
}

func foreignApprovedRoots() []string {
	return []string{"third_party/benoitkugler/pdf", "third_party/benoitkugler/pstokenizer", "third_party/pdfcpu/pdfcpu"}
}

func presentForeignRoots(tree *os.Root) (map[string]bool, error) {
	present := map[string]bool{}

	for _, root := range foreignApprovedRoots() {
		_, err := tree.Lstat(root)
		if err == nil {
			present[root] = true
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: inspect %s: %w", errForeignSource, root, err)
		}
	}

	return present, nil
}

func decodeForeignSources(record []byte, present map[string]bool) ([]ForeignSource, error) {
	decoder := json.NewDecoder(bytes.NewReader(record))
	decoder.DisallowUnknownFields()

	var sources []ForeignSource
	if err := decoder.Decode(&sources); err != nil {
		return nil, fmt.Errorf("%w: decode record: %w", errForeignSource, err)
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: record requires one JSON value and EOF", errForeignSource)
	}

	if len(sources) == 0 || len(sources) > len(foreignApprovedRoots()) {
		return nil, fmt.Errorf("%w: record needs one to three exact module roots", errForeignSource)
	}

	seen := map[string]bool{}

	for index := range sources {
		source := &sources[index]
		if err := validateForeignRecord(*source); err != nil {
			return nil, err
		}

		if seen[source.Root] || !present[source.Root] {
			return nil, fmt.Errorf("%w: duplicate or missing declared root %s", errForeignSource, source.Root)
		}

		seen[source.Root] = true
	}

	for root := range present {
		if !seen[root] {
			return nil, fmt.Errorf("%w: undeclared foreign root %s", errForeignSource, root)
		}
	}

	return sources, nil
}

func validateForeignRecord(source ForeignSource) error {
	if !slices.Contains(foreignApprovedRoots(), source.Root) {
		return fmt.Errorf("%w: unsupported root %q; only approved exact module roots may be foreign", errForeignSource, source.Root)
	}

	expectedModule := "github.com/" + strings.TrimPrefix(source.Root, "third_party/")
	if source.Module != expectedModule || source.Repository != "https://"+source.Module || !strings.HasPrefix(source.Version, "v") ||
		strings.ContainsAny(source.Version, "/\\ \n\r") {
		return fmt.Errorf("%w: invalid module/version/repository identity for %s", errForeignSource, source.Root)
	}

	if err := validateForeignArtifactRoles(source); err != nil {
		return err
	}

	if err := validateForeignTestInputs(source); err != nil {
		return err
	}

	return validateForeignPins(source)
}

func validateForeignTestInputs(source ForeignSource) error {
	if source.TestInputs == (ForeignTestInputs{}) {
		return nil
	}

	revision, err := hex.DecodeString(source.Revision)
	if err != nil || len(revision) != 20 ||
		source.TestInputs.URL != "https://codeload.github.com/"+strings.TrimPrefix(
			source.Module,
			"github.com/",
		)+"/tar.gz/"+source.Revision {
		return fmt.Errorf("%w: test inputs require the exact declared Git origin and commit", errForeignSource)
	}

	digest, err := hex.DecodeString(source.TestInputs.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return fmt.Errorf("%w: test inputs need a complete SHA-256 pin", errForeignSource)
	}

	return nil
}

func validateForeignPins(source ForeignSource) error {
	for _, digest := range []string{source.SourceSHA256, source.PatchSHA256} {
		data, err := hex.DecodeString(digest)
		if err != nil || len(data) != sha256.Size {
			return fmt.Errorf("%w: invalid SHA-256 pin", errForeignSource)
		}
	}

	if source.ModuleSum == "" || source.GoModSum == "" || source.Revision == "" ||
		source.Completeness != "Go module ZIP plus exact local patch; not a full Git tree claim" {
		return fmt.Errorf("%w: incomplete module ZIP provenance", errForeignSource)
	}

	return nil
}

func validateForeignArtifactRoles(source ForeignSource) error {
	if !foreignArtifactPath(source.SourceArtifact, source.Root, "base/", ".zip") ||
		!foreignArtifactPath(source.Patch, source.Root, "patches/", ".patch") {
		return fmt.Errorf("%w: artifacts must be rooted in their declared roles", errForeignSource)
	}

	license := path.Base(source.License)
	if !fs.ValidPath(source.License) || path.Dir(source.License) != source.Root ||
		license != foreignLicenseFile && license != "LICENSE.txt" {
		return fmt.Errorf("%w: upstream license must name a retained root license", errForeignSource)
	}

	return nil
}

func foreignArtifactPath(name, root, role, suffix string) bool {
	return fs.ValidPath(name) && strings.HasPrefix(name, path.Dir(root)+"/"+role) && strings.HasSuffix(name, suffix)
}

func foreignRegularBytes(tree *os.Root, name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, fmt.Errorf("%w: invalid rooted path %q", errForeignSource, name)
	}

	components := strings.Split(name, "/")
	for index := range components {
		entry, err := tree.Lstat(strings.Join(components[:index+1], "/"))
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", name, err)
		}

		if entry.Mode()&os.ModeSymlink != 0 || index < len(components)-1 && !entry.IsDir() ||
			index == len(components)-1 && !entry.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: %s is not a rooted regular file", errForeignSource, name)
		}
	}

	data, err := tree.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf(foreignReadFailure, name, err)
	}

	return data, nil
}
