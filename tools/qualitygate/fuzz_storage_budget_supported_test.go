// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFuzzStorageCounterRejectsBytesEntriesAndLinks(t *testing.T) {
	t.Parallel()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(root)
	if err = root.WriteFile("first", []byte("1234"), fileMode); err != nil {
		t.Fatal(err)
	}
	usage, err := countFuzzTree(root, ".", fuzzDiskUsage{4, 1})
	if err != nil || usage != (fuzzDiskUsage{4, 1}) {
		t.Fatalf("exact boundary: %v/%v", usage, err)
	}
	if _, err = countFuzzTree(root, ".", fuzzDiskUsage{3, 1}); err == nil {
		t.Fatal("next byte accepted")
	}
	if err = root.WriteFile("empty", nil, fileMode); err != nil {
		t.Fatal(err)
	}
	if _, err = countFuzzTree(root, ".", fuzzDiskUsage{4, 1}); err == nil {
		t.Fatal("empty extra entry accepted")
	}
	if err = root.Remove("empty"); err != nil {
		t.Fatal(err)
	}
	if err = root.Link("first", "hard"); err != nil {
		t.Fatal(err)
	}
	if _, err = countFuzzTree(root, ".", fuzzDiskUsage{7, 2}); err == nil {
		t.Fatal("hard link logical bytes omitted")
	}
	if err = root.Remove("hard"); err != nil {
		t.Fatal(err)
	}
	if err = root.Symlink(t.TempDir(), "outside"); err != nil {
		t.Fatal(err)
	}
	if _, err = countFuzzTree(root, ".", fuzzDiskUsage{100, 100}); err == nil || !strings.Contains(err.Error(), "nonregular") {
		t.Fatalf("link followed or accepted: %v", err)
	}
}

func TestFuzzStorageCounterBoundsLargeDirectoryAndSparseFiles(t *testing.T) {
	t.Parallel()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
	}()
	for i := range 150 {
		if err = root.WriteFile(strconv.Itoa(i), nil, fileMode); err != nil {
			t.Fatal(err)
		}
	}
	usage, err := countFuzzTree(root, ".", fuzzDiskUsage{1024, 64})
	if err == nil || usage.entries != 65 {
		t.Fatalf("streamed entry rejection: %v/%v", usage, err)
	}
	file, err := root.Create("sparse")
	if err != nil {
		t.Fatal(err)
	}
	if err = errors.Join(file.Truncate(4096), file.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err = countFuzzTree(root, ".", fuzzDiskUsage{4095, 200}); err == nil {
		t.Fatal("sparse logical size accepted")
	}
	if err = root.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = countFuzzTree(root, ".", fuzzDiskUsage{4096, 200}); err == nil {
		t.Fatal("inspection error accepted")
	}
}

func storageWatchFixture(t *testing.T, limit fuzzDiskUsage) (*fuzzStorage, *fuzzStorageWatch) {
	t.Helper()
	storage, err := newFuzzStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	watch := startFuzzStorageWatch(t.Context(), storage, limit, time.Hour)
	t.Cleanup(func() { removeAll(storage.path) })
	return storage, watch
}

func TestFuzzStorageWatchCorpusBaselineAndAbsentAncestors(t *testing.T) {
	t.Parallel()
	storage, watch := storageWatchFixture(t, fuzzDiskUsage{4096, 8})
	source := t.TempDir()
	path := filepath.Join(source, "testdata", "fuzz", "FuzzKnown")
	if err := os.MkdirAll(path, dirMode); err != nil {
		t.Fatal(err)
	}
	baseline := filepath.Join(path, "existing")
	if err := os.WriteFile(baseline, []byte("user corpus"), fileMode); err != nil {
		t.Fatal(err)
	}
	targets := []fuzzTarget{{pkg: "fuzz.fixture", name: "FuzzKnown"}, {pkg: "fuzz.fixture/child", name: "FuzzAbsent"}}
	if err := watch.watchCorpus(source, "fuzz.fixture", targets); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "new"), []byte("failure evidence"), fileMode); err != nil {
		t.Fatal(err)
	}
	if err := watch.check(true); err != nil {
		t.Fatal(err)
	}
	absent := filepath.Join(source, "child")
	if err := os.Symlink(t.TempDir(), absent); err != nil {
		t.Fatal(err)
	}
	if err := watch.finish(nil); err == nil || !strings.Contains(err.Error(), "substituted") {
		t.Fatalf("new ancestor symlink accepted: %v", err)
	}
	if err := storage.close(); err != nil {
		t.Fatal(err)
	}
	if data, err := readInRoot(path, "existing"); err != nil || string(data) != "user corpus" {
		t.Fatalf("baseline modified: %q/%v", data, err)
	}
	if data, err := readInRoot(path, "new"); err != nil || string(data) != "failure evidence" {
		t.Fatalf("failure input removed: %q/%v", data, err)
	}
}

