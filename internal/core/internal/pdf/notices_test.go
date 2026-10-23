// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"errors"
	"fmt"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func TestPageDamageNoticeKeepsRepairAndSkipGuards(t *testing.T) {
	active := model.NewValidationNotice(model.NoticePhaseValidate, model.NoticeSkipped, "page 2: active content: JavaScript in page additional action /O (obj#:5)", nil)
	active.PageNumber = 2
	for _, kind := range []string{"JavaScript", "Sound", "Movie", "Rendition"} {
		notice := active
		notice.Message = fmt.Sprintf("page 2: active content: %s in annotation action (obj#:5)", kind)
		if pageDamageNotice(notice) {
			t.Fatalf("retained active content refused: %+v", notice)
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*model.ValidationNotice)
	}{
		{"repaired", func(n *model.ValidationNotice) { n.Disposition = model.NoticeRepaired }},
		{"read phase", func(n *model.ValidationNotice) { n.Phase = model.NoticePhaseRead }},
		{"cause", func(n *model.ValidationNotice) { n.Cause = errors.New("content skipped") }},
		{"path", func(n *model.ValidationNotice) { n.Path = "page.Contents" }},
		{"different page", func(n *model.ValidationNotice) { n.PageNumber = 3 }},
		{"unknown active kind", func(n *model.ValidationNotice) { n.Message = "page 2: active content: unknown in annotation action" }},
		{"ordinary skip", func(n *model.ValidationNotice) { n.Message = "page 2: content stream skipped" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notice := active
			tc.change(&notice)
			if !pageDamageNotice(notice) {
				t.Fatalf("damage guard bypassed: %+v", notice)
			}
		})
	}
	for _, disposition := range []model.NoticeDisposition{model.NoticeRepaired, model.NoticeSkipped} {
		notice := model.NewValidationNotice(model.NoticePhaseValidate, disposition, "content changed", nil)
		notice.Path = "Pages.Contents"
		if !pageDamageNotice(notice) {
			t.Fatal("page path no longer protects content", notice)
		}
		notice.Path = ""
		if pageDamageNotice(notice) {
			t.Fatal("unlocated non-page notice treated as page damage", notice)
		}
	}
}
