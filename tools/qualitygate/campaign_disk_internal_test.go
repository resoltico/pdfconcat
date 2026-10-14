// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func mutationStorageFixture(t *testing.T, limit campaignDiskUsage) (*campaignScratch, *mutationStorageWatch) {
	t.Helper()
	scratch, err := newCampaignScratch(t.TempDir(), "qualitygate-mutation-control-")
	if err != nil {
		t.Fatal(err)
	}
	watch := startMutationStorageWatch(t.Context(), scratch, limit, 0, time.Hour, time.Hour)
	t.Cleanup(func() { removeAll(scratch.path) })
	return scratch, watch
}

func TestCampaignCapacityUsesNativeVolumeAndRejectsImpossibleReserve(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	available, err := requireCampaignCapacity(path, 0)
	if err != nil || available == 0 {
		t.Fatalf("native capacity unavailable: %d/%v", available, err)
	}
	if _, err = requireCampaignCapacity(path, math.MaxUint64); !errors.Is(err, errGate) || errors.Is(err, errCampaignCapacityQuery) {
		t.Fatalf("impossible reserve was not rejected by actual capacity: %v", err)
	}
	if _, err = requireCampaignCapacity(filepath.Join(path, "absent"), 0); !errors.Is(err, errGate) || !errors.Is(err, errCampaignCapacityQuery) {
		t.Fatalf("native query error hidden: %v", err)
	}
}

func TestCampaignInspectionClassifiesOnlyQualifiedLiveRaces(t *testing.T) {
	t.Parallel()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if file, err := root.Open("../outside"); err == nil {
		closeLogged(file)
		t.Fatal("real root escape accepted")
	} else if !incompleteCampaignInspection(err) {
		t.Fatalf("pinned real escape not classified: %v", err)
	}
	for _, text := range []string{"path escapes from parent [unexpected]", "unexpected path escapes from parent", "I/O metadata unavailable"} {
		if incompleteCampaignInspection(&os.PathError{Op: "openat", Path: "file", Err: errors.New(text)}) {
			t.Fatalf("unqualified error accepted: %q", text)
		}
	}
	if err = root.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = root.Lstat("."); err == nil || incompleteCampaignInspection(err) {
		t.Fatalf("real unexpected closed root was treated as live topology: %v", err)
	}
}

func TestMutationIdleParentRetainsSDKLeftoverDespiteCompleteReport(t *testing.T) {
	t.Parallel()
	scratch, watch := mutationStorageFixture(t, campaignDiskUsage{4096, 100})
	if err := scratch.root.Mkdir("snapshot", dirMode); err != nil {
		t.Fatal(err)
	}
	watch.setSnapshot(filepath.Join(scratch.path, "snapshot"))
	if err := scratch.root.Mkdir("gremlins-unsettled", dirMode); err != nil {
		t.Fatal(err)
	}
	if err := scratch.root.WriteFile("gremlins-unsettled/retain", []byte("unknown ownership"), fileMode); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(report, []byte(`{"complete":true}`), fileMode); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("SDK exited after writing report")
	result := mutationCommandFailure(report, failure)
	finishMutationStorage(&result, scratch, watch, false)
	if !errors.Is(result, failure) || !errors.Is(result, errMutationCleanupUncertain) {
		t.Fatalf("leftover lost failure or uncertainty: %v", result)
	}
	if data, err := readInRoot(scratch.path, "gremlins-unsettled/retain"); err != nil || string(data) != "unknown ownership" {
		t.Fatalf("unknown SDK storage reclaimed: %q/%v", data, err)
	}
}

