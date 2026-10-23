// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestCatalogVersionSynchronizesEffectiveValidationVersion(t *testing.T) {
	for _, version := range []model.Version{model.V17, model.V20} {
		conf, err := configuration()
		if err != nil {
			t.Fatal(err)
		}
		document, err := api.ReadContext(t.Context(), bytes.NewReader(pdffixture.Plain("version").Bytes()), conf)
		if err != nil {
			t.Fatal(err)
		}
		if err = cleanCatalog(document, version, false); err != nil {
			t.Fatal(err)
		}
		if document.XRefTable.Version() != version || document.HeaderVersion == nil || *document.HeaderVersion != version || document.RootVersion == nil || *document.RootVersion != version {
			t.Fatal("catalog and effective-version caches disagree")
		}
		report, err := api.ValidateContextWithReport(t.Context(), document)
		if err != nil {
			t.Fatal(err)
		}
		requiredMetricsNotice := false
		for _, notice := range report.Notices() {
			if strings.Contains(notice.Message, "missing required entries") && strings.Contains(notice.Message, "Type1 font") {
				requiredMetricsNotice = true
			}
		}
		if requiredMetricsNotice != (version == model.V20) {
			t.Fatalf("effective version did not select its real Type1 validation rule: version=%s notices=%+v", version, report.Notices())
		}
	}
}
