// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

// gremlinsBinary requires the reviewed source variant and its actual native module/main identity.
func gremlinsBinary(ctx context.Context, root string) (string, error) {
	binary, err := findTool(root, "gremlins")
	if err != nil {
		return "", err
	}

	versions, err := readToolVersions(root)
	if err != nil {
		return "", err
	}

	identity, err := repopolicy.GremlinsBuildIdentity(versions)
	if err != nil {
		return "", err
	}

	if metadataErr := repopolicy.VerifyGremlinsBinaryMetadata(binary); metadataErr != nil {
		return "", metadataErr
	}

	printed, err := (&command{dir: root, name: binary, args: []string{"--version"}}).output(ctx)
	if err != nil {
		return "", fmt.Errorf("inspect mutation tool source variant: %w", err)
	}

	expected := "gremlins version " + identity.Version + " " + runtime.GOOS + "/" + runtime.GOARCH
	if strings.TrimSpace(printed) != expected {
		return "", fmt.Errorf(
			"%w: mutation tool is not reviewed variant %s; run `go run ./tools/installtools gremlins`",
			errGate,
			identity.Version,
		)
	}

	return binary, nil
}
