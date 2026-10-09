// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type (
	foreignFileIdentity struct {
		digest [32]byte
		size   int64
	}
	foreignReconstruction struct {
		files  map[string]foreignFileIdentity
		source ForeignSource
	}
	foreignReconstructionCache struct {
		roots map[string]foreignReconstruction
		mutex sync.Mutex
	}
)

const (
	foreignPatchTimeout  = 30 * time.Second
	foreignDirectoryMode = 0o700
	foreignFileMode      = 0o600
)

var foreignReconstructions = foreignReconstructionCache{roots: map[string]foreignReconstruction{}}

func verifyForeignSource(ctx context.Context, tree *os.Root, source ForeignSource) error {
	archive, err := foreignRegularBytes(tree, source.SourceArtifact)
	if err != nil {
		return err
	}

	patch, err := foreignRegularBytes(tree, source.Patch)
	if err != nil {
		return err
	}

	if fmt.Sprintf("%x", sha256.Sum256(archive)) != source.SourceSHA256 || fmt.Sprintf("%x", sha256.Sum256(patch)) != source.PatchSHA256 {
		return fmt.Errorf("%w: ZIP or patch SHA-256 differs from pin", errForeignSource)
	}

	expected, err := expectedForeignFiles(ctx, source, archive, patch)
	if err != nil {
		return err
	}

	return compareForeignTree(ctx, tree, source.Root, expected)
}

// Only immutable reconstructed expectations are cached, with at most one slot per allowed root.
// ZIP and patch bytes are checked before lookup; observed source bytes are never cached.
func expectedForeignFiles(ctx context.Context, source ForeignSource, archive, patch []byte) (map[string]foreignFileIdentity, error) {
	foreignReconstructions.mutex.Lock()
	previous, found := foreignReconstructions.roots[source.Root]
	foreignReconstructions.mutex.Unlock()

	if canceled := ctx.Err(); canceled != nil {
		return nil, fmt.Errorf("source reconstruction canceled: %w", canceled)
	}

	if found && previous.source == source {
		return previous.files, nil
	}

	base, err := foreignArchiveFiles(source, archive)
	if err != nil {
		return nil, err
	}

	scratch, err := os.MkdirTemp("", "pdfconcat-foreign-source-")
	if err != nil {
		return nil, fmt.Errorf("create source reconstruction: %w", err)
	}

	expected, buildErr := reconstructForeignFiles(ctx, scratch, base, patch, path.Base(source.License))
	if cleanupErr := errors.Join(buildErr, os.RemoveAll(scratch)); cleanupErr != nil {
		return nil, cleanupErr
	}

	if canceled := ctx.Err(); canceled != nil {
		return nil, fmt.Errorf("source reconstruction canceled: %w", canceled)
	}

	foreignReconstructions.mutex.Lock()
	foreignReconstructions.roots[source.Root] = foreignReconstruction{source: source, files: expected}
	foreignReconstructions.mutex.Unlock()

	return expected, nil
}

func reconstructForeignFiles(
	ctx context.Context,
	scratch string,
	base map[string][]byte,
	patch []byte,
	license string,
) (map[string]foreignFileIdentity, error) {
	if base[license] == nil {
		return nil, fmt.Errorf("%w: module ZIP omits declared upstream license %s", errForeignSource, license)
	}

	for name, data := range base {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("source extraction canceled: %w", err)
		}

		target := filepath.Join(scratch, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), foreignDirectoryMode); err != nil {
			return nil, fmt.Errorf("extract source directory: %w", err)
		}

		if err := os.WriteFile(target, data, foreignFileMode); err != nil {
			return nil, fmt.Errorf("extract source file: %w", err)
		}
	}

	if err := applyForeignPatch(ctx, scratch, patch); err != nil {
		return nil, err
	}

	if err := verifyProtectedForeignFiles(scratch, base, license); err != nil {
		return nil, err
	}

	var expected map[string]foreignFileIdentity

	err := withRoot(scratch, func(tree *os.Root) error {
		var readErr error

		expected, readErr = foreignTreeDigests(ctx, tree, ".", nil)

		return readErr
	})

	return expected, err
}

func verifyProtectedForeignFiles(scratch string, base map[string][]byte, license string) error {
	return withRoot(scratch, func(tree *os.Root) error {
		for _, name := range []string{goModuleFile, "go.sum", license} {
			actual, err := tree.ReadFile(name)
			if errors.Is(err, fs.ErrNotExist) && base[name] == nil {
				continue
			}

			if err != nil {
				return fmt.Errorf("read protected source input %s: %w", name, err)
			}

			if !bytes.Equal(actual, base[name]) {
				return fmt.Errorf("%w: patch changes protected upstream %s", errForeignSource, name)
			}
		}

		return nil
	})
}

