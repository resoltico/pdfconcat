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
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	maxForeignTestArchive     = 320 << 20
	foreignAcquisitionTimeout = 5 * time.Minute
)

func foreignTestArtifact(ctx context.Context, pin repopolicy.ForeignTestInputs) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("foreign test cache: %w", err)
	}

	cache = filepath.Join(cache, "pdfconcat", "foreign-test-inputs")
	if operationErr := os.MkdirAll(cache, foreignStageDirectoryMode); operationErr != nil {
		return "", fmt.Errorf("create foreign test cache: %w", operationErr)
	}

	name := filepath.Join(cache, pin.SHA256+".tar.gz")
	if _, err = os.Lstat(name); err == nil {
		return name, verifyForeignTestArtifact(name, pin.SHA256)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect foreign test cache: %w", err)
	}

	file, err := os.CreateTemp(cache, ".acquisition-")
	if err != nil {
		return "", fmt.Errorf("create foreign test acquisition: %w", err)
	}

	return name, acquireForeignTestArtifact(ctx, pin, file, name)
}

func verifyForeignTestArtifact(name, expected string) error {
	info, err := os.Lstat(name)
	if err != nil {
		return fmt.Errorf("inspect foreign test artifact: %w", err)
	}

	if !info.Mode().IsRegular() || info.Size() > maxForeignTestArchive {
		return fmt.Errorf("%w: foreign test artifact is not a bounded regular file", errGate)
	}

	tree, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		return fmt.Errorf("open foreign artifact directory: %w", err)
	}
	defer closeLogged(tree)

	file, err := tree.Open(filepath.Base(name))
	if err != nil {
		return fmt.Errorf("open foreign test artifact: %w", err)
	}
	defer closeLogged(file)

	digest := sha256.New()
	if _, err = io.Copy(digest, io.LimitReader(file, maxForeignTestArchive+1)); err != nil {
		return fmt.Errorf("hash foreign test artifact: %w", err)
	}

	if hex.EncodeToString(digest.Sum(nil)) != expected {
		return fmt.Errorf("%w: foreign test artifact SHA-256 mismatch", errGate)
	}

	return nil
}

func downloadForeignTestArtifact(ctx context.Context, url string, file *os.File) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("foreign test request: %w", err)
	}

	client := &http.Client{Timeout: foreignAcquisitionTimeout}

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("acquire required foreign test inputs: %w", err)
	}
	defer closeLogged(response.Body)

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: foreign test acquisition HTTP %d", errGate, response.StatusCode)
	}

	n, err := io.Copy(file, io.LimitReader(response.Body, maxForeignTestArchive+1))
	if err != nil {
		return fmt.Errorf("download foreign test inputs: %w", err)
	}

	if n > maxForeignTestArchive {
		return fmt.Errorf("%w: foreign test archive exceeds acquisition bound", errGate)
	}

	return nil
}

func cleanupForeignAcquisition(file *os.File) error {
	closeErr := file.Close()
	if errors.Is(closeErr, os.ErrClosed) {
		closeErr = nil
	}

	removeErr := os.Remove(file.Name())
	if os.IsNotExist(removeErr) {
		removeErr = nil
	}

	return errors.Join(closeErr, removeErr)
}

func acquireForeignTestArtifact(ctx context.Context, pin repopolicy.ForeignTestInputs, file *os.File, name string) (result error) {
	defer func() { result = errors.Join(result, cleanupForeignAcquisition(file)) }()

	if operationErr := downloadForeignTestArtifact(ctx, pin.URL, file); operationErr != nil {
		return operationErr
	}

	if operationErr := file.Close(); operationErr != nil {
		return fmt.Errorf("close foreign test acquisition: %w", operationErr)
	}

	if operationErr := verifyForeignTestArtifact(file.Name(), pin.SHA256); operationErr != nil {
		return operationErr
	}

	if operationErr := os.Rename(file.Name(), name); operationErr != nil {
		return fmt.Errorf("retain verified foreign test artifact: %w", operationErr)
	}

	return nil
}