func TestFuzzStorageWatchRejectsNewCorpusBytesAndEmptyEntries(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"bytes", "entries", "replacement"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			storage, watch := storageWatchFixture(t, fuzzDiskUsage{4096, 4})
			source := t.TempDir()
			path := filepath.Join(source, "testdata", "fuzz", "FuzzKnown")
			if err := os.MkdirAll(path, dirMode); err != nil {
				t.Fatal(err)
			}
			if err := watch.watchCorpus(source, "fuzz.fixture", []fuzzTarget{{pkg: "fuzz.fixture", name: "FuzzKnown"}}); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "bytes":
				root, err := os.OpenRoot(path)
				if err != nil {
					t.Fatal(err)
				}
				defer closeLogged(root)
				file, err := root.Create("new")
				if err != nil {
					t.Fatal(err)
				}
				if err = errors.Join(file.Truncate(fuzzCorpusGrowthBytes+1), file.Close()); err != nil {
					t.Fatal(err)
				}
			case "entries":
				for i := range 5 {
					if err := os.WriteFile(filepath.Join(path, strconv.Itoa(i)), nil, fileMode); err != nil {
						t.Fatal(err)
					}
				}
			case "replacement":
				if err := os.Rename(path, path+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, dirMode); err != nil {
					t.Fatal(err)
				}
			}
			if err := watch.finish(nil); !errors.Is(err, errGate) {
				t.Fatalf("corpus %s accepted: %v", kind, err)
			}
			if err := storage.close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFuzzStorageWatchRejectsOwnedRootSubstitution(t *testing.T) {
	t.Parallel()
	storage, watch := storageWatchFixture(t, fuzzDiskUsage{4096, 100})
	moved := storage.path + "-original"
	t.Cleanup(func() { removeAll(moved) })
	if err := os.Rename(storage.path, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(storage.path, dirMode); err != nil {
		t.Fatal(err)
	}
	if err := watch.finish(nil); !errors.Is(err, errGate) {
		t.Fatalf("substitution accepted: %v", err)
	}
	if err := storage.close(); err == nil {
		t.Fatal("substitution cleaned")
	}
}

func TestFuzzStorageBudgetChild(t *testing.T) {
	path := os.Getenv("PDFCONCAT_STORAGE_BUDGET_CHILD")
	if path == "" {
		return
	}
	size, err := strconv.Atoi(os.Getenv("PDFCONCAT_STORAGE_BUDGET_BYTES"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(root)
	if err = root.WriteFile("pid", []byte(strconv.Itoa(os.Getpid())), fileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Println("owned writer began"); err != nil {
		t.Fatal(err)
	}
	if err = root.WriteFile("payload", make([]byte, size), fileMode); err != nil {
		t.Fatal(err)
	}
	if size > 1024 {
		time.Sleep(time.Hour)
	}
}

func TestFuzzStorageBudgetCancelsJoinsAndRetainsEvidence(t *testing.T) {
	t.Parallel()
	for _, size := range []int{512, 2048} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			t.Parallel()
			storage, err := newFuzzStorage(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			watch := startFuzzStorageWatch(ctx, storage, fuzzDiskUsage{1024, 20}, 5*time.Millisecond)
			evidence, err := openTestEvidence("")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { removeAll(evidence.path); removeAll(evidence.path + ".stderr"); removeAll(storage.path) })
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			tmp := filepath.Join(storage.path, "tmp")
			run := &command{name: executable, args: []string{"-test.run=^TestFuzzStorageBudgetChild$"}, env: []string{"PDFCONCAT_STORAGE_BUDGET_CHILD=" + tmp, "PDFCONCAT_STORAGE_BUDGET_BYTES=" + strconv.Itoa(size)}, stdout: evidence.stdout, stderr: evidence.stderr, fuzzLifecycle: true}
			runErr := run.run(watch.ctx)
			watchErr := watch.finish(runErr)
			result := errors.Join(runErr, watchErr, evidence.closeFiles())
			if err = evidence.finish(result); err != nil {
				t.Fatal(err)
			}
			if size > 1024 {
				if !errors.Is(watchErr, errGate) || runErr == nil || ctx.Err() != nil {
					t.Fatalf("resource threshold did not independently fail child: %v/%v/%v", runErr, watchErr, ctx.Err())
				}
				data, err := os.ReadFile(evidence.path)
				if err != nil || !strings.Contains(string(data), "owned writer began") {
					t.Fatalf("partial evidence missing: %q/%v", data, err)
				}
			} else if result != nil {
				t.Fatalf("healthy child rejected: %v", result)
			}
			data, err := readInRoot(tmp, "pid")
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(string(data))
			if err != nil {
				t.Fatal(err)
			}
			requireFuzzWorkerExited(t, pid)
			if err := storage.finish(result); size <= 1024 && err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(storage.path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("settled owned storage remained: %v", err)
			}
		})
	}
}

// A target can complete before the first periodic observation. Its actual PASS
// events must still become an infrastructure failure, with raw evidence retained.
func TestFuzzStorageFinalCheckRejectsPassingTargetAndKeepsEvidence(t *testing.T) {
	evidenceParent := t.TempDir()
	t.Setenv("TMPDIR", evidenceParent)
	for _, size := range []int{512, 2048} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			dir := fuzzLifecycleFixture(t)
			source := `package fixture
import("os";"testing")
func FuzzDisk(f *testing.F){f.Add(1);f.Fuzz(func(t *testing.T,n int){
 if err:=os.WriteFile(os.Getenv("PDFCONCAT_DISK_FILE"),make([]byte,` + strconv.Itoa(size) + `),0600);err!=nil{t.Fatal(err)}
})}`
			if err := os.WriteFile(filepath.Join(dir, "fuzz_test.go"), []byte(source), fileMode); err != nil {
				t.Fatal(err)
			}
			storage, err := newFuzzStorage(dir)
			if err != nil {
				t.Fatal(err)
			}
			watch := startFuzzStorageWatch(t.Context(), storage, fuzzDiskUsage{1024, 20}, time.Hour)
			environment := []string{"TMPDIR=" + filepath.Join(storage.path, "tmp"), "PDFCONCAT_DISK_FILE=" + filepath.Join(storage.path, "tmp", "payload")}
			result := runFuzzTarget(watch.ctx, dir, fuzzTarget{pkg: fuzzFixturePackage, name: "FuzzDisk"}, "1x", environment, 2, watch.observeJoined)
			result = errors.Join(result, watch.finish(result))
			matches, err := filepath.Glob(filepath.Join(evidenceParent, "qualitygate-test-events-*"))
			if err != nil {
				t.Fatal(err)
			}
			if size <= 1024 {
				if result != nil || len(matches) != 0 {
					t.Fatalf("healthy fuzz target rejected or retained evidence: %v/%v", result, matches)
				}
			} else {
				if !errors.Is(result, errGate) || !strings.Contains(result.Error(), "logical bytes") || len(matches) != 2 {
					t.Fatalf("passing target exceeded storage without retained infrastructure failure: %v/%v", result, matches)
				}
				var retained int64
				for _, path := range matches {
					info, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					retained += info.Size()
				}
				if retained == 0 || retained > fuzzEvidenceLimit {
					t.Fatalf("raw evidence outside existing byte bound: %d", retained)
				}
			}
			cleanupResult := storage.finish(result)
			if size <= 1024 && cleanupResult != nil {
				t.Fatal(cleanupResult)
			}
			if size > 1024 && !errors.Is(cleanupResult, errGate) {
				t.Fatalf("resource classification lost at cleanup: %v", cleanupResult)
			}
		})
	}
}

