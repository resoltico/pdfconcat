// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Command installtools installs the development tools pinned in tools/versions.env into .tools/bin.
//
// golangci-lint and gremlins are built from verified upstream modules with reviewed
// source patches, preserving upstream dependency files. GoReleaser comes from an official
// archive whose digest and, when GitHub CLI is available, immutable-release attestation are checked.
// govulncheck and actionlint publish no
// attested archives, so they are built from source with `go install`, which verifies every module
// against the Go checksum database.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

type (
	// archiveTool describes a tool distributed as an official release archive.
	archiveTool struct {
		// asset returns the archive file name for a platform and version without its leading "v".
		asset func(goos, goarch, version string) string
		// checksums returns the checksums file name for a version without its leading "v".
		checksums func(version string) string
		// member returns the archive path of the executable.
		member         func(goos, goarch, version string) string
		name           string
		versionKey     string
		checksumPinKey string
		repo           string
	}

	// sourceTool describes a tool built from a Go module with `go install`.
	sourceTool struct {
		name       string
		versionKey string
		pkg        string
	}
)

const (
	readFileError = "read %s: %w"
	versionsFile  = "tools/versions.env"
	installDir    = ".tools/bin"
	maxDownload   = 256 << 20
	httpTimeout   = 5 * time.Minute

	dirMode     = 0o750
	privateMode = 0o600
	execMode    = 0o700

	windowsOS      = "windows"
	darwinOS       = "darwin"
	linuxOS        = "linux"
	arm64Arch      = "arm64"
	amd64Arch      = "amd64"
	checksumFields = 2
)

// errInstall is wrapped by every failure this command reports on its own account.
var errInstall = errors.New("install tools")

func archiveTools() []archiveTool {
	return []archiveTool{
		{
			name:       "goreleaser",
			versionKey: "GORELEASER_VERSION",
			repo:       "goreleaser/goreleaser",
			asset: func(goos, goarch, _ string) string {
				return fmt.Sprintf("goreleaser_%s_%s%s", goreleaserOS(goos), goreleaserArch(goarch), archiveSuffix(goos))
			},
			checksums: func(string) string { return "checksums.txt" },
			member:    func(goos, _, _ string) string { return "goreleaser" + executableSuffix(goos) },
		},
		{
			name: "gitleaks", versionKey: "GITLEAKS_VERSION", checksumPinKey: "GITLEAKS_CHECKSUMS_SHA256", repo: "gitleaks/gitleaks",
			asset: func(goos, goarch, version string) string {
				if goarch == amd64Arch {
					goarch = "x64"
				}

				return fmt.Sprintf("gitleaks_%s_%s_%s%s", version, goos, goarch, archiveSuffix(goos))
			},
			checksums: func(version string) string { return "gitleaks_" + version + "_checksums.txt" },
			member:    func(goos, _, _ string) string { return "gitleaks" + executableSuffix(goos) },
		},
	}
}

func sourceTools() []sourceTool {
	return []sourceTool{
		{name: "govulncheck", versionKey: "GOVULNCHECK_VERSION", pkg: "golang.org/x/vuln/cmd/govulncheck"},
		{name: "actionlint", versionKey: "ACTIONLINT_VERSION", pkg: "github.com/rhysd/actionlint/cmd/actionlint"},
	}
}

func archiveSuffix(goos string) string {
	if goos == windowsOS {
		return ".zip"
	}

	return ".tar.gz"
}

func executableSuffix(goos string) string {
	if goos == windowsOS {
		return ".exe"
	}

	return ""
}

func goreleaserOS(goos string) string {
	return strings.ToUpper(goos[:1]) + goos[1:]
}

func goreleaserArch(goarch string) string {
	if goarch == amd64Arch {
		return "x86_64"
	}

	return goarch
}

func main() {
	log.SetFlags(0)

	err := run(context.Background(), os.Args[1:])
	if err != nil {
		log.Print("installtools: ", err)
		os.Exit(1)
	}
}

// run installs the named tools, or every tool when none is named, from the repository root.
func run(ctx context.Context, names []string) error {
	root, err := repositoryRoot()
	if err != nil {
		return err
	}

	content, err := readFile(root, versionsFile)
	if err != nil {
		return err
	}

	versions, err := repopolicy.ParseToolVersions(string(content))
	if err != nil {
		return fmt.Errorf("parse %s: %w", versionsFile, err)
	}

	binDir := filepath.Join(root, filepath.FromSlash(installDir))

	err = os.MkdirAll(binDir, dirMode)
	if err != nil {
		return fmt.Errorf("create %s: %w", installDir, err)
	}

	selected, err := selectTools(names)
	if err != nil {
		return err
	}

	err = installSelected(ctx, root, selected, versions, binDir)
	if err != nil {
		return err
	}

	log.Printf("installed into %s", binDir)

	return nil
}

