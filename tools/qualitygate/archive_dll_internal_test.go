// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"debug/buildinfo"
	"debug/pe"
	"encoding/binary"
	"path/filepath"
	"reflect"
	"testing"
)

func TestArchiveRejectsRealWindowsBinaryWithDLLCharacteristic(t *testing.T) {
	t.Parallel()

	root, rootErr := repoRoot()
	if rootErr != nil {
		t.Fatal(rootErr)
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "pdfconcat.exe")
	build := goCommand(root, goBuildVerb, "-trimpath", "-o", file, "./cmd/pdfconcat")

	build.env = []string{"GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0"}
	if _, err := build.output(t.Context()); err != nil {
		t.Fatal(err)
	}

	data, readErr := readInRoot(dir, filepath.Base(file))
	if readErr != nil {
		t.Fatal(readErr)
	}

	before, beforeErr := buildinfo.Read(bytes.NewReader(data))
	if beforeErr != nil {
		t.Fatal(beforeErr)
	}

	if !binaryHeaderMatches(windowsOS, amd64Arch, data) {
		t.Fatal("positive real Windows executable rejected")
	}

	offset := int(binary.LittleEndian.Uint32(data[0x3c:])) + 4 + 18
	characteristics := binary.LittleEndian.Uint16(data[offset:])
	binary.LittleEndian.PutUint16(data[offset:], characteristics|pe.IMAGE_FILE_DLL)

	after, afterErr := buildinfo.Read(bytes.NewReader(data))
	if afterErr != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("negative control changed Go identity: %v", afterErr)
	}

	parsed, parseErr := pe.NewFile(bytes.NewReader(data))
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	defer closeLogged(parsed)

	if parsed.Characteristics&pe.IMAGE_FILE_DLL == 0 || parsed.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 {
		t.Fatal("control lacks simultaneous DLL and executable characteristics")
	}

	if binaryHeaderMatches(windowsOS, amd64Arch, data) {
		t.Fatal("DLL accepted as standalone Windows executable")
	}
}
