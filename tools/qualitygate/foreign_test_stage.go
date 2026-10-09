// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

type foreignTestStage struct {
	goCache   string
	directory string
	modfile   string
	roots     map[string]string
	identity  map[string]foreignFixture
	authority map[string][32]byte
	sources   []repopolicy.ForeignSource
}

const (
	foreignStageOutput        = "test-output"
	foreignModuleSums         = "go.sum"
	foreignRaceFlag           = "-race"
	foreignWorkspaceOff       = "GOWORK=off"
	foreignStageDirectoryMode = 0o700
)

func prepareForeignTests(ctx context.Context, root string) (*foreignTestStage, error) {
	sources, err := repopolicy.ForeignSourcesContext(ctx, root)
	if err != nil {
		return nil, err
	}

	stage := &foreignTestStage{roots: map[string]string{}, sources: sources}

	cache, cacheErr := goCommand(root, goEnvVerb, "GOCACHE").output(ctx)
	if cacheErr != nil {
		return nil, cacheErr
	}

	stage.goCache = strings.TrimSpace(cache)
	if len(sources) == 0 {
		return stage, nil
	}

	stage.directory, err = os.MkdirTemp("", "pdfconcat-foreign-tests-")
	if err != nil {
		return nil, fmt.Errorf("create owned foreign test stage: %w", err)
	}

	stage.authority, err = stage.authorityIdentity(root)
	if err != nil {
		return nil, errors.Join(err, stage.cleanup())
	}

	if operationErr := stage.populate(ctx, root); operationErr != nil {
		return nil, errors.Join(operationErr, stage.cleanup())
	}

	return stage, nil
}

func (stage *foreignTestStage) populate(ctx context.Context, root string) error {
	for index := range stage.sources {
		source := &stage.sources[index]

		destination := filepath.Join(stage.directory, filepath.FromSlash(source.Root))
		if err := os.MkdirAll(destination, foreignStageDirectoryMode); err != nil {
			return fmt.Errorf("create foreign module stage: %w", err)
		}

		if err := repopolicy.ReconstructForeignModule(ctx, root, *source, destination); err != nil {
			return err
		}

		stage.roots[filepath.Join(root, filepath.FromSlash(source.Root))] = destination
	}

	baseline, err := foreignStageIdentity(ctx, stage.directory)
	if err != nil {
		return err
	}

	stage.identity = baseline
	for index := range stage.sources {
		source := &stage.sources[index]
		destination := stage.roots[filepath.Join(root, filepath.FromSlash(source.Root))]

		fixtures, fixtureErr := supplementForeignFixtures(ctx, root, destination, *source)
		if fixtureErr != nil {
			return fixtureErr
		}

		stage.expectFixtures(source.Root, fixtures)
	}

	if moduleErr := stage.deriveModuleFiles(root); moduleErr != nil {
		return moduleErr
	}

	return stage.verify(ctx, root)
}

func (stage *foreignTestStage) deriveModuleFiles(root string) error {
	if stage.identity == nil {
		stage.identity = map[string]foreignFixture{}
	}

	module, err := readInRoot(root, moduleFileName)
	if err != nil {
		return err
	}

	text := string(module)

	for index := range stage.sources {
		source := &stage.sources[index]

		original := "replace " + source.Module + " => ./" + source.Root
		if strings.Count(text, original+"\n") != 1 {
			return fmt.Errorf("%w: expected single declared replacement for %s", errGate, source.Module)
		}

		destination := stage.roots[filepath.Join(root, filepath.FromSlash(source.Root))]
		text = strings.Replace(
			text,
			original+"\n",
			"replace "+source.Module+" => "+fmt.Sprintf("%q", filepath.ToSlash(destination))+"\n",
			1,
		)
	}

	stage.modfile = filepath.Join(stage.directory, "tests.mod")
	if operationErr := os.WriteFile(stage.modfile, []byte(text), fileMode); operationErr != nil {
		return fmt.Errorf("write command-local module file: %w", operationErr)
	}

	stage.identity["tests.mod"] = foreignFixture{digest: fmt.Sprintf("%x", sha256.Sum256([]byte(text))), size: int64(len(text))}

	sum, err := readInRoot(root, foreignModuleSums)
	if err != nil {
		return err
	}

	if operationErr := os.WriteFile(filepath.Join(stage.directory, "tests.sum"), sum, fileMode); operationErr != nil {
		return fmt.Errorf("write command-local module sums: %w", operationErr)
	}

	stage.identity["tests.sum"] = foreignFixture{digest: fmt.Sprintf("%x", sha256.Sum256(sum)), size: int64(len(sum))}
	if directoryErr := os.Mkdir(filepath.Join(stage.directory, foreignStageOutput), foreignStageDirectoryMode); directoryErr != nil {
		return fmt.Errorf("create owned test output: %w", directoryErr)
	}

	return nil
}

func (stage *foreignTestStage) flags(options testOptions) []string {
	return nativeTestFlags(options, stage.modfile)
}

func nativeTestFlags(options testOptions, modfile string) []string {
	var flags []string
	if modfile != "" {
		flags = append(flags, "-modfile", modfile, "-mod=readonly")
	}

	if options.race {
		flags = append(flags, foreignRaceFlag)
	}

	return flags
}

