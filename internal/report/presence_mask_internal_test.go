// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"reflect"
	"testing"
)

func TestStructuralRequiredMembersFitPresenceMask(t *testing.T) {
	t.Parallel()

	capacity := reflect.TypeOf(frame{}.seen).Bits()
	if maxRequiredBits != capacity {
		t.Fatalf("required-member cutoff %d differs from presence-mask capacity %d", maxRequiredBits, capacity)
	}

	for name, model := range shapes() {
		if len(model.required) > capacity || len(model.required) > maxRequiredBits {
			t.Errorf("%s requires %d members beyond presence capacity %d/cutoff %d", name, len(model.required), capacity, maxRequiredBits)
		}
	}
}