func TestMutationStorageKeepAndDefaultFailureEvidence(t *testing.T) {
	t.Parallel()
	for _, keep := range []bool{false, true} {
		t.Run(strconv.FormatBool(keep), func(t *testing.T) {
			t.Parallel()
			scratch, watch := mutationStorageFixture(t, campaignDiskUsage{4096, 100})
			if err := scratch.root.Mkdir("snapshot", dirMode); err != nil {
				t.Fatal(err)
			}
			watch.setSnapshot(filepath.Join(scratch.path, "snapshot"))
			if err := scratch.root.WriteFile("snapshot/source", []byte("kept source"), fileMode); err != nil {
				t.Fatal(err)
			}
			var result error
			finishMutationStorage(&result, scratch, watch, keep)
			if result != nil {
				t.Fatal(result)
			}
			_, err := os.Stat(scratch.path)
			if keep && err != nil {
				t.Fatalf("requested snapshot removed: %v", err)
			}
			if !keep && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("disposable storage remained: %v", err)
			}
		})
	}
	scratch, watch := mutationStorageFixture(t, campaignDiskUsage{16, 100})
	if err := scratch.root.Mkdir("snapshot", dirMode); err != nil {
		t.Fatal(err)
	}
	watch.setSnapshot(filepath.Join(scratch.path, "snapshot"))
	report := filepath.Join(scratch.path, "snapshot", "report.json")
	if err := watch.setReport(report, true); err != nil {
		t.Fatal(err)
	}
	if err := scratch.root.WriteFile("snapshot/report.json", []byte(strings.Repeat("x", 32)), fileMode); err != nil {
		t.Fatal(err)
	}
	var result error
	finishMutationStorage(&result, scratch, watch, false)
	if !errors.Is(result, errGate) {
		t.Fatalf("resource failure accepted: %v", result)
	}
	if data, err := readInRoot(scratch.path, "snapshot/report.json"); err != nil || len(data) != 32 {
		t.Fatalf("default failure evidence deleted: %d/%v", len(data), err)
	}
}