func applyForeignPatch(ctx context.Context, scratch string, patch []byte) (result error) {
	ctx, cancel := context.WithTimeout(ctx, foreignPatchTimeout)
	defer cancel()

	// Git's config reader does not consistently accept Windows' NUL device.
	// Keep the empty regular config outside the reconstructed source inventory.
	configDirectory, err := os.MkdirTemp("", "pdfconcat-git-config-")
	if err != nil {
		return fmt.Errorf("create isolated Git config: %w", err)
	}
	defer func() { result = errors.Join(result, os.RemoveAll(configDirectory)) }()

	config := filepath.Join(configDirectory, "config")
	if err = os.WriteFile(config, nil, foreignFileMode); err != nil {
		return fmt.Errorf("write isolated Git config: %w", err)
	}

	commands := []*exec.Cmd{
		exec.CommandContext(ctx, "git", "apply", "--check", "--whitespace=nowarn", "-"),
		exec.CommandContext(ctx, "git", "apply", "--whitespace=nowarn", "-"),
	}
	for _, command := range commands {
		command.Dir = scratch

		command.Stdin = bytes.NewReader(patch)
		for _, entry := range command.Environ() {
			if !strings.HasPrefix(entry, "GIT_") {
				command.Env = append(command.Env, entry)
			}
		}

		command.Env = append(command.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+config)
		if output, commandErr := command.CombinedOutput(); commandErr != nil {
			return fmt.Errorf("%w: exact git apply (no fuzz): %w: %s", errForeignSource, commandErr, strings.TrimSpace(string(output)))
		}
	}

	return nil
}

func compareForeignTree(ctx context.Context, tree *os.Root, root string, expected map[string]foreignFileIdentity) error {
	actual, err := foreignTreeDigests(ctx, tree, root, expected)
	if err != nil {
		return err
	}

	for name := range expected {
		if _, found := actual[name]; !found {
			return fmt.Errorf("%w: missing source file %s/%s", errForeignSource, root, name)
		}
	}

	return nil
}

func foreignTreeDigests(
	ctx context.Context,
	tree *os.Root,
	root string,
	expected map[string]foreignFileIdentity,
) (map[string]foreignFileIdentity, error) {
	directories := map[string]bool{".": true}

	for name := range expected {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			directories[dir] = true
		}
	}

	actual := map[string]foreignFileIdentity{}

	err := fs.WalkDir(tree.FS(), root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk source tree: %w", walkErr)
		}

		if err := ctx.Err(); err != nil {
			return fmt.Errorf("source verification canceled: %w", err)
		}

		relative := foreignRelativePath(root, name)
		if shapeErr := foreignTreeEntryShape(name, relative, entry, expected, directories); shapeErr != nil {
			return shapeErr
		}

		if entry.IsDir() {
			return nil
		}

		identity, fileErr := foreignFileIdentityOf(tree, name, relative, expected)
		if fileErr != nil {
			return fileErr
		}

		actual[relative] = identity

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("inspect complete foreign source tree: %w", err)
	}

	return actual, nil
}

func foreignRelativePath(root, name string) string {
	if name == root {
		return "."
	}

	if root == "." {
		return name
	}

	return strings.TrimPrefix(name, root+"/")
}

func foreignTreeEntryShape(
	name, relative string,
	entry fs.DirEntry,
	expected map[string]foreignFileIdentity,
	directories map[string]bool,
) error {
	if entry.Type()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: source symlink %s", errForeignSource, name)
	}

	if entry.IsDir() && expected != nil && !directories[relative] {
		return fmt.Errorf("%w: extra source directory %s", errForeignSource, name)
	}

	return nil
}

func foreignFileIdentityOf(tree *os.Root, name, relative string, expected map[string]foreignFileIdentity) (foreignFileIdentity, error) {
	if expected != nil {
		want, found := expected[relative]
		if !found {
			return foreignFileIdentity{}, fmt.Errorf("%w: extra source file %s", errForeignSource, name)
		}

		info, statErr := tree.Lstat(name)
		if statErr != nil {
			return foreignFileIdentity{}, fmt.Errorf("inspect source file: %w", statErr)
		}

		if info.Size() != want.size {
			return foreignFileIdentity{}, fmt.Errorf("%w: changed source file size %s", errForeignSource, name)
		}
	}

	data, err := foreignRegularBytes(tree, name)
	if err != nil {
		return foreignFileIdentity{}, err
	}

	identity := foreignFileIdentity{digest: sha256.Sum256(data), size: int64(len(data))}

	if expected != nil {
		want := expected[relative]
		if want.digest != identity.digest || want.size != identity.size {
			return foreignFileIdentity{}, fmt.Errorf("%w: changed source file %s", errForeignSource, name)
		}
	}

	return identity, nil
}
