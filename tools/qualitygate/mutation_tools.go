// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type preparedMutationTools struct {
	SourceSHA   string `json:"source_sha"`
	RunID       string `json:"run_id"`
	PatchSHA    string `json:"patch_sha256"`
	GoVersion   string `json:"go_version"`
	GOOS        string `json:"goos"`
	GOARCH      string `json:"goarch"`
	GremlinsSHA string `json:"gremlins_sha256"`
	LinterSHA   string `json:"golangci_lint_sha256"`
}

func preparedToolFilename(name string) string {
	if runtime.GOOS == windowsOS {
		return name + exeSuffix
	}
	return name
}

func preparedToolsAuthority(ctx context.Context, root string) (preparedMutationTools, error) {
	source, run := os.Getenv("GITHUB_SHA"), os.Getenv("GITHUB_RUN_ID")
	if len(source) != 40 || strings.Trim(source, "0123456789abcdef") != "" || run == "" || strings.Trim(run, "0123456789") != "" || strings.HasPrefix(run, "0") {
		return preparedMutationTools{}, fmt.Errorf("%w: exact GitHub source/run identity required", errGate)
	}
	head, err := (&command{dir: root, name: gitTool, args: []string{"rev-parse", "HEAD"}}).output(ctx)
	if err != nil || strings.TrimSpace(head) != source {
		return preparedMutationTools{}, errors.Join(fmt.Errorf("%w: tool source differs from workflow source", errGate), err)
	}
	dirty, err := (&command{dir: root, name: gitTool, args: []string{"status", "--porcelain"}}).output(ctx)
	if err != nil || strings.TrimSpace(dirty) != "" {
		return preparedMutationTools{}, errors.Join(fmt.Errorf("%w: tool source is not clean", errGate), err)
	}
	versions, err := readToolVersions(root)
	if err != nil {
		return preparedMutationTools{}, err
	}
	return preparedMutationTools{SourceSHA: source, RunID: run, PatchSHA: versions["GREMLINS_PATCH_SHA256"], GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}, nil
}

// Only these two native commands are delivered; never cache/compiler workspaces.
func hashPreparedTools(root string, receipt preparedMutationTools) (result preparedMutationTools, failure error) {
	tree, err := os.OpenRoot(root)
	if err != nil {
		return result, err
	}
	defer func() { failure = errors.Join(failure, tree.Close()) }()
	for _, dir := range []string{".tools", ".tools/bin"} {
		info, err := tree.Lstat(filepath.FromSlash(dir))
		if err != nil {
			return result, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return result, fmt.Errorf("%w: prepared tool directory is not an actual directory", errGate)
		}
	}
	entries, err := fs.ReadDir(tree.FS(), ".tools/bin")
	if err != nil {
		return result, err
	}
	expected := map[string]*string{preparedToolFilename("gremlins"): &receipt.GremlinsSHA, preparedToolFilename(lintTool): &receipt.LinterSHA}
	if len(entries) != len(expected) {
		return result, fmt.Errorf("%w: prepared tool file inventory differs", errGate)
	}
	for _, entry := range entries {
		digest, found := expected[entry.Name()]
		if !found {
			return result, fmt.Errorf("%w: unexpected prepared tool %q", errGate, entry.Name())
		}
		file, err := regularArtifactReader(filepath.Join(root, ".tools", "bin"))(entry.Name())
		if err != nil {
			return result, err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		if err := errors.Join(copyErr, file.Close()); err != nil {
			return result, err
		}
		*digest = hex.EncodeToString(hash.Sum(nil))
	}
	return receipt, nil
}

func validatePreparedTools(received, authority preparedMutationTools) error {
	identity := received
	identity.GremlinsSHA, identity.LinterSHA = "", ""
	if identity != authority {
		return fmt.Errorf("%w: prepared tools source/run/pin/Go/native identity differs", errGate)
	}
	for _, value := range []string{received.GremlinsSHA, received.LinterSHA} {
		raw, err := hex.DecodeString(value)
		if err != nil || len(raw) != sha256.Size || value != strings.ToLower(value) {
			return fmt.Errorf("%w: incomplete prepared tool digest", errGate)
		}
	}
	return nil
}

func verifyPreparedTools(root string, received, authority preparedMutationTools) error {
	if err := validatePreparedTools(received, authority); err != nil {
		return err
	}
	actual, err := hashPreparedTools(root, authority)
	if err != nil {
		return err
	}
	if actual != received {
		return fmt.Errorf("%w: prepared tool bytes differ from trusted producer output", errGate)
	}
	tree, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	var modeErr error
	for _, name := range []string{"gremlins", lintTool} {
		modeErr = errors.Join(modeErr, tree.Chmod(filepath.Join(".tools", "bin", preparedToolFilename(name)), execMode))
	}
	return errors.Join(modeErr, tree.Close())
}

func runPreparedMutationTools(ctx context.Context, args []string, record bool) error {
	if len(args) != 0 {
		return fmt.Errorf("%w: prepared mutation tools accepts no arguments", errGate)
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	authority, err := preparedToolsAuthority(ctx, root)
	if err != nil {
		return err
	}
	if !record {
		var received preparedMutationTools
		if err := json.Unmarshal([]byte(os.Getenv("PDFCONCAT_MUTATION_TOOLS")), &received, json.RejectUnknownMembers(true)); err != nil {
			return fmt.Errorf("%w: trusted producer receipt PDFCONCAT_MUTATION_TOOLS: %w", errGate, err)
		}
		return verifyPreparedTools(root, received, authority)
	}
	receipt, err := hashPreparedTools(root, authority)
	if err != nil {
		return err
	}
	// Existing validators own native/module/version semantics; no artifact-specific copy.
	if _, err := gremlinsBinary(ctx, root, root); err != nil {
		return err
	}
	versions, err := readToolVersions(root)
	if err != nil {
		return err
	}
	if err := checkLinterIdentity(ctx, root, filepath.Join(root, ".tools", "bin", preparedToolFilename(lintTool)), versions); err != nil {
		return err
	}
	for _, name := range []string{"gremlins", lintTool} {
		info, err := buildinfo.ReadFile(filepath.Join(root, ".tools", "bin", preparedToolFilename(name)))
		if err != nil {
			return err
		}
		if info.GoVersion != authority.GoVersion {
			return fmt.Errorf("%w: %s compiler %s differs from workflow Go %s", errGate, name, info.GoVersion, authority.GoVersion)
		}
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	_, err = fmt.Println(string(data))
	return err
}
