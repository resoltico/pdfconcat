// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package scale_test

import (
	"encoding/json"
	"testing"

	"github.com/resoltico/pdfconcat/test/scale"
)

func TestMeasuredRowSerializesActualNativeCollectorProvenance(t *testing.T) {
	t.Parallel()

	measured, err := scale.Measure(t.Context(), scale.RunSpec{Binary: "/usr/bin/true", Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	if measured.DescriptorCollector == nil {
		t.Fatal("actual native collector identity missing before row projection")
	}

	row := newRow(&acceptanceCase{name: "collector-provenance"}, &scale.Workload{}, &measured)

	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}

	var persisted struct {
		Collector *scale.DescriptorCollectorIdentity `json:"descriptor_collector"`
	}
	if decodeErr := json.Unmarshal(encoded, &persisted); decodeErr != nil {
		t.Fatal(decodeErr)
	}

	assertPersistedCollectorIdentity(t, measured.DescriptorCollector, persisted.Collector)
}

func assertPersistedCollectorIdentity(t *testing.T, measured, persisted *scale.DescriptorCollectorIdentity) {
	t.Helper()

	if persisted == nil {
		t.Fatal("serialized result dropped actual measured native collector identity")
	}

	if persisted.NativeCompilerIdentity != measured.NativeCompilerIdentity || persisted.SourceSHA256 != measured.SourceSHA256 ||
		persisted.BinarySHA256 != measured.BinarySHA256 {
		t.Fatal("serialized result changed native compiler/source/executable provenance")
	}

	if persisted.ReadinessPID != measured.ReadinessPID || persisted.ReadinessFrame != measured.ReadinessFrame ||
		!persisted.ReadinessStarted.Equal(measured.ReadinessStarted) || !persisted.ReadinessFinished.Equal(measured.ReadinessFinished) {
		t.Fatal("serialized result lost or changed separate parent readiness evidence")
	}
}
