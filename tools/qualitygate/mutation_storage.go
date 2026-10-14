// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/resoltico/pdfconcat/internal/capture"
)

const mutationRawAllowance int64 = 17 << 27 // 2 GiB mutants + four 32 MiB setup commands.

type mutationStorageWatch struct {
	ctx                         context.Context
	cancel                      context.CancelCauseFunc
	stop, done                  chan struct{}
	scratch                     *campaignScratch
	limit                       campaignDiskUsage
	reserve                     uint64
	mutex                       sync.Mutex
	failure                     error
	report                      *os.Root
	reportParent                string
	reportIdentity              os.FileInfo
	reportPaths                 []string
	snapshotName                string
	reportDefault               bool
	liveSamples, partialSamples int
	volumeSamples               int
	maximum                     campaignDiskUsage
	minimumAvailable            uint64
	lastPartial                 error
}

func newMutationStorageWatch(ctx context.Context, scratch *campaignScratch, workers int) *mutationStorageWatch {
	return startMutationStorageWatch(ctx, scratch, campaignDiskUsage{int64(workers)*(6<<30) + mutationRawAllowance, int64(workers) * 65536}, campaignFinalizationBytes, time.Second, 5*time.Second)
}

func startMutationStorageWatch(ctx context.Context, scratch *campaignScratch, limit campaignDiskUsage, reserve uint64, freeInterval, treeInterval time.Duration) *mutationStorageWatch {
	ctx, cancel := context.WithCancelCause(ctx)
	w := &mutationStorageWatch{ctx: ctx, cancel: cancel, stop: make(chan struct{}), done: make(chan struct{}), scratch: scratch, limit: limit, reserve: reserve, minimumAvailable: ^uint64(0)}
	initial := w.observe(true, false)
	go func() {
		defer close(w.done)
		if initial != nil {
			return
		}
		ticker := time.NewTicker(freeInterval)
		defer ticker.Stop()
		lastTree := time.Now()
		for {
			select {
			case <-w.stop:
				return
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				tree := now.Sub(lastTree) >= treeInterval
				if err := w.observe(tree, false); err != nil {
					return
				}
				if tree {
					lastTree = now
				}
			}
		}
	}()
	return w
}

func (w *mutationStorageWatch) environment() []string {
	return []string{"TMPDIR=" + w.scratch.path, "TMP=" + w.scratch.path, "TEMP=" + w.scratch.path, "GOTMPDIR=" + w.scratch.path}
}

