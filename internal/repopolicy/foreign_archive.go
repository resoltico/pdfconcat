// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"
)

// The Go module ZIP format's 500 MiB limit bounds extraction independently of compression ratios.
const maxForeignArchiveBytes = 500 << 20

func foreignArchiveFiles(source ForeignSource, data []byte) (map[string][]byte, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open module ZIP: %w", err)
	}

	prefix := source.Module + "@" + source.Version + "/"
	files := map[string][]byte{}
	zipNames := map[string][]byte{}
	remaining := int64(maxForeignArchiveBytes)

	for _, entry := range archive.File {
		relative, pathErr := foreignZipRelative(entry, prefix)
		if pathErr != nil {
			return nil, pathErr
		}

		if _, duplicate := files[relative]; duplicate {
			return nil, fmt.Errorf("%w: duplicate module ZIP path %s", errForeignSource, relative)
		}

		content, readErr := readForeignZipEntry(entry, remaining)
		if readErr != nil {
			return nil, readErr
		}

		remaining -= int64(len(content))
		files[relative], zipNames[entry.Name] = content, content
	}

	if len(files) == 0 || files[goModuleFile] == nil {
		return nil, fmt.Errorf("%w: module ZIP is empty or omits go.mod", errForeignSource)
	}

	if foreignH1(zipNames) != source.ModuleSum ||
		foreignH1(map[string][]byte{goModuleFile: files[goModuleFile]}) != source.GoModSum {
		return nil, fmt.Errorf("%w: module ZIP/source go.mod h1 differs from pin", errForeignSource)
	}

	return files, nil
}

func foreignZipRelative(entry *zip.File, prefix string) (string, error) {
	relative, valid := strings.CutPrefix(entry.Name, prefix)
	if !valid || !fs.ValidPath(relative) || strings.ContainsAny(relative, "\\\n\r:") || !entry.Mode().IsRegular() {
		return "", fmt.Errorf("%w: unsafe/nonregular module ZIP path %q", errForeignSource, entry.Name)
	}

	return relative, nil
}

func readForeignZipEntry(entry *zip.File, remaining int64) ([]byte, error) {
	if remaining < 0 || entry.UncompressedSize64 > uint64(remaining) {
		return nil, fmt.Errorf("%w: module ZIP exceeds %d decoded bytes", errForeignSource, maxForeignArchiveBytes)
	}

	reader, err := entry.Open()
	if err != nil {
		return nil, fmt.Errorf("open module ZIP entry: %w", err)
	}

	content, readErr := io.ReadAll(io.LimitReader(reader, remaining+1))
	if err = errors.Join(readErr, reader.Close()); err != nil {
		return nil, fmt.Errorf("read module ZIP entry: %w", err)
	}

	if int64(len(content)) > remaining {
		return nil, fmt.Errorf("%w: module ZIP exceeds decoded byte budget", errForeignSource)
	}

	return content, nil
}

// foreignH1 implements Go's documented h1 manifest: sorted archive names paired with their byte
// SHA-256 sums, then SHA-256/base64 of that manifest. go.mod uses the one-file goModuleFile manifest.
func foreignH1(files map[string][]byte) string {
	var manifest strings.Builder
	for _, name := range slices.Sorted(maps.Keys(files)) {
		fmt.Fprintf(&manifest, "%x  %s\n", sha256.Sum256(files[name]), name)
	}

	digest := sha256.Sum256([]byte(manifest.String()))

	return "h1:" + base64.StdEncoding.EncodeToString(digest[:])
}
