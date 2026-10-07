// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package exectest builds the pdfconcat executable for tests that run it as a child process, and
// lets the coverage gate (tools/qualitygate coverage) see what those child processes execute.
//
// When the environment variable PDFCONCAT_COVERDIR names a directory, Build compiles with
// `go build -cover` (instrumenting the packages listed in PDFCONCAT_COVERPKG, default every package
// of the module) and Command points the child's GOCOVERDIR at that directory. Otherwise the
// executable is a plain build and nothing changes.
package exectest

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type (
	// buildCache holds the executables built so far in this test process.
	buildCache struct {
		built map[string]string
		dir   string
		mu    sync.Mutex
	}
)

const (
	// EnvCoverDir is the directory that receives the child processes' coverage data.
	EnvCoverDir = "PDFCONCAT_COVERDIR"
	// EnvCoverPkg is the comma-separated list of packages the executable is instrumented for.
	EnvCoverPkg = "PDFCONCAT_COVERPKG"
)

var (
	errBuild = errors.New("exectest")

	// cache is process-wide on purpose: building the executable once per test process, not once per
	// test, is what keeps executable tests fast.
	cache = buildCache{built: map[string]string{}}
)

// Build compiles the main package pkg (for example "./cmd/pdfconcat", relative to the module root)
// once per test process and returns the executable's path.
func Build(tb testing.TB, pkg string) string {
	tb.Helper()

	cache.mu.Lock()
	defer cache.mu.Unlock()

	instrumented := os.Getenv(EnvCoverDir) != ""
	key := pkg + " instrumented=" + strconv.FormatBool(instrumented)

	if path, found := cache.built[key]; found {
		return path
	}

	root, err := moduleRoot()
	if err != nil {
		tb.Fatal(err)
	}

	if cache.dir == "" {
		cache.dir, err = os.MkdirTemp("", "exectest-")
		if err != nil {
			tb.Fatal(err)
		}
	}

	name := filepath.Base(pkg)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	target := filepath.Join(cache.dir, strconv.Itoa(len(cache.built)), name)
	args := []string{"build", "-o", target}

	if instrumented {
		coverPkg := os.Getenv(EnvCoverPkg)
		if coverPkg == "" {
			coverPkg = "./..."
		}

		args = append(args, "-cover", "-covermode=atomic", "-coverpkg="+coverPkg)
	}

	build := exec.CommandContext(tb.Context(), "go", append(args, pkg)...)
	build.Dir = root

	output, err := build.CombinedOutput()
	if err != nil {
		tb.Fatalf("go %s: %v\n%s", strings.Join(append(args, pkg), " "), err, output)
	}

	cache.built[key] = target

	return target
}

// Cleanup removes every executable Build produced. Call it from TestMain after m.Run; a test
// process that does not leaves its temporary directory behind.
func Cleanup() {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if cache.dir == "" {
		return
	}

	err := os.RemoveAll(cache.dir)
	if err != nil {
		log.Printf("exectest: cannot remove %s: %v", cache.dir, err)
	}

	cache.dir = ""
	cache.built = map[string]string{}
}

// Command returns a command that runs the executable at path. Under the coverage gate the child's
// coverage data goes to EnvCoverDir.
func Command(ctx context.Context, path string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, path, args...)
	command.Env = os.Environ()

	if dir := os.Getenv(EnvCoverDir); dir != "" {
		command.Env = append(command.Env, "GOCOVERDIR="+dir)
	}

	return command
}

// moduleRoot returns the directory of the module containing the test's working directory.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}

	for {
		_, statErr := os.Stat(filepath.Join(dir, "go.mod"))
		if statErr == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%w: go.mod not found above %s", errBuild, dir)
		}

		dir = parent
	}
}
