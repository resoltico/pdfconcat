// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestResponseSchemaKeepsCompleteReportDefinitions(t *testing.T) {
	t.Parallel()

	var complete, response map[string]any
	if err := json.Unmarshal([]byte(report.Schema()), &complete); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal([]byte(report.ResponseSchema()), &response); err != nil {
		t.Fatal(err)
	}

	completeDefinitions, completeOK := complete["$defs"].(map[string]any)

	responseDefinitions, responseOK := response["$defs"].(map[string]any)
	if !completeOK || !responseOK {
		t.Fatal("missing schema definitions")
	}

	for name, definition := range completeDefinitions {
		if !reflect.DeepEqual(definition, responseDefinitions[name]) {
			t.Errorf("response schema drifted from complete report definition %s", name)
		}
	}
}
