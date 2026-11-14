// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package cli

import (
	"bytes"
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
)

func TestEffectiveEnvironmentTimestamp(t *testing.T) {
	for _, tc := range []struct {
		plan, environment string
		exit              int
		epoch             *int64
	}{
		{``, "17", 0, new(int64(17))}, {`"timestamp":0,`, "invalid", 0, new(int64(0))}, {``, "-1", 2, nil}, {``, "", 2, nil}, {``, "253402300800", 2, nil},
	} {
		var out, errout bytes.Buffer
		dir := t.TempDir()
		code := Run(t.Context(), []string{"check", "--plan", "-", "--json"}, strings.NewReader(`{"version":5,`+tc.plan+`"items":[{"page":{"size":"A4"}}]}`), &out, &errout, Host{WorkingDir: func() (string, error) { return dir, nil }, SourceDateEpoch: func() (string, bool) { return tc.environment, true }})
		if code != tc.exit || errout.Len() != 0 {
			t.Fatalf("exit %d stderr %s", code, errout.String())
		}
		var result core.Result
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if tc.epoch != nil && (result.Timestamp == nil || *result.Timestamp != *tc.epoch) {
			t.Fatalf("effective timestamp %v", result.Timestamp)
		}
	}
}