// installSelected installs every selected tool into binDir.
func installSelected(ctx context.Context, root string, selected map[string]bool, versions map[string]string, binDir string) error {
	if patchedErr := installPatchedTools(ctx, root, selected, versions, binDir); patchedErr != nil {
		return patchedErr
	}

	for _, tool := range archiveTools() {
		if selected[tool.name] {
			err := installArchive(ctx, &tool, versions[tool.versionKey], versions[tool.checksumPinKey], binDir)
			if err != nil {
				return fmt.Errorf("%s: %w", tool.name, err)
			}
		}
	}

	for _, tool := range sourceTools() {
		if selected[tool.name] {
			err := installSource(ctx, tool, versions[tool.versionKey], binDir)
			if err != nil {
				return fmt.Errorf("%s: %w", tool.name, err)
			}
		}
	}

	return nil
}

// selectTools returns the set of tool names to install: all of them when names is empty.
func selectTools(names []string) (map[string]bool, error) {
	known := map[string]bool{"golangci-lint": true, "gremlins": true}
	for _, tool := range archiveTools() {
		known[tool.name] = true
	}

	for _, tool := range sourceTools() {
		known[tool.name] = true
	}

	if len(names) == 0 {
		return known, nil
	}

	selected := map[string]bool{}

	for _, name := range names {
		if !known[name] {
			return nil, fmt.Errorf("%w: unknown tool %q", errInstall, name)
		}

		selected[name] = true
	}

	return selected, nil
}

// repositoryRoot returns the nearest ancestor of the working directory that holds tools/versions.env.
func repositoryRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}

	for {
		_, statErr := os.Stat(filepath.Join(dir, filepath.FromSlash(versionsFile)))
		if statErr == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%w: %s not found above the working directory", errInstall, versionsFile)
		}

		dir = parent
	}
}

// readFile reads a slash-separated file below dir through an [os.Root].
func readFile(dir, file string) ([]byte, error) {
	tree, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dir, err)
	}

	content, readErr := tree.ReadFile(filepath.FromSlash(file))
	if readErr != nil {
		readErr = fmt.Errorf(readFileError, file, readErr)
	}

	return content, errors.Join(readErr, tree.Close())
}

func installSource(ctx context.Context, tool sourceTool, version, binDir string) error {
	if version == "" {
		return fmt.Errorf("%w: %s is not set in %s", errInstall, tool.versionKey, versionsFile)
	}

	command := exec.CommandContext(ctx, "go", "install", tool.pkg+"@"+version)

	command.Env = append(os.Environ(), "GOBIN="+binDir, "GOFLAGS=-mod=mod")
	command.Stdout = log.Writer()
	command.Stderr = log.Writer()

	err := command.Run()
	if err != nil {
		return fmt.Errorf("go install %s@%s: %w", tool.pkg, version, err)
	}

	log.Printf("%s %s built from source with go install (module checksums verified by the Go checksum database)", tool.name, version)

	return nil
}

func installArchive(ctx context.Context, tool *archiveTool, tag, checksumDigest, binDir string) error {
	if tag == "" {
		return fmt.Errorf("%w: %s is not set in %s", errInstall, tool.versionKey, versionsFile)
	}

	version := strings.TrimPrefix(tag, "v")
	asset := tool.asset(runtime.GOOS, runtime.GOARCH, version)
	base := fmt.Sprintf("https://github.com/%s/releases/download/%s/", tool.repo, tag)

	archive, err := download(ctx, base+asset)
	if err != nil {
		return err
	}

	sums, err := download(ctx, base+tool.checksums(version))
	if err != nil {
		return err
	}

	if tool.checksumPinKey != "" {
		if manifestErr := verifyManifestDigest(sums, checksumDigest); manifestErr != nil {
			return manifestErr
		}
	}

	err = verifyChecksum(archive, string(sums), asset)
	if err != nil {
		return err
	}

	provenance := verifyReleaseAttestation(ctx, tool.repo, tag, asset, archive)

	executable, err := extractMember(archive, asset, tool.member(runtime.GOOS, runtime.GOARCH, version))
	if err != nil {
		return err
	}

	err = installSourceExecutable(binDir, tool.name, executable)
	if err != nil {
		return fmt.Errorf("install %s: %w", tool.name, err)
	}

	log.Printf("%s %s installed: sha256 matches the release checksums; %s", tool.name, tag, provenance)

	return nil
}

