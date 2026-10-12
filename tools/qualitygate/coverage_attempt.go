// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type coverageAttempt struct {
	WorktreeClean bool              `json:"worktree_clean"`
	SchemaVersion int               `json:"schema_version"`
	Commit        string            `json:"commit"`
	Toolchain     string            `json:"toolchain"`
	Platform      string            `json:"platform"`
	Command       []string          `json:"command"`
	Started       string            `json:"started"`
	Finished      string            `json:"finished,omitempty"`
	Outcome       string            `json:"outcome"`
	Complete      bool              `json:"complete"`
	Stages        map[string]string `json:"stages"`
	Planned       []string          `json:"planned_files"`
	Available     []string          `json:"available_files"`
	Failure       string            `json:"failure,omitempty"`
}

func startCoverageEvidence(ctx context.Context, root, directory string) (*coverageAttempt, error) {
	if err := os.Mkdir(directory, dirMode); err != nil {
		return nil, fmt.Errorf("create new coverage evidence directory: %w", err)
	}
	attempt := &coverageAttempt{SchemaVersion: 1, Toolchain: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH, Command: []string{"qualitygate", "coverage", "-evidence", directory}, Started: time.Now().UTC().Format(time.RFC3339Nano), Outcome: "started", Stages: map[string]string{"discovery": "incomplete", "unit": "incomplete", "executable_data": "incomplete", "conversion": "incomplete", "baseline_merge": "incomplete", "native": "not_applicable", "source_inventory": "not_applicable", "collection": "incomplete", "merged_profile": "incomplete"}, Planned: []string{"unit.events.json", "unit.events.json.stderr", "unit.out", "covdata/", "subprocess.out", "merged.out", "merged.out.func.txt", "native-progress/"}}
	// The start manifest exists even if identity discovery or collection subsequently fails.
	if err := writeCoverageManifest(ctx, directory, attempt); err != nil {
		return nil, err
	}
	commit, err := coverageCommit(ctx, root)
	if err != nil {
		return nil, finishCoverageEvidence(directory, attempt, err)
	}
	attempt.Commit = commit
	status, err := (&command{dir: root, name: gitTool, args: []string{"status", "--porcelain=v1", "--untracked-files=all"}}).output(ctx)
	if err != nil {
		return nil, finishCoverageEvidence(directory, attempt, err)
	}
	attempt.WorktreeClean = strings.TrimSpace(status) == ""
	if runtime.GOOS == nativeProgressOS {
		attempt.Stages["native"] = "incomplete"
		attempt.Stages["source_inventory"] = "incomplete"
	}
	if err := writeCoverageManifest(ctx, directory, attempt); err != nil {
		return nil, err
	}
	return attempt, nil
}

func coverageCommit(ctx context.Context, root string) (string, error) {
	value, err := (&command{dir: root, name: "git", args: []string{"rev-parse", "HEAD"}}).output(ctx)
	return strings.TrimSpace(value), err
}

// Finalization owns an independent bounded context and performs no conversion or merge rescue.
func finishCoverageEvidence(directory string, attempt *coverageAttempt, result error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	complete := len(attempt.Stages) > 0
	for _, status := range attempt.Stages {
		if status != "complete" && status != "not_applicable" {
			complete = false
		}
	}
	if result == nil && !complete {
		result = fmt.Errorf("%w: coverage ended before every stage completed", errGate)
	}
	attempt.Finished = time.Now().UTC().Format(time.RFC3339Nano)
	attempt.Outcome = "passed"
	if result != nil {
		attempt.Outcome = "failed"
		attempt.Failure = result.Error()
	}
	attempt.Complete = result == nil && complete
	attempt.Available = nil
	walkErr := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("nonregular evidence: %s", path)
		}
		if !entry.IsDir() && filepath.Base(path) != "manifest.json" {
			relative, err := filepath.Rel(directory, path)
			if err != nil {
				return err
			}
			attempt.Available = append(attempt.Available, filepath.ToSlash(relative))
		}
		return nil
	})
	if walkErr != nil {
		attempt.Complete = false
		attempt.Outcome = "failed"
		attempt.Failure = errors.Join(result, walkErr).Error()
	}
	manifestErr := writeCoverageManifest(ctx, directory, attempt)
	log.Printf("coverage: %s attempt evidence: %s (complete=%t)", attempt.Outcome, directory, attempt.Complete)
	return errors.Join(result, walkErr, manifestErr)
}

func writeCoverageManifest(ctx context.Context, directory string, attempt *coverageAttempt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	content, err := json.MarshalIndent(attempt, "", "  ")
	if err != nil {
		return err
	}
	staged, err := os.CreateTemp(directory, ".manifest-*")
	if err != nil {
		return fmt.Errorf("stage coverage manifest: %w", err)
	}
	_, writeErr := staged.Write(append(content, '\n'))
	closeErr := staged.Close()
	if err := errors.Join(writeErr, closeErr, ctx.Err()); err != nil {
		return errors.Join(fmt.Errorf("stage coverage manifest: %w", err), os.Remove(staged.Name()))
	}
	if err := os.Rename(staged.Name(), filepath.Join(directory, "manifest.json")); err != nil {
		return errors.Join(fmt.Errorf("finalize coverage manifest: %w", err), os.Remove(staged.Name()))
	}
	return nil
}
