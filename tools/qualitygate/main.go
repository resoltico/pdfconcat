// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Command qualitygate runs the repository's quality gates that need more than `go test`: lint
// configuration and staleness checks against the central exception registry, test discovery,
// merged coverage, bounded fuzzing, mutation testing with its negative controls, and release
// archive inspection. Run it from anywhere inside the repository.
//
//	go run ./tools/qualitygate <command> [flags]
//
// Run without arguments to list the available gates and their flags.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const (
	architectureCommand = "architecture"
	lintCommand         = "lint"
	formatCommand       = "format"

	runSelectionFlag  = "-run"
	jsonFlag          = "-json"
	readonlyGoFlags   = "GOFLAGS=-mod=readonly"
	configFlag        = "--config"
	enableOnlyFlag    = "--enable-only"
	dependencyLinter  = "depguard"
	securityLinter    = "gosec"
	lintTool          = "golangci-lint"
	serialLintRunners = "--allow-serial-runners"

	usage = `usage: go run ./tools/qualitygate <command> [flags]

commands:
  architecture  verify complete production ownership and real import-boundary controls
  lint          run configured lint over every owned compiled package, including fixtures
  format        verify formatting of every owned Go source directory
  lint-config   verify .golangci.yml against .quality-exceptions.yml and the pinned golangci-lint
  lint-stale    verify every lint diagnostic exclusion still matches a real diagnostic
  test          run go test with discovery checks (flags: -race -run -require -timeout)
  coverage      run unit and executable-subprocess coverage, merge, apply the registry, enforce the threshold
  fuzz          discover and run every fuzz target (flag: -time)
  mutation      run mutation testing in a clean snapshot and judge it against the registry
  controls      prove the tests detect deliberate mutations listed in tools/mutation-controls.yml
  secrets       scan current source and reachable history for credentials
  release-notes extract the exact dated CHANGELOG section (-tag/-output)
  release-version validate the configured project version and release tag (-tag or -snapshot)
  release-bytes verify actual remote draft bytes against the attested local artifact manifest
  archives      inspect release archives in a directory (default dist)
`

	archivesCommand       = "archives"
	versionVerb           = "version"
	parseFlagsError       = "parse flags: %w"
	readFileError         = "read %s: %w"
	pathError             = "%s: %w"
	openFileError         = "open %s: %w"
	moduleFileName        = "go.mod"
	controlsCommand       = "controls"
	allPackages           = "./..."
	goListVerb            = "list"
	coverageCommand       = "coverage"
	loadRegistryError     = "load registry: %w"
	lintConfigCommand     = "lint-config"
	lintStaleCommand      = "lint-stale"
	lintConfigFileName    = ".golangci.yml"
	scratchLintConfigName = "golangci.yml"
	lintReportFileName    = "report.json"
	runVerb               = "run"
	fuzzCommand           = "fuzz"
	mutationCommand       = "mutation"

	// File modes of what the gates create: private directories and files, and executables.
	dirMode  = 0o750
	fileMode = 0o600
	execMode = 0o700

	// goTool is the Go command every gate drives.
	goTool  = "go"
	gitTool = "git"
	// testVerb is the go subcommand that runs tests.
	testVerb = "test"
	// countOnce disables test result caching.
	countOnce = "-count=1"
	// windowsOS and exeSuffix identify Windows executables.
	windowsOS = "windows"
	exeSuffix = ".exe"

	registryName        = ".quality-exceptions.yml"
	releaseBytesCommand = "release-bytes"
	checksumFile        = "checksums.txt"

	// firstLineParts splits off the first line of a multi-line message.
	firstLineParts = 2
)

var (
	// errGate is wrapped by every failure this command reports on its own account.
	errGate = errors.New("quality gate")

	modulePattern = regexp.MustCompile(`(?m)^module (\S+)$`)
)

func main() {
	log.SetFlags(0)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	err := dispatch(ctx, os.Args[1:])

	stop()

	if err != nil {
		log.Print("qualitygate: ", err)
		os.Exit(1)
	}
}

