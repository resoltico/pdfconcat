// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

// Command releasecheck verifies configured deliverables against independent support expectations.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

type configuration struct {
	Project string `yaml:"project_name"`
	Builds  []struct {
		Binary string   `yaml:"binary"`
		OS     []string `yaml:"goos"`
		Arch   []string `yaml:"goarch"`
	} `yaml:"builds"`
	Archives []struct {
		Name  string `yaml:"name_template"`
		Files []struct {
			Src, Dst string
			Strip    bool `yaml:"strip_parent"`
		} `yaml:"files"`
	} `yaml:"archives"`
}

func inventory(root string) (map[string][]string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yml"))
	if err != nil {
		return nil, err
	}
	var config configuration
	if err = yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	if len(config.Builds) != 1 || len(config.Archives) != 1 || config.Archives[0].Name != "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}" {
		return nil, errors.New("unsupported release declaration; update inventory interpretation explicitly")
	}
	// This independently declared support contract must survive an accidental
	// configuration target deletion. Updating it changes supported delivery.
	required := []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"}
	actual := []string{}
	for _, os := range config.Builds[0].OS {
		for _, arch := range config.Builds[0].Arch {
			actual = append(actual, os+"/"+arch)
		}
	}
	slices.Sort(actual)
	if !slices.Equal(required, actual) {
		return nil, fmt.Errorf("release targets differ from required six-target support: %v", actual)
	}
	members := []string{}
	for _, file := range config.Archives[0].Files {
		paths, err := filepath.Glob(filepath.Join(root, file.Src))
		if err != nil {
			return nil, err
		}
		if len(paths) == 0 {
			return nil, fmt.Errorf("required archive input missing: %s", file.Src)
		}
		for _, path := range paths {
			name, err := filepath.Rel(root, path)
			if err != nil {
				return nil, err
			}
			if file.Strip {
				name = filepath.Base(path)
			}
			if file.Dst != "" {
				name = filepath.Join(file.Dst, name)
			}
			members = append(members, filepath.ToSlash(name))
		}
	}
	result := map[string][]string{"SOURCE.tar.gz": nil, "install.sh": nil, "install.ps1": nil}
	for _, target := range required {
		parts := strings.Split(target, "/")
		base := config.Project + "_" + parts[0] + "_" + parts[1]
		extension, binary := ".tar.gz", config.Builds[0].Binary
		if parts[0] == "windows" {
			extension = ".zip"
			binary += ".exe"
		}
		list := append(slices.Clone(members), binary)
		slices.Sort(list)
		result[base+extension] = list
		result[base+".cdx.json"] = nil
	}
	return result, nil
}
func names(values map[string][]string) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	slices.Sort(result)
	return result
}
func manifest(dir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || len(fields[0]) != 64 {
			return nil, errors.New("invalid checksum manifest")
		}
		name := strings.TrimPrefix(fields[1], "*")
		if filepath.Base(name) != name || result[name] != "" {
			return nil, fmt.Errorf("unsafe or repeated manifest filename: %s", name)
		}
		result[name] = fields[0]
	}
	return result, nil
}
func archiveMembers(path string) ([]string, error) {
	result := []string{}
	if strings.HasSuffix(path, ".zip") {
		reader, err := zip.OpenReader(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = reader.Close() }()
		for _, file := range reader.File {
			if !file.Mode().IsRegular() {
				return nil, errors.New("nonregular archive member")
			}
			result = append(result, file.Name)
		}
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer func() { _ = gz.Close() }()
		reader := tar.NewReader(gz)
		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if header.Typeflag != tar.TypeReg {
				return nil, fmt.Errorf("nonregular archive member: %s", header.Name)
			}
			result = append(result, header.Name)
		}
	}
	slices.Sort(result)
	return result, nil
}
func verify(root, dir string) error {
	expected, err := inventory(root)
	if err != nil {
		return err
	}
	checksums, err := manifest(dir)
	if err != nil {
		return err
	}
	listed := make([]string, 0, len(checksums))
	for name := range checksums {
		listed = append(listed, name)
	}
	slices.Sort(listed)
	if !slices.Equal(names(expected), listed) {
		return fmt.Errorf("manifest does not enumerate complete required asset set: got %v; want %v", listed, names(expected))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "checksums.txt" || name == "artifacts.json" || name == "metadata.json" || name == "config.yaml" {
			continue
		}
		if _, ok := expected[name]; !ok {
			return fmt.Errorf("unexpected delivered asset: %s", name)
		}
	}
	for _, name := range names(expected) {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != checksums[name] {
			return fmt.Errorf("checksum mismatch: %s", name)
		}
		if members := expected[name]; members != nil {
			actual, err := archiveMembers(path)
			if err != nil {
				return err
			}
			if !slices.Equal(actual, members) {
				return fmt.Errorf("archive members differ: %s: %v != %v", name, actual, members)
			}
		}
	}
	return nil
}
func compare(root, first, second string) error {
	if err := verify(root, first); err != nil {
		return err
	}
	if err := verify(root, second); err != nil {
		return err
	}
	a, err := os.ReadFile(filepath.Join(first, "checksums.txt"))
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(second, "checksums.txt"))
	if err != nil {
		return err
	}
	if string(a) != string(b) {
		return errors.New("clean builds produced different delivered checksums")
	}
	return nil
}
func main() {
	root := flag.String("root", ".", "source checkout")
	dir := flag.String("dir", "dist", "qualified artifact directory")
	second := flag.String("compare", "", "second clean build directory")
	flag.Parse()
	var err error
	if *second != "" {
		err = compare(*root, *dir, *second)
	} else {
		err = verify(*root, *dir)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
