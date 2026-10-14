// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	fuzzStorageBaseBytes   int64 = 5 << 30
	fuzzWorkerStorageBytes int64 = 100 << 20
	fuzzStorageEntries     int64 = 65536
	fuzzCorpusGrowthBytes  int64 = 128 << 20
)

type fuzzDiskUsage = campaignDiskUsage

type fuzzStorageWatch struct {
	ctx               context.Context
	cancel            context.CancelCauseFunc
	stop              chan struct{}
	done              chan struct{}
	storage           *fuzzStorage
	limit             fuzzDiskUsage
	mutex             sync.Mutex
	source            *os.Root
	sourcePath        string
	sourceSpelling    string
	sourceIdentity    os.FileInfo
	corpusPaths       []string
	corpusDirectories map[string]os.FileInfo
	baseline          fuzzDiskUsage
	result            error
	partialSamples    int
	lastPartial       error
}

// This is a sampled cancellation threshold, not a quota: a producer can write
// between scans. The base covers compiler files, corpus and shared-memory headers;
// Go's pinned fuzz coordinator additionally creates one 100 MiB file per worker.
// The frozen default worker count comes from runtime.GOMAXPROCS(0), whose
// pinned runtime backing value is int32; the byte calculation fits int64.
func newFuzzStorageWatch(ctx context.Context, storage *fuzzStorage, workers int) *fuzzStorageWatch {
	return startFuzzStorageWatch(ctx, storage, fuzzDiskUsage{fuzzStorageBaseBytes + int64(workers)*fuzzWorkerStorageBytes, fuzzStorageEntries}, time.Second)
}

func startFuzzStorageWatch(ctx context.Context, storage *fuzzStorage, limit fuzzDiskUsage, interval time.Duration) *fuzzStorageWatch {
	ctx, cancel := context.WithCancelCause(ctx)
	w := &fuzzStorageWatch{ctx: ctx, cancel: cancel, stop: make(chan struct{}), done: make(chan struct{}), storage: storage, limit: limit, corpusDirectories: map[string]os.FileInfo{}}
	initialErr := w.observe()
	go func() {
		defer close(w.done)
		if initialErr != nil {
			return
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := w.observe(); err != nil {
					return
				}
			}
		}
	}()
	return w
}

// watchCorpus counts only discovered targets' source corpus directories. Existing
// user inputs form a baseline and are never deleted or rewritten by this monitor.
func (w *fuzzStorageWatch) watchCorpus(sourcePath, module string, targets []fuzzTarget) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	spelling := sourcePath
	sourcePath, err := nativeCampaignDirectory(sourcePath)
	if err != nil {
		return err
	}
	source, err := os.OpenRoot(sourcePath)
	if err != nil {
		return err
	}
	identity, err := source.Stat(".")
	if err != nil {
		return errors.Join(err, source.Close())
	}
	w.source, w.sourcePath, w.sourceSpelling, w.sourceIdentity = source, sourcePath, spelling, identity
	seen := map[string]bool{}
	for _, target := range targets {
		relative := strings.TrimPrefix(target.pkg, module)
		if relative != "" && !strings.HasPrefix(relative, "/") {
			return fmt.Errorf("%w: fuzz package outside source module: %s", errGate, target.pkg)
		}
		path := filepath.Join(strings.TrimPrefix(relative, "/"), "testdata", "fuzz", target.name)
		if !filepath.IsLocal(path) {
			return fmt.Errorf("%w: nonlocal fuzz corpus path: %s", errGate, path)
		}
		if !seen[path] {
			seen[path] = true
			w.corpusPaths = append(w.corpusPaths, path)
		}
	}
	usage, err := w.corpusUsage(fuzzDiskUsage{math.MaxInt64, fuzzStorageEntries})
	if err != nil {
		return fmt.Errorf("existing target corpus inspection has a %d-entry allowance; select fewer package patterns if exceeded; inputs are preserved: %w", fuzzStorageEntries, err)
	}
	w.baseline = usage
	if usage.bytes > math.MaxInt64-fuzzCorpusGrowthBytes {
		return fmt.Errorf("%w: source corpus baseline size cannot be accounted safely", errGate)
	}
	return err
}

func (w *fuzzStorageWatch) finish(producerResult error) error {
	close(w.stop)
	<-w.done
	// A final scan catches successful short commands between observations. If
	// native cleanup is uncertain, stop observation and leave retention to the
	// storage owner; do not claim the producer has settled.
	if w.result == nil && !errors.Is(producerResult, errFuzzCleanupUncertain) {
		if err := w.observeJoined(); err != nil {
			w.result = err
		}
	}
	result := w.result
	if w.partialSamples > 0 {
		log.Printf("fuzz: %d incomplete live private-storage observations (non-atomic); last: %q", w.partialSamples, w.lastPartial)
	}
	w.cancel(nil)
	if w.source != nil {
		result = errors.Join(result, w.source.Close())
	}
	return result
}

