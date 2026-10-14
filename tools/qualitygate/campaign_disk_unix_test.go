// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCampaignCounterDoesNotFollowLinksDuringBoundedRenames(t *testing.T) {
	t.Parallel()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(root)
	outside := t.TempDir()
	outsideRoot, err := os.OpenRoot(outside)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(outsideRoot)
	file, err := outsideRoot.Create("large")
	if err != nil {
		t.Fatal(err)
	}
	if err = errors.Join(file.Truncate(1<<30), file.Close()); err != nil {
		t.Fatal(err)
	}
	if err = root.Mkdir("moving", dirMode); err != nil {
		t.Fatal(err)
	}
	if err = root.WriteFile("moving/inside", []byte("owned bytes"), fileMode); err != nil {
		t.Fatal(err)
	}
	if err = root.Rename("moving", "parked"); err != nil {
		t.Fatal(err)
	}
	if err = root.Symlink(outside, "moving"); err != nil {
		t.Fatal(err)
	}
	sample, err := countCampaignDiskTree(root, ".", campaignDiskUsage{4096, 100})
	if err != nil || sample.nonregular == "" || sample.usage.bytes >= 4096 {
		t.Fatalf("outside link was followed: %+v/%v", sample, err)
	}
	done := make(chan error, 1)
	go func() {
		for range 12 {
			if err := root.Remove("moving"); err != nil {
				done <- err
				return
			}
			if err := root.Rename("parked", "moving"); err != nil {
				done <- err
				return
			}
			time.Sleep(time.Millisecond)
			if err := root.Rename("moving", "parked"); err != nil {
				done <- err
				return
			}
			if err := root.Symlink(outside, "moving"); err != nil {
				done <- err
				return
			}
			time.Sleep(time.Millisecond)
		}
		done <- nil
	}()
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			return
		default:
		}
		if sample, err := countCampaignDiskTree(root, ".", campaignDiskUsage{4096, 100}); err != nil || sample.usage.bytes >= 4096 {
			t.Fatalf("legitimate nested changes failed or followed external data: %+v/%v", sample, err)
		}
	}
}

func TestMutationOpaqueLiveSampleKeepsCapacityObservationsAndRequiresFinalAccess(t *testing.T) {
	t.Parallel()
	scratch, err := newCampaignScratch(t.TempDir(), "qualitygate-mutation-control-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeAll(scratch.path) })
	if err = scratch.root.Mkdir("snapshot", dirMode); err != nil {
		t.Fatal(err)
	}
	if err = scratch.root.Mkdir("snapshot/opaque", dirMode); err != nil {
		t.Fatal(err)
	}
	if err = scratch.root.Chmod("snapshot/opaque", 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	watch := startMutationStorageWatch(ctx, scratch, campaignDiskUsage{4096, 100}, 0, 2*time.Millisecond, time.Hour)
	finished := false
	defer func() {
		if !finished {
			if err := scratch.root.Chmod("snapshot/opaque", dirMode); err != nil {
				t.Error(err)
			}
			var cleanupErr error
			finishMutationStorage(&cleanupErr, scratch, watch, false)
			if cleanupErr != nil {
				t.Error(cleanupErr)
			}
		}
	}()
	watch.setSnapshot(filepath.Join(scratch.path, "snapshot"))
	// Observe real native capacity while a separate inheriting producer completes.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := os.CreateTemp(t.TempDir(), "heartbeat-")
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(stdout)
	run := &command{name: executable, args: []string{"-test.run=^TestMutationStorageHeartbeat$"}, env: []string{"PDFCONCAT_MUTATION_STORAGE_HEARTBEAT=" + filepath.Join(scratch.path, "snapshot")}, stdout: stdout, stderr: stdout}
	// Hold the actual producer until the periodic observer has completed another
	// capacity query after its heartbeat. Scheduling cannot shorten this boundary.
	observed := make(chan error, 1)
	go func() {
		progress := func() error {
			ticker := time.NewTicker(2 * time.Millisecond)
			defer ticker.Stop()
			baseline := -1
			for {
				select {
				case <-watch.ctx.Done():
					return context.Cause(watch.ctx)
				case <-ticker.C:
					if _, err := scratch.root.Lstat("snapshot/heartbeat"); errors.Is(err, os.ErrNotExist) {
						continue
					} else if err != nil {
						return err
					}
					watch.mutex.Lock()
					queries := watch.volumeSamples
					watch.mutex.Unlock()
					if baseline < 0 {
						baseline = queries
					} else if queries > baseline {
						return scratch.root.WriteFile("snapshot/release", []byte("observation complete"), fileMode)
					}
				}
			}
		}
		err := progress()
		if err != nil {
			cancel()
		}
		observed <- err
	}()
	runErr := run.run(watch.ctx)
	cancel()
	if err := errors.Join(runErr, <-observed); err != nil {
		t.Fatalf("opaque live fixture aborted healthy producer: %v", err)
	}
	watch.mutex.Lock()
	partials := watch.partialSamples
	// Exercise the actual settled predicate without recording a production
	// watcher failure; the independent restored-access finish must stay healthy.
	finalErr := watch.inspect(true, true)
	watch.mutex.Unlock()
	if partials == 0 || !errors.Is(finalErr, os.ErrPermission) || !errors.Is(finalErr, errGate) {
		t.Fatalf("opaque final inspection was not rejected: %d/%v", partials, finalErr)
	}
	if err = scratch.root.Chmod("snapshot/opaque", dirMode); err != nil {
		t.Fatal(err)
	}
	var result error
	finishMutationStorage(&result, scratch, watch, false)
	finished = true
	if result != nil {
		t.Fatalf("complete settled inspection rejected: %v", result)
	}
}

func TestCampaignOwnerPreservesNativeTemporaryParentTraversal(t *testing.T) {
	base := t.TempDir()
	logical, physical := filepath.Join(base, "logical"), filepath.Join(base, "physical")
	if err := os.MkdirAll(filepath.Join(physical, "child"), dirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(logical, dirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(physical, "child"), filepath.Join(logical, "link")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", logical+"/link/..")
	storage, err := newFuzzStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	selected, err := os.Stat(filepath.Dir(storage.path))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.Stat(physical)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(selected, expected) {
		t.Fatalf("native parent traversal collapsed to a different directory: %s", storage.path)
	}
	if err = storage.close(); err != nil {
		t.Fatal(err)
	}
}