func download(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", url, err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxDownload+1))
	closeErr := response.Body.Close()

	switch {
	case response.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: download %s: %s", errInstall, url, response.Status)
	case readErr != nil || closeErr != nil:
		return nil, fmt.Errorf("download %s: %w", url, errors.Join(readErr, closeErr))
	case len(body) > maxDownload:
		return nil, fmt.Errorf("%w: download %s: larger than %d bytes", errInstall, url, maxDownload)
	default:
		return body, nil
	}
}

// verifyChecksum requires sums (sha256sum format) to list asset with the digest of content.
func verifyChecksum(content []byte, sums, asset string) error {
	digest := sha256.Sum256(content)
	actual := hex.EncodeToString(digest[:])

	for line := range strings.SplitSeq(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) != checksumFields || strings.TrimPrefix(fields[1], "*") != asset {
			continue
		}

		if fields[0] != actual {
			return fmt.Errorf("%w: sha256 of %s is %s, release checksums say %s", errInstall, asset, actual, fields[0])
		}

		return nil
	}

	return fmt.Errorf("%w: release checksums do not list %s", errInstall, asset)
}

// verifyReleaseAttestation checks the archive against the release's attestation with the GitHub CLI
// and describes what was established. Without the CLI the digest check is all there is.
func verifyReleaseAttestation(ctx context.Context, repo, tag, asset string, archive []byte) string {
	github, err := exec.LookPath("gh")
	if err != nil {
		return "release attestation NOT checked (GitHub CLI `gh` not found)"
	}

	dir, err := os.MkdirTemp("", "installtools-")
	if err != nil {
		return "release attestation NOT checked: " + err.Error()
	}

	defer removeAll(dir)

	file := filepath.Join(dir, asset)

	err = os.WriteFile(file, archive, privateMode)
	if err != nil {
		return "release attestation NOT checked: " + err.Error()
	}

	output, err := exec.CommandContext(ctx, github, "release", "verify-asset", tag, file, "-R", repo).CombinedOutput()
	if err != nil {
		return "release attestation NOT verified: " + strings.TrimSpace(string(output))
	}

	return "release attestation verified with gh release verify-asset"
}

func removeAll(dir string) {
	err := os.RemoveAll(dir)
	if err != nil {
		log.Printf("cannot remove %s: %v", dir, err)
	}
}

// extractMember returns the named file from a .tar.gz or .zip archive.
func extractMember(archive []byte, name, member string) ([]byte, error) {
	if strings.HasSuffix(name, ".zip") {
		return extractZip(archive, member)
	}

	return extractTarGz(archive, member)
}

func extractTarGz(archive []byte, member string) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}

	reader := tar.NewReader(gzipReader)

	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			return nil, fmt.Errorf("%w: archive has no %s", errInstall, member)
		}

		if nextErr != nil {
			return nil, fmt.Errorf("read tar: %w", nextErr)
		}

		if path.Clean(header.Name) == member {
			content, readErr := io.ReadAll(io.LimitReader(reader, maxDownload))
			if readErr != nil {
				return nil, fmt.Errorf(readFileError, member, readErr)
			}

			return content, nil
		}
	}
}

func extractZip(archive []byte, member string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	for _, file := range reader.File {
		if path.Clean(file.Name) == member {
			return readZipMember(file)
		}
	}

	return nil, fmt.Errorf("%w: archive has no %s", errInstall, member)
}

func readZipMember(file *zip.File) ([]byte, error) {
	handle, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", file.Name, err)
	}

	content, readErr := io.ReadAll(io.LimitReader(handle, maxDownload))
	if readErr != nil {
		readErr = fmt.Errorf(readFileError, file.Name, readErr)
	}

	return content, errors.Join(readErr, handle.Close())
}

func installPatchedTools(ctx context.Context, root string, selected map[string]bool, versions map[string]string, binDir string) error {
	if selected["golangci-lint"] {
		if err := installLinter(ctx, root, versions, binDir); err != nil {
			return fmt.Errorf("golangci-lint: %w", err)
		}
	}

	if selected["gremlins"] {
		if err := installGremlins(ctx, root, versions, binDir); err != nil {
			return fmt.Errorf("gremlins: %w", err)
		}
	}

	return nil
}

// verifyManifestDigest binds a mutable upstream release's checksum manifest to a reviewed input.
func verifyManifestDigest(data []byte, expected string) error {
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != expected {
		return fmt.Errorf("%w: checksum manifest differs from its authoritative pin", errInstall)
	}

	return nil
}