func (stage *foreignTestStage) environment() []string {
	env := []string{"GOFLAGS=" + strings.TrimSpace(os.Getenv("GOFLAGS")+" -mod=readonly"), foreignWorkspaceOff}

	if stage.directory != "" {
		output := filepath.Join(stage.directory, foreignStageOutput)
		env = append(env, "TMPDIR="+output, "TMP="+output, "TEMP="+output)
	}

	return env
}

func (stage *foreignTestStage) verify(ctx context.Context, root string) error {
	if stage.directory == "" {
		return nil
	}

	observed, err := repopolicy.ForeignSourcesContext(ctx, root)
	if err != nil {
		return err
	}

	if !slices.Equal(stage.sources, observed) {
		return fmt.Errorf("%w: declared foreign source values changed during staging/test", errGate)
	}

	authority, err := stage.authorityIdentity(root)
	if err != nil {
		return err
	}

	if !maps.Equal(stage.authority, authority) {
		return fmt.Errorf("%w: canonical module/source authority changed during test", errGate)
	}

	after, err := foreignStageIdentity(ctx, stage.directory)
	if err != nil {
		return err
	}

	if !maps.Equal(stage.identity, after) {
		return fmt.Errorf("%w: foreign staged source/fixtures/module files changed during execution", errGate)
	}

	return nil
}

func (stage *foreignTestStage) cleanup() error {
	if stage.directory == "" {
		return nil
	}

	if err := os.RemoveAll(stage.directory); err != nil {
		return fmt.Errorf("remove owned foreign test stage: %w", err)
	}

	return nil
}

func foreignStageIdentity(ctx context.Context, directory string) (map[string]foreignFixture, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("open foreign stage identity: %w", err)
	}
	defer closeLogged(root)

	identity := map[string]foreignFixture{}

	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if cancellation := ctx.Err(); cancellation != nil {
			return fmt.Errorf("stage identity canceled: %w", cancellation)
		}

		if name == foreignStageOutput && entry.IsDir() {
			return fs.SkipDir
		}

		if entry.IsDir() {
			identity[name+"/"] = foreignFixture{digest: "directory"}
			return nil
		}

		value, fileErr := foreignStageFileIdentity(ctx, root, name, entry)
		if fileErr != nil {
			return fileErr
		}

		identity[name] = value

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("verify foreign stage input tree: %w", err)
	}

	return identity, nil
}

func (stage *foreignTestStage) authorityIdentity(root string) (map[string][32]byte, error) {
	baseNames := []string{moduleFileName, foreignModuleSums, repopolicy.ForeignSourceRecord}
	names := make([]string, 0, len(baseNames)+2*len(stage.sources))
	names = append(names, baseNames...)

	for index := range stage.sources {
		source := &stage.sources[index]
		names = append(names, source.SourceArtifact, source.Patch)
	}

	identity := map[string][32]byte{}

	for _, name := range names {
		data, err := readInRoot(root, name)
		if err != nil {
			return nil, err
		}

		identity[name] = sha256.Sum256(data)
	}

	return identity, nil
}

func supplementForeignFixtures(
	ctx context.Context,
	root, destination string,
	source repopolicy.ForeignSource,
) (map[string]foreignFixture, error) {
	if source.TestInputs.URL == "" {
		return map[string]foreignFixture{}, nil
	}

	artifact, err := foreignTestArtifact(ctx, source.TestInputs)
	if err != nil {
		return nil, err
	}

	return stageForeignFixtures(ctx, root, artifact, destination, source)
}

func foreignStageFileIdentity(ctx context.Context, root *os.Root, name string, entry fs.DirEntry) (foreignFixture, error) {
	info, err := entry.Info()
	if err != nil {
		return foreignFixture{}, fmt.Errorf("inspect staged input: %w", err)
	}

	if !info.Mode().IsRegular() {
		return foreignFixture{}, fmt.Errorf("%w: foreign stage input not regular: %s", errGate, name)
	}

	file, err := root.Open(name)
	if err != nil {
		return foreignFixture{}, fmt.Errorf("open staged input: %w", err)
	}

	digest := sha256.New()

	n, readErr := io.Copy(digest, &foreignContextReader{cancellation: ctx.Err, source: file})
	if closeErr := file.Close(); readErr != nil || closeErr != nil {
		return foreignFixture{}, fmt.Errorf("hash staged input: %w", errors.Join(readErr, closeErr))
	}

	return foreignFixture{digest: hex.EncodeToString(digest.Sum(nil)), size: n}, nil
}

func (stage *foreignTestStage) expectFixtures(moduleRoot string, fixtures map[string]foreignFixture) {
	for name, value := range fixtures {
		stagedName := filepath.ToSlash(filepath.Join(filepath.FromSlash(moduleRoot), filepath.FromSlash(name)))

		stage.identity[stagedName] = value
		for parent := path.Dir(stagedName); parent != "."; parent = path.Dir(parent) {
			stage.identity[parent+"/"] = foreignFixture{digest: "directory"}
		}
	}
}
