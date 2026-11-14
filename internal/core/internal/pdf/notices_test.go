// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
)

func noticeReport(t *testing.T, notices ...model.ValidationNotice) model.ValidationReport {
	t.Helper()
	ctx, err := model.NewContext(bytes.NewReader(nil), model.NewStatelessConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range notices {
		ctx.AddValidationNotice(n)
	}
	return ctx.ValidationReport()
}

func TestValidationNoticesDoNotInferContentLossFromWording(t *testing.T) {
	for _, message := range []string{
		"page 2: active content: JavaScript in annotation action",
		"page 2: content stream skipped", "completely changed upstream wording", "",
	} {
		n := model.NewValidationNotice(model.NoticePhaseValidate, model.NoticeSkipped, message, errors.New("a validation divergence"))
		n.ObjectNumber, n.PageNumber, n.Path = 5, 2, "Pages.Contents"
		ds, err := validationNotices(noticeReport(t, n))
		if err != nil || len(ds) != 1 || ds[0].Code != diag.SourceValidationNotice || ds[0].Severity != diag.Warning {
			t.Fatalf("wording selected admission policy: %+v %v", ds, err)
		}
		if ds[0].PDFLocation == nil || *ds[0].PDFLocation != (diag.PDFLocation{Object: 5, Page: 2, Path: "Pages.Contents"}) || !strings.Contains(ds[0].Detail, message) {
			t.Fatalf("notice evidence lost: %+v", ds)
		}
	}
}

func TestReportedRepairsAreRefusedByDisposition(t *testing.T) {
	for _, phase := range []model.NoticePhase{model.NoticePhaseParse, model.NoticePhaseRead, model.NoticePhaseValidate} {
		for _, message := range []string{"page 2: active content: JavaScript in annotation action", "unrelated wording", ""} {
			n := model.NewValidationNotice(phase, model.NoticeRepaired, message, nil)
			n.PageNumber = 2
			advisory := model.NewValidationNotice(model.NoticePhaseValidate, model.NoticeDigested, "another finding", nil)
			ds, err := validationNotices(noticeReport(t, advisory, n, advisory))
			if err == nil || diagnostic(err, diag.SourceInvalid).Code != diag.SourceUnsupported || len(ds) != 3 {
				t.Fatalf("repair accepted or other evidence lost: %+v %v", ds, err)
			}
		}
	}
}

func TestValidationNoticeEvidenceIsSafeToEncode(t *testing.T) {
	notice := model.NewValidationNotice(model.NoticePhaseValidate, model.NoticeSkipped, "backend\xff", nil)
	notice.Path = "entry\xff"
	ds, err := validationNotices(noticeReport(t, notice))
	if err != nil || len(ds) != 1 {
		t.Fatal(ds, err)
	}
	if _, err := json.Marshal(ds); err != nil {
		t.Fatalf("backend evidence broke result encoding: %v", err)
	}
}
