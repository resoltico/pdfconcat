// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

type moduleSource struct {
	Path     string `json:"Path"`
	Version  string `json:"Version"`
	Sum      string `json:"Sum"`
	GoModSum string `json:"GoModSum"`
	Zip      string `json:"Zip"`
	Info     string `json:"Info"`
	Error    string `json:"Error"`
}

const (
	sourceModVerb      = "mod"
	sourceDownloadVerb = "download"
	sourceTestVerb     = "test"
	sourceModuleFile   = "go.mod"
	linterPinPrefix    = "GOLANGCI_LINT"
	linterModule       = "github.com/golangci/golangci-lint/v2"
)

// sourceBuildEnv verifies public dependencies and prevents ambient workspaces or toolchain substitution.
func sourceBuildEnv() []string {
	return []string{
		"GOWORK=off", "GO111MODULE=on", "GOENV=off", "GOPRIVATE=", "GOFLAGS=-mod=readonly",
		"GOSUMDB=sum.golang.org", "GONOSUMDB=", "GONOPROXY=", "GOPROXY=https://proxy.golang.org", "GOTOOLCHAIN=local",
		"GOOS=" + runtime.GOOS, "GOARCH=" + runtime.GOARCH,
	}
}

// sourceInstallEnv retains the same public-checksum and native-toolchain boundary for plain Go installs.
func sourceInstallEnv(binDir string) []string {
	return append(sourceBuildEnv(), "GOBIN="+binDir, "GOFLAGS=-mod=mod")
}

func downloadModuleSource(
	ctx context.Context,
	versions map[string]string,
	workspace, module, prefix string,
) (*moduleSource, []byte, error) {
	command := exec.CommandContext(ctx, "go", sourceModVerb, sourceDownloadVerb, "-json", module+"@"+versions[prefix+"_VERSION"])
	command.Dir = workspace

	command.Env = append(os.Environ(), sourceBuildEnv()...)

	var stdout bytes.Buffer

	command.Stdout = &stdout

	command.Stderr = log.Writer()
	if err := command.Run(); err != nil {
		return nil, nil, fmt.Errorf("download upstream tool module: %w", err)
	}

	var source moduleSource
	if err := json.Unmarshal(stdout.Bytes(), &source); err != nil {
		return nil, nil, fmt.Errorf("decode upstream tool module: %w", err)
	}

	if source.Error != "" || source.Zip == "" || source.Info == "" {
		return nil, nil, fmt.Errorf("%w: incomplete upstream tool download: %s", errInstall, source.Error)
	}

	content, readErr := readFile(filepath.Dir(source.Zip), filepath.Base(source.Zip))
	if readErr != nil {
		return nil, nil, readErr
	}

	if err := verifyModuleSource(&source, content, versions, module, prefix); err != nil {
		return nil, nil, err
	}

	return &source, content, nil
}

func verifyModuleSource(source *moduleSource, content []byte, versions map[string]string, module, prefix string) error {
	digest := sha256.Sum256(content)
	if source.Path != module || source.Version != versions[prefix+"_VERSION"] ||
		source.Sum != versions[prefix+"_SOURCE_SUM"] || source.GoModSum != versions[prefix+"_SOURCE_MOD_SUM"] ||
		hex.EncodeToString(digest[:]) != versions[prefix+"_SOURCE_ZIP_SHA256"] {
		return fmt.Errorf("%w: upstream tool source identity differs from the pinned module, sums, or archive digest", errInstall)
	}

	return nil
}

// extractModuleSource uses the verified archive instead of trusting an unpacked module-cache directory.
func extractModuleSource(content []byte, module, version, destination string) error {
	archive, archiveErr := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if archiveErr != nil {
		return fmt.Errorf("open upstream tool source: %w", archiveErr)
	}

	root, rootErr := os.OpenRoot(destination)
	if rootErr != nil {
		return fmt.Errorf("open isolated tool directory: %w", rootErr)
	}

	prefix := module + "@" + version + "/"

	var extractErr error
	for _, file := range archive.File {
		if extractErr = extractModuleSourceFile(root, prefix, file); extractErr != nil {
			break
		}
	}

	if len(archive.File) == 0 {
		extractErr = fmt.Errorf("%w: empty upstream tool source archive", errInstall)
	}

	return errors.Join(extractErr, root.Close())
}

func extractModuleSourceFile(root *os.Root, prefix string, file *zip.File) error {
	if !strings.HasPrefix(file.Name, prefix) {
		return fmt.Errorf("%w: upstream source member outside module prefix: %q", errInstall, file.Name)
	}

	relative := strings.TrimPrefix(file.Name, prefix)
	if file.FileInfo().IsDir() {
		return nil
	}

	if !regularModuleSourceMember(relative, file) {
		return fmt.Errorf("%w: unsafe upstream source member %q", errInstall, file.Name)
	}

	if err := root.MkdirAll(filepath.FromSlash(path.Dir(relative)), dirMode); err != nil {
		return fmt.Errorf("create source directory: %w", err)
	}

	source, sourceErr := file.Open()
	if sourceErr != nil {
		return fmt.Errorf("open upstream source member: %w", sourceErr)
	}

	target, targetErr := root.OpenFile(filepath.FromSlash(relative), os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateMode)
	if targetErr != nil {
		return errors.Join(fmt.Errorf("create upstream source file: %w", targetErr), source.Close())
	}

	copied, copyErr := io.Copy(target, io.LimitReader(source, maxDownload+1))
	if copied > maxDownload {
		copyErr = errors.Join(copyErr, fmt.Errorf("%w: upstream source member exceeds decoded limit", errInstall))
	}

	if copyErr != nil {
		copyErr = fmt.Errorf("extract upstream source file: %w", copyErr)
	}

	return errors.Join(copyErr, source.Close(), target.Close())
}

func regularModuleSourceMember(relative string, file *zip.File) bool {
	return relative != "" && path.Clean(relative) == relative && filepath.IsLocal(filepath.FromSlash(relative)) &&
		!strings.Contains(relative, "\\") && file.Mode().IsRegular()
}

func verifySourceTime(source *moduleSource, want string) error {
	content, readErr := readFile(filepath.Dir(source.Info), filepath.Base(source.Info))
	if readErr != nil {
		return readErr
	}

	var info struct {
		Version string `json:"Version"`
		Time    string `json:"Time"`
	}
	if err := json.Unmarshal(content, &info); err != nil {
		return fmt.Errorf("decode upstream source timestamp: %w", err)
	}

	if info.Version != source.Version || info.Time != want {
		return fmt.Errorf("%w: upstream source tag epoch differs from the pin", errInstall)
	}

	return nil
}
