// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"cmp"
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

// Assign larger source files first to the least-loaded partition. Source byte counts
// are a deterministic work estimate, derived from the same frozen bytes as the hashes;
// discovery still determines the complete mutant inventory independently.
func selectMutationPartition(sizes map[string]int, partition mutationPartition) (map[string]bool, error) {
	if partition.Count < 1 || partition.Index < 0 || partition.Index >= partition.Count || partition.Count > len(sizes) {
		return nil, fmt.Errorf("%w: invalid or empty mutation partition", errGate)
	}
	paths := slices.Sorted(maps.Keys(sizes))
	slices.SortStableFunc(paths, func(a, b string) int { return cmp.Compare(sizes[b], sizes[a]) })
	loads := make([]int64, partition.Count)
	selected := map[string]bool{}
	for ordinal, path := range paths {
		// Seed every partition so zero-size sources cannot leave it empty.
		index := ordinal
		if ordinal >= partition.Count {
			index = slices.Index(loads, slices.Min(loads))
		}
		loads[index] += int64(sizes[path])
		if index == partition.Index {
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
	commit, err := coverageCommit(ctx, options.root)
	if err != nil {
		return scopeOfMutation{}, err
	}
	digests := map[string]string{}
	sizes := map[string]int{}
	for path := range scope.hostFiles {
		data, err := readInRoot(options.snapshot, path)
		if err != nil {
			return scopeOfMutation{}, err
		}
		digests[path] = fmt.Sprintf("%x", sha256.Sum256(data))
		sizes[path] = len(data)
	}
	selected, err := selectMutationPartition(sizes, partition)
	if err != nil {
		return scopeOfMutation{}, err
	}
	for path := range scope.hostFiles {
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