func (w *mutationStorageWatch) setReport(path string, defaultReport bool) error {
	// Use the same qualified native-parent binding as mutation flag parsing.
	bound, err := capture.NativeArtifactPath(path)
	if err != nil {
		return err
	}
	parent := capture.NativeParent(bound)
	relative, relErr := filepath.Rel(w.scratch.path, parent)
	if relErr == nil && filepath.IsLocal(relative) {
		w.mutex.Lock()
		w.reportDefault = defaultReport
		w.mutex.Unlock()
		return nil // Already inside the owned tree.
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	identity, err := root.Stat(".")
	if err != nil {
		return errors.Join(err, root.Close())
	}
	w.mutex.Lock()
	w.report, w.reportParent, w.reportIdentity = root, parent, identity
	name := filepath.Base(bound)
	w.reportPaths = []string{name, name + ".scope.json", name + ".discovery.json", name + ".executions", name + ".discovery.json.executions"}
	w.reportDefault = defaultReport
	w.mutex.Unlock()
	return w.observe(false, false)
}

func (w *mutationStorageWatch) observe(tree, final bool) error {
	w.mutex.Lock()
	err := w.inspect(tree, final)
	if err != nil {
		err = errors.Join(errCampaignResourceStop, fmt.Errorf("%w: mutation disk resource guard: %w", errGate, err))
		w.failure = errors.Join(w.failure, err)
	}
	w.mutex.Unlock()
	if err != nil {
		w.cancel(err)
	}
	return err
}

func (w *mutationStorageWatch) inspect(tree, final bool) error {
	parent, err := os.OpenRoot(filepath.Dir(w.scratch.path))
	if err != nil {
		return err
	}
	err = errors.Join(w.scratch.verifyNamespace(parent), parent.Close())
	if err != nil {
		return errors.Join(errMutationCleanupUncertain, err)
	}
	if err = w.inspectCapacity(w.scratch.path); err != nil {
		return err
	}
	if w.report != nil {
		current, err := os.Stat(w.reportParent)
		if err != nil || !os.SameFile(w.reportIdentity, current) {
			return errors.Join(errMutationCleanupUncertain, fmt.Errorf("%w: mutation report parent changed: %v", errGate, err))
		}
		if err = w.inspectCapacity(w.reportParent); err != nil {
			return err
		}
	}
	if !tree {
		return nil
	}
	if final {
		if err := w.requireIdleScratch(); err != nil {
			return err
		}
	}
	sample, err := countCampaignDiskTree(w.scratch.root, ".", w.limit)
	if err != nil {
		return fmt.Errorf("%w: mutation storage resource threshold: %w", errGate, err)
	}
	if w.report != nil {
		for _, path := range w.reportPaths {
			remainder := campaignDiskUsage{w.limit.bytes - sample.usage.bytes, w.limit.entries - sample.usage.entries}
			extra, err := countCampaignDiskTree(w.report, path, remainder)
			if err != nil {
				return fmt.Errorf("%w: mutation report resource threshold: %w", errGate, err)
			}
			sample.usage.bytes += extra.usage.bytes
			sample.usage.entries += extra.usage.entries
			if sample.incomplete == nil {
				sample.incomplete = extra.incomplete
			}
		}
	}
	w.liveSamples++
	w.maximum.bytes = max(w.maximum.bytes, sample.usage.bytes)
	w.maximum.entries = max(w.maximum.entries, sample.usage.entries)
	if sample.incomplete != nil {
		w.partialSamples++
		w.lastPartial = sample.incomplete
		if final {
			return fmt.Errorf("%w: final mutation storage inspection incomplete: %w", errGate, sample.incomplete)
		}
	}
	return nil
}

// An interrupted SDK may have unjoined writers. Only the existing terminal
// ownership verdict permits a final complete inspection or disposable cleanup.
func (w *mutationStorageWatch) finish(producerResult error) error {
	close(w.stop)
	<-w.done
	if !errors.Is(producerResult, errMutationCleanupUncertain) {
		if err := w.observe(true, true); err != nil && w.failure == nil {
			w.failure = err
		}
	}
	w.cancel(nil)
	result := w.failure
	if w.report != nil {
		result = errors.Join(result, w.report.Close())
	}
	available := "unavailable"
	if w.volumeSamples > 0 {
		available = fmt.Sprint(w.minimumAvailable)
	}
	log.Printf("mutation: disk observations: %d tree samples (%d incomplete), maximum observed logical bytes/entries %d/%d, minimum available volume bytes %s (%d successful volume queries); thresholds %d bytes/%d entries, finalization %d bytes; non-atomic observations, not a quota", w.liveSamples, w.partialSamples, w.maximum.bytes, w.maximum.entries, available, w.volumeSamples, w.limit.bytes, w.limit.entries, w.reserve)
	if w.lastPartial != nil {
		log.Printf("mutation: last incomplete live disk observation: %q", w.lastPartial)
	}
	return result
}

func finishMutationStorage(result *error, scratch *campaignScratch, watch *mutationStorageWatch, keep bool) {
	*result = errors.Join(*result, watch.finish(*result))
	if errors.Is(*result, errMutationCleanupUncertain) || keep || (watch.reportDefault && watch.failure != nil) {
		log.Printf("mutation: retained source/evidence storage %s", scratch.path)
		*result = errors.Join(*result, scratch.retain())
		return
	}
	*result = errors.Join(*result, scratch.close())
}

func (w *mutationStorageWatch) setSnapshot(path string) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.snapshotName = filepath.Base(path)
}

// SDK reporting precedes its deferred outer-directory cleanup. Even a complete
// report cannot authorize deleting unknown remaining SDK child directories.
func (w *mutationStorageWatch) requireIdleScratch() error {
	directory, err := w.scratch.root.Open(".")
	if err != nil {
		return err
	}
	result := func() error {
		for {
			entries, readErr := directory.ReadDir(64)
			for _, entry := range entries {
				if entry.Name() != w.snapshotName {
					return fmt.Errorf("%w: retain unexpected SDK temporary entry %q", errMutationCleanupUncertain, entry.Name())
				}
			}
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}()
	return errors.Join(result, directory.Close())
}

func (w *mutationStorageWatch) checkpoint(producerResult error) error {
	if errors.Is(producerResult, errMutationCleanupUncertain) {
		return nil
	}
	return w.observe(true, true)
}

func (w *mutationStorageWatch) inspectCapacity(path string) error {
	available, err := requireCampaignCapacity(path, w.reserve)
	if !errors.Is(err, errCampaignCapacityQuery) {
		w.volumeSamples++
		w.minimumAvailable = min(w.minimumAvailable, available)
	}
	return err
}
