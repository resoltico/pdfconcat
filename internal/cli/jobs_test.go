// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package cli_test

import (
	"math"
	"strconv"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
)

// TestParseJobsAcceptsTheWholeRange pins both ends of the accepted --jobs range: one worker, and the
// largest int, which must not be rejected as too big.
func TestParseJobsAcceptsTheWholeRange(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name  string
		value string
		want  int
	}{
		{name: "smallest", value: "1", want: 1},
		{name: "largest int", value: strconv.Itoa(math.MaxInt), want: math.MaxInt},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			got, err := cli.Parse([]string{string(cli.NameCheck), "--jobs=" + row.value, sourceAPath})
			if err != nil {
				t.Fatalf("--jobs=%s rejected: %v", row.value, err)
			}

			if got.Jobs != row.want {
				t.Errorf("--jobs=%s gave Jobs %d, want %d", row.value, got.Jobs, row.want)
			}
		})
	}
}