func TestFuzzStorageWatchAcceptsRepositoryAliasAndRejectsRetargeting(t *testing.T) {
	t.Parallel()
	for _, retarget := range []bool{false, true} {
		t.Run(strconv.FormatBool(retarget), func(t *testing.T) {
			t.Parallel()
			storage, watch := storageWatchFixture(t, fuzzDiskUsage{4096, 20})
			source := t.TempDir()
			alias := filepath.Join(t.TempDir(), "repository")
			if err := os.Symlink(source, alias); err != nil {
				t.Fatal(err)
			}
			if err := watch.watchCorpus(alias, "fuzz.fixture", []fuzzTarget{{pkg: "fuzz.fixture", name: "FuzzKnown"}}); err != nil {
				t.Fatal(err)
			}
			if retarget {
				if err := os.Remove(alias); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), alias); err != nil {
					t.Fatal(err)
				}
			}
			err := watch.finish(nil)
			if retarget && !errors.Is(err, errGate) {
				t.Fatalf("retargeted source accepted: %v", err)
			}
			if !retarget && err != nil {
				t.Fatalf("supported repository alias rejected: %v", err)
			}
			if err := storage.close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFuzzStorageWatchUncertainProducerRetainsExactStorage(t *testing.T) {
	t.Parallel()
	storage, watch := storageWatchFixture(t, fuzzDiskUsage{1024, 20})
	if err := storage.root.WriteFile("tmp/unknown-producer", make([]byte, 2048), fileMode); err != nil {
		t.Fatal(err)
	}
	result := errors.Join(errFuzzCleanupUncertain, watch.finish(errFuzzCleanupUncertain))
	if !errors.Is(result, errFuzzCleanupUncertain) || errors.Is(result, errGate) {
		t.Fatalf("uncertainty misreported as settled resource result: %v", result)
	}
	result = storage.finish(result)
	if !errors.Is(result, errFuzzCleanupUncertain) || !strings.Contains(result.Error(), storage.path) {
		t.Fatalf("retention path or uncertainty lost: %v", result)
	}
	if data, err := readInRoot(storage.path, "tmp/unknown-producer"); err != nil || len(data) != 2048 {
		t.Fatalf("uncertain producer data removed: %d/%v", len(data), err)
	}
}

func TestFuzzCorpusBindingPreservesNativeParentTraversal(t *testing.T) {
	t.Parallel()
	storage, watch := storageWatchFixture(t, fuzzDiskUsage{4096, 20})
	base := t.TempDir()
	logical, physical := filepath.Join(base, "logical"), filepath.Join(base, "physical")
	if err := os.Mkdir(logical, dirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(physical, "child"), dirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(physical, "child"), filepath.Join(logical, "link")); err != nil {
		t.Fatal(err)
	}
	bound := logical + "/link/.."
	bindErr := watch.watchCorpus(bound, "fuzz.fixture", []fuzzTarget{{pkg: "fuzz.fixture", name: "FuzzKnown"}})
	selected := watch.sourceIdentity
	finishErr := watch.finish(nil)
	closeErr := storage.close()
	if err := errors.Join(bindErr, finishErr, closeErr); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Stat(physical)
	if err != nil {
		t.Fatal(err)
	}
	if selected == nil || !os.SameFile(selected, expected) {
		t.Fatal("corpus root collapsed native link/.. traversal to a different directory")
	}
}

func TestFuzzPrivateStorageQualifiesActualLiveRenameAndRequiresSettledInspection(t *testing.T) {
	t.Parallel()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(root)
	if err := root.WriteFile("compiler-output", []byte("temporary"), fileMode); err != nil {
		t.Fatal(err)
	}
	directory, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	entries, readErr := directory.ReadDir(64)
	if err := errors.Join(readErr, directory.Close()); err != nil || len(entries) != 1 {
		t.Fatalf("native directory sample: %v/%v", entries, err)
	}
	if err := root.Rename(entries[0].Name(), "completed-output"); err != nil {
		t.Fatal(err)
	}
	_, vanished := root.Lstat(entries[0].Name())
	if !errors.Is(vanished, os.ErrNotExist) || !incompleteCampaignInspection(vanished) {
		t.Fatalf("real compiler rename did not produce qualified disappearance: %v", vanished)
	}
	sample := campaignDiskSample{incomplete: vanished}
	if err := fuzzStorageSampleError(sample, nil, false); err != nil {
		t.Fatalf("live compiler rename rejected: %v", err)
	}
	if err := fuzzStorageSampleError(sample, nil, true); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete settled observation accepted: %v", err)
	}
	for _, settled := range []bool{false, true} {
		if err := fuzzStorageSampleError(sample, os.ErrClosed, settled); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("unexpected inspection failure hidden: %v", err)
		}
		if err := fuzzStorageSampleError(campaignDiskSample{nonregular: "unexpected-link"}, nil, settled); err == nil {
			t.Fatal("nonregular private entry accepted")
		}
	}
	if _, err := countFuzzTree(root, ".", fuzzDiskUsage{4096, 20}); err != nil {
		t.Fatalf("completed actual rename did not permit strict final inspection: %v", err)
	}
}