func (w *fuzzStorageWatch) observe() error { return w.observeState(false) }

// Only joined producers authorize a complete private-tree inspection. Compiler
// scratch can disappear during live scans; source corpus remains strict always.
func (w *fuzzStorageWatch) observeJoined() error { return w.observeState(true) }

func (w *fuzzStorageWatch) observeState(settled bool) error {
	if err := w.check(settled); err != nil {
		err = fmt.Errorf("%w: fuzz resource inspection: %w", errGate, err)
		w.mutex.Lock()
		if w.result == nil {
			w.result = err
		}
		w.mutex.Unlock()
		w.cancel(err)
		return err
	}
	return nil
}

func (w *fuzzStorageWatch) check(settled bool) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	parent, err := os.OpenRoot(filepath.Dir(w.storage.path))
	if err != nil {
		return fmt.Errorf("%w: inspect fuzz resource ownership: %w", errGate, err)
	}
	err = w.storage.verifyNamespace(parent)
	err = errors.Join(err, parent.Close())
	if err != nil {
		return err
	}
	if _, err = requireCampaignCapacity(w.storage.path, campaignFinalizationBytes); err != nil {
		return err
	}
	sample, err := countCampaignDiskTree(w.storage.root, ".", w.limit)
	if err = fuzzStorageSampleError(sample, err, settled); err != nil {
		return fmt.Errorf("%w: fuzz storage resource threshold at %q: %w", errGate, w.storage.path, err)
	}
	if sample.incomplete != nil {
		w.partialSamples++
		w.lastPartial = sample.incomplete
	}
	owned := sample.usage
	if w.source == nil {
		return nil
	}
	alias, aliasErr := os.Stat(w.sourceSpelling)
	if aliasErr != nil || !os.SameFile(w.sourceIdentity, alias) {
		return fmt.Errorf("%w: fuzz source alias changed at %q: %v", errGate, w.sourceSpelling, aliasErr)
	}
	current, err := os.Lstat(w.sourcePath)
	if err != nil || !os.SameFile(w.sourceIdentity, current) || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: fuzz source ownership changed at %q: %v", errGate, w.sourcePath, err)
	}
	if _, err = requireCampaignCapacity(w.sourcePath, campaignFinalizationBytes); err != nil {
		return err
	}
	corpus, err := w.corpusUsage(fuzzDiskUsage{w.baseline.bytes + fuzzCorpusGrowthBytes, w.baseline.entries + w.limit.entries})
	if err != nil {
		return fmt.Errorf("%w: fuzz source corpus resource threshold: %w", errGate, err)
	}
	if owned.entries+max(corpus.entries-w.baseline.entries, 0) > w.limit.entries {
		return fmt.Errorf("%w: fuzz storage and new corpus exceed %d entries", errGate, w.limit.entries)
	}
	return nil
}

func (w *fuzzStorageWatch) corpusUsage(limit fuzzDiskUsage) (fuzzDiskUsage, error) {
	var total fuzzDiskUsage
	for _, path := range w.corpusPaths {
		// Check every ancestor, including initially absent directories as they appear.
		// Captured identities reject replacement even by another in-root directory.
		prefix := ""
		missing := false
		for _, part := range strings.Split(path, string(filepath.Separator)) {
			prefix = filepath.Join(prefix, part)
			info, err := w.source.Lstat(prefix)
			if errors.Is(err, os.ErrNotExist) {
				if w.corpusDirectories[prefix] != nil {
					return total, fmt.Errorf("corpus directory disappeared: %s", prefix)
				}
				missing = true
				break
			}
			if err != nil {
				return total, err
			}
			previous := w.corpusDirectories[prefix]
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || (previous != nil && !os.SameFile(previous, info)) {
				return total, fmt.Errorf("corpus directory substituted: %s", prefix)
			}
			w.corpusDirectories[prefix] = info
		}
		if missing {
			continue
		}
		usage, err := countFuzzTree(w.source, path, fuzzDiskUsage{limit.bytes - total.bytes, limit.entries - total.entries})
		if err != nil {
			return total, fmt.Errorf("%s: %w", path, err)
		}
		total.bytes += usage.bytes
		total.entries += usage.entries
	}
	return total, nil
}

// Private live compiler scratch can change during enumeration. Source corpus
// and settled private trees still require complete regular-file observations.
func fuzzStorageSampleError(sample campaignDiskSample, err error, settled bool) error {
	if sample.nonregular != "" {
		err = errors.Join(err, fmt.Errorf("nonregular entry: %s", sample.nonregular))
	}
	if settled {
		err = errors.Join(err, sample.incomplete)
	}
	return err
}

func countFuzzTree(root *os.Root, path string, limit fuzzDiskUsage) (fuzzDiskUsage, error) {
	sample, err := countCampaignDiskTree(root, path, limit)
	return sample.usage, fuzzStorageSampleError(sample, err, true)
}
