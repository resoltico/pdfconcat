// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package scale

import (
	"testing"
	"time"
)

// Expected serialized state vocabulary is independent of the sampler's implementation.
const (
	unavailableReading = "unavailable"
	validReading       = "valid"
	failedReading      = "error"
)

func TestMeasurementStatesRejectMissingAndFailedReadings(t *testing.T) {
	t.Parallel()

	var peaks processPeaks
	requireReadingStates(t, &peaks, 0, unavailableReading)
	peaks.record(processSample{descriptors: 0})
	requireReadingStates(t, &peaks, 0, unavailableReading)
	peaks = processPeaks{}
	peaks.record(processSample{descriptors: -1, rssError: true})
	requireReadingStates(t, &peaks, 0, failedReading)
	requireReadingCounts(t, &peaks, 0, 1)
	peaks = processPeaks{}
	peaks.record(processSample{descriptors: 10, rssBytes: 100})
	requireReadingStates(t, &peaks, 100, validReading)
	requireReadingCounts(t, &peaks, 1, 1)
	peaks.record(processSample{descriptors: -1, rssError: true})
	requireReadingStates(t, &peaks, 100, failedReading)
	requireReadingCounts(t, &peaks, 1, 2)
}

func requireReadingStates(t *testing.T, peaks *processPeaks, bytes int64, want string) {
	t.Helper()

	rss, descriptors := peaks.states(bytes)
	if rss != want || descriptors != want {
		t.Fatalf("readings states: RSS=%s descriptors=%s want %s", rss, descriptors, want)
	}
}

func requireReadingCounts(t *testing.T, peaks *processPeaks, successful, attempted int64) {
	t.Helper()

	if peaks.descriptorSamples != successful || peaks.sampleAttempts != attempted {
		t.Fatalf("reading counts: %+v", peaks)
	}
}

func TestTerminalObservationCannotMakeAnUnmeasuredProcessValid(t *testing.T) {
	t.Parallel()

	var peaks processPeaks
	peaks.record(processSample{descriptors: -1, terminal: true})
	requireReadingStates(t, &peaks, 0, unavailableReading)

	if peaks.sampleAttempts != 1 || peaks.terminalSamples != 1 || peaks.descriptorSamples != 0 || !peaks.firstDescriptor.IsZero() ||
		!peaks.lastDescriptor.IsZero() {
		t.Fatalf("terminalreading inventedcoverage: %+v", peaks)
	}
}

func TestMixedTerminalReadingRetainsKnownDescriptorBudgetViolationAndTimestamp(t *testing.T) {
	t.Parallel()

	observed := time.Now().Add(-time.Second)

	var peaks processPeaks
	peaks.record(processSample{descriptors: 95, descriptorAt: observed, rssError: true, terminal: true})

	rss, descriptors := peaks.states(100)
	if peaks.descriptors != 95 || peaks.descriptors <= 64 || peaks.descriptorSamples != 1 || peaks.descriptorTerminalSamples != 0 ||
		peaks.terminalSamples != 1 ||
		peaks.sampleAttempts != 1 ||
		!peaks.lastDescriptor.Equal(observed) ||
		descriptors != validReading ||
		rss != validReading {
		t.Fatalf("known highdescriptorreading lost or miscounted: %+v", peaks)
	}
}

func TestMixedTerminalReadingRetainsKnownRSSBudgetViolation(t *testing.T) {
	t.Parallel()

	var peaks processPeaks
	peaks.record(processSample{descriptors: 10, rssBytes: 100})
	peaks.record(processSample{descriptors: -1, rssBytes: 2 << 30, terminal: true})

	rss, descriptors := peaks.states(peaks.rssBytes)
	if peaks.rssBytes != 2<<30 || peaks.rssBytes <= 1<<30 || peaks.descriptorSamples != 1 || peaks.descriptorTerminalSamples != 1 ||
		peaks.terminalSamples != 1 ||
		peaks.sampleAttempts != 2 ||
		rss != validReading ||
		descriptors != validReading {
		t.Fatalf("known highRSSreading discarded: %+v", peaks)
	}
}

func TestZeroDescriptorReadingsCannotInventAvailableMeasurement(t *testing.T) {
	t.Parallel()

	var peaks processPeaks
	peaks.record(processSample{descriptors: 0})

	_, state := peaks.states(100)
	if state != unavailableReading || peaks.descriptorSamples != 1 || peaks.sampleAttempts != 1 {
		t.Fatalf("zero reading availability: state=%s peaks=%+v", state, peaks)
	}

	peaks.record(processSample{descriptors: 5})

	_, state = peaks.states(100)
	if state != validReading || peaks.descriptorSamples != 2 || peaks.sampleAttempts != 2 || peaks.descriptors != 5 {
		t.Fatalf("known zero damaged prior positive reading: state=%s peaks=%+v", state, peaks)
	}
}
