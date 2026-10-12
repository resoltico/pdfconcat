// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

type mutationPartition struct {
	Index int `json:"index"`
	Count int `json:"count"`
}

func parseMutationPartition(value string) (mutationPartition, error) {
	left, right, found := strings.Cut(value, "/")
	index, indexErr := strconv.Atoi(left)
	count, countErr := strconv.Atoi(right)
	if !found || indexErr != nil || countErr != nil || index < 0 || count < 1 || index >= count {
		return mutationPartition{}, fmt.Errorf("%w: partition must be INDEX/COUNT with 0 <= INDEX < COUNT", errGate)
	}
	return mutationPartition{Index: index, Count: count}, nil
}

// Sorted-file round robin partitions the complete native inventory, never its covered subset.
func selectMutationPartition(files map[string]bool, partition mutationPartition) (map[string]bool, error) {
	if partition.Count < 1 || partition.Index < 0 || partition.Index >= partition.Count || partition.Count > len(files) {
		return nil, fmt.Errorf("%w: invalid or empty mutation partition", errGate)
	}
	selected := map[string]bool{}
	for index, path := range slices.Sorted(maps.Keys(files)) {
		if index%partition.Count == partition.Index {
			selected[path] = true
		}
	}
	return selected, nil
}

func partitionMutationScope(ctx context.Context, scope scopeOfMutation, options *mutationOptions, reportFile string) (scopeOfMutation, error) {
	partition := options.partition
	if partition.Count == 0 {
		partition = mutationPartition{Count: 1}
	}
	selected, err := selectMutationPartition(scope.hostFiles, partition)
	if err != nil {
		return scopeOfMutation{}, err
	}
	commit, err := coverageCommit(ctx, options.root)
	if err != nil {
		return scopeOfMutation{}, err
	}
	digests := map[string]string{}
	for path := range scope.hostFiles {
		data, err := readInRoot(options.snapshot, path)
		if err != nil {
			return scopeOfMutation{}, err
		}
		digests[path] = fmt.Sprintf("%x", sha256.Sum256(data))
		if !selected[path] {
			scope.excludes = append(scope.excludes, "^"+regexp.QuoteMeta(path)+"$")
		}
	}
	// Every receipt records the full pristine inventory and this partition's selection.
	receipt := struct {
		Commit        string            `json:"commit"`
		Only          string            `json:"only,omitempty"`
		Platform      string            `json:"platform"`
		Partition     mutationPartition `json:"partition"`
		FullInventory map[string]string `json:"full_inventory_sha256"`
		Selected      []string          `json:"selected_files"`
	}{commit, options.only, runtime.GOOS + "/" + runtime.GOARCH, partition, digests, slices.Sorted(maps.Keys(selected))}
	content, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return scopeOfMutation{}, err
	}
	parent, err := os.OpenRoot(filepath.Dir(reportFile))
	if err != nil {
		return scopeOfMutation{}, err
	}
	file, err := parent.OpenFile(filepath.Base(reportFile)+".scope.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	rootErr := parent.Close()
	if err != nil {
		return scopeOfMutation{}, fmt.Errorf("create mutation scope receipt: %w", errors.Join(err, rootErr))
	}
	_, writeErr := file.Write(append(content, '\n'))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil || rootErr != nil {
		return scopeOfMutation{}, fmt.Errorf("write mutation scope receipt: %w", errors.Join(writeErr, closeErr, rootErr))
	}
	scope.hostFiles = selected
	return scope, nil
}