func TestMutationStorageChild(t *testing.T) {
	path := os.Getenv("PDFCONCAT_MUTATION_STORAGE_CHILD")
	if path == "" {
		return
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(root)
	if err = root.WriteFile("pid", []byte(strconv.Itoa(os.Getpid())), fileMode); err != nil {
		t.Fatal(err)
	}
	size, err := strconv.Atoi(os.Getenv("PDFCONCAT_MUTATION_STORAGE_BYTES"))
	if err != nil {
		t.Fatal(err)
	}
	if err = root.WriteFile("payload", make([]byte, size), fileMode); err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Println("owned mutation producer began"); err != nil {
		t.Fatal(err)
	}
	if size > 1024 {
		time.Sleep(time.Hour)
	}
}

func TestMutationStorageResourceTripCancelsRealLauncherAndRetainsUnknownSDK(t *testing.T) {
	t.Parallel()
	scratch, err := newCampaignScratch(t.TempDir(), "qualitygate-mutation-control-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeAll(scratch.path) })
	if err = scratch.root.Mkdir("snapshot", dirMode); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	watch := startMutationStorageWatch(ctx, scratch, campaignDiskUsage{1024, 100}, 0, 5*time.Millisecond, 5*time.Millisecond)
	snapshot := filepath.Join(scratch.path, "snapshot")
	watch.setSnapshot(snapshot)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := os.CreateTemp(t.TempDir(), "launcher-output-")
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(stdout)
	trip := make(chan time.Time, 1)
	go func() { <-watch.ctx.Done(); trip <- time.Now() }()
	run := &command{name: executable, args: []string{"-test.run=^TestMutationStorageChild$"}, env: []string{"PDFCONCAT_MUTATION_STORAGE_CHILD=" + snapshot, "PDFCONCAT_MUTATION_STORAGE_BYTES=2048"}, stdout: stdout, stderr: stdout}
	runErr := run.runMutation(watch.ctx, 0)
	if runErr == nil || !errors.Is(context.Cause(watch.ctx), errCampaignResourceStop) {
		t.Fatalf("resource threshold failed to stop actual launcher: %v/%v", runErr, context.Cause(watch.ctx))
	}
	if elapsed := time.Since(<-trip); elapsed > 5*time.Second {
		t.Fatalf("resource cancellation waited ordinary grace: %v", elapsed)
	}
	data, err := readInRoot(snapshot, "pid")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	requireCampaignProducerExited(t, pid)
	result := mutationCommandFailure(filepath.Join(t.TempDir(), "absent.json"), runErr)
	finishMutationStorage(&result, scratch, watch, false)
	if !errors.Is(result, errGate) || !errors.Is(result, errMutationCleanupUncertain) {
		t.Fatalf("resource cause or missing SDK ownership lost: %v", result)
	}
	if data, err = readInRoot(snapshot, "payload"); err != nil || len(data) != 2048 {
		t.Fatalf("unsettled storage removed: %d/%v", len(data), err)
	}
}

func TestMutationInitialCapacityRejectionNeverStartsChild(t *testing.T) {
	t.Parallel()
	scratch, err := newCampaignScratch(t.TempDir(), "qualitygate-mutation-control-")
	if err != nil {
		t.Fatal(err)
	}
	watch := startMutationStorageWatch(t.Context(), scratch, campaignDiskUsage{4096, 100}, math.MaxUint64, time.Hour, time.Hour)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := os.CreateTemp(t.TempDir(), "not-started-")
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(stdout)
	result := (&command{name: executable, args: []string{"-test.run=^TestMutationStorageChild$"}, env: []string{"PDFCONCAT_MUTATION_STORAGE_CHILD=" + scratch.path, "PDFCONCAT_MUTATION_STORAGE_BYTES=1"}, stdout: stdout, stderr: stdout}).runMutation(watch.ctx, 0)
	if result == nil {
		t.Fatal("impossible native capacity allowed launch")
	}
	if _, err = readInRoot(scratch.path, "pid"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("child started before capacity rejection: %v", err)
	}
	finishMutationStorage(&result, scratch, watch, false)
	if !errors.Is(result, errGate) {
		t.Fatalf("capacity rejection lost: %v", result)
	}
	if _, err = os.Stat(scratch.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty rejected storage retained: %v", err)
	}
}

func TestMutationStorageHeartbeat(t *testing.T) {
	path := os.Getenv("PDFCONCAT_MUTATION_STORAGE_HEARTBEAT")
	if path == "" {
		return
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(root)
	for {
		if err = root.WriteFile("heartbeat", []byte("producer active"), fileMode); err != nil {
			t.Fatal(err)
		}
		if _, err := root.Lstat("release"); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestMutationKnownReportPathsAreCountedWithoutOtherCallerFiles(t *testing.T) {
	t.Parallel()
	scratch, watch := mutationStorageFixture(t, campaignDiskUsage{100, 100})
	if err := scratch.root.Mkdir("snapshot", dirMode); err != nil {
		t.Fatal(err)
	}
	watch.setSnapshot(filepath.Join(scratch.path, "snapshot"))
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "unrelated"), make([]byte, 1024), fileMode); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(directory, "report.json")
	if err := watch.setReport(report, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, make([]byte, 101), fileMode); err != nil {
		t.Fatal(err)
	}
	result := watch.checkpoint(nil)
	if !errors.Is(result, errGate) {
		t.Fatalf("external report bytes ignored: %v", result)
	}
	finishMutationStorage(&result, scratch, watch, false)
	if data, err := readInRoot(directory, "unrelated"); err != nil || len(data) != 1024 {
		t.Fatalf("unrelated caller files changed: %d/%v", len(data), err)
	}
}

func TestMutationDefaultReportInsideScratchIsNotCountedTwice(t *testing.T) {
	t.Parallel()
	scratch, watch := mutationStorageFixture(t, campaignDiskUsage{96, 100})
	if err := scratch.root.Mkdir("snapshot", dirMode); err != nil {
		t.Fatal(err)
	}
	watch.setSnapshot(filepath.Join(scratch.path, "snapshot"))
	report := filepath.Join(scratch.path, "snapshot", "report.json")
	if err := watch.setReport(report, true); err != nil {
		t.Fatal(err)
	}
	if err := scratch.root.WriteFile("snapshot/report.json", make([]byte, 64), fileMode); err != nil {
		t.Fatal(err)
	}
	var result error
	finishMutationStorage(&result, scratch, watch, false)
	if result != nil {
		t.Fatalf("internal report double-counted: %v", result)
	}
}

func TestCampaignScratchBindingFailurePreservesUnknownEntry(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	path := filepath.Join(parent, "unknown")
	if err := os.WriteFile(path, []byte("preserve unverified user entry"), fileMode); err != nil {
		t.Fatal(err)
	}
	if scratch, err := openCampaignScratch(path); scratch != nil || err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("failed ownership was accepted or lost path: %v/%v", scratch, err)
	}
	if data, err := readInRoot(parent, "unknown"); err != nil || string(data) != "preserve unverified user entry" {
		t.Fatalf("unverified initialization deleted entry: %q/%v", data, err)
	}
}

func TestCampaignScratchCleanupRemovesMultipleBoundedBatches(t *testing.T) {
	t.Parallel()
	scratch, err := newCampaignScratch(t.TempDir(), "qualitygate-cleanup-control-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeAll(scratch.path) })
	for index := range 137 {
		name := fmt.Sprintf("entry-%03d", index)
		if index%2 == 0 {
			if err := scratch.root.Mkdir(name, dirMode); err != nil {
				t.Fatal(err)
			}
			name += "/nested"
		}
		if err := scratch.root.WriteFile(name, []byte("owned"), fileMode); err != nil {
			t.Fatal(err)
		}
	}
	if err := scratch.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(scratch.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settled multi-batch scratch remains: %v", err)
	}
}