func dispatch(ctx context.Context, args []string) error {
	if len(args) == 0 {
		log.Print(usage)

		return fmt.Errorf("%w: a command is required", errGate)
	}

	command, rest := args[0], args[1:]

	switch command {
	case secretsCommand:
		return runSecrets(ctx, rest)
	case architectureCommand:
		return architecture(ctx, rest)
	case lintCommand, formatCommand, lintConfigCommand, lintStaleCommand:
		return dispatchLint(ctx, command, rest)
	case testVerb:
		return runTests(ctx, rest)
	case coverageCommand:
		return runCoverage(ctx, rest)
	case fuzzCommand:
		return runFuzz(ctx, rest)
	case mutationCommand:
		return runMutation(ctx, rest)
	case controlsCommand:
		return runControls(ctx, rest)
	case releaseVersionCommand, releaseNotesCommand, releaseBytesCommand, archivesCommand:
		return dispatchRelease(ctx, command, rest)
	default:
		log.Print(usage)

		return fmt.Errorf("%w: unknown command %q", errGate, command)
	}
}

// newFlags returns a flag set that reports errors instead of exiting.
func newFlags(name string) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(log.Writer())

	return set
}

// repoRoot returns the nearest ancestor of the working directory that holds the registry.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}

	for {
		_, statErr := os.Stat(filepath.Join(dir, registryName))
		if statErr == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%w: %s not found above the working directory", errGate, registryName)
		}

		dir = parent
	}
}

// modulePath reads the module path from root/go.mod.
func modulePath(root string) (string, error) {
	content, err := readInRoot(root, moduleFileName)
	if err != nil {
		return "", err
	}

	match := modulePattern.FindSubmatch(content)
	if match == nil {
		return "", fmt.Errorf("%w: go.mod has no module line", errGate)
	}

	return string(match[1]), nil
}

// readInRoot reads a slash-separated file below dir through an [os.Root], so that a symbolic link
// cannot lead the read outside dir.
func readInRoot(dir, file string) ([]byte, error) {
	tree, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf(openFileError, dir, err)
	}

	content, readErr := tree.ReadFile(filepath.FromSlash(file))
	if readErr != nil {
		readErr = fmt.Errorf(readFileError, file, readErr)
	}

	return content, errors.Join(readErr, tree.Close())
}

// fileReader returns a repopolicy.SourceReader over the files below root.
func fileReader(root string) func(string) ([]byte, error) {
	return func(file string) ([]byte, error) {
		return readInRoot(root, file)
	}
}

// removeAll deletes a scratch path, logging a failure that cannot change the gate's verdict.
func removeAll(path string) {
	err := os.RemoveAll(path)
	if err != nil {
		log.Printf("qualitygate: cannot remove scratch path %s: %v", path, err)
	}
}

// closeLogged closes a handle whose close error cannot change the gate's verdict.
func closeLogged(closer io.Closer) {
	err := closer.Close()
	if err != nil {
		log.Printf("qualitygate: close: %v", err)
	}
}

// indent prefixes every line of text for nested log output.
func indent(text string) string {
	return "    " + strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", "\n    ")
}

func dispatchLint(ctx context.Context, name string, args []string) error {
	switch name {
	case lintCommand:
		return lintOwned(ctx, args)
	case formatCommand:
		return formatOwned(ctx, args)
	case lintConfigCommand:
		return lintConfig(ctx, args)
	case lintStaleCommand:
		return lintStale(ctx, args)
	default:
		return fmt.Errorf("%w: unknown lint action %s", errGate, name)
	}
}

func dispatchRelease(ctx context.Context, name string, args []string) error {
	switch name {
	case releaseNotesCommand:
		return writeReleaseNotes(ctx, args)
	case releaseVersionCommand:
		return checkReleaseVersion(ctx, args)
	case releaseBytesCommand:
		return verifyRelease(ctx, args)
	case archivesCommand:
		return inspectArchives(ctx, args)
	default:
		return fmt.Errorf("%w: unknown release action %s", errGate, name)
	}
}
