// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"strings"
	"testing"
	"time"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/core/outcome"
	"github.com/resoltico/pdfconcat/internal/files"
)

func TestInterruptionSentencesAcrossApplicationStages(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		var ctx context.Context
		var cancel context.CancelFunc
		if deadline {
			ctx, cancel = context.WithDeadline(t.Context(), time.Unix(0, 0))
		} else {
			ctx, cancel = context.WithCancel(t.Context())
			cancel()
		}
		defer cancel()
		req := Request{Operation: Check, InspectionWorkers: 1, WorkingDir: t.TempDir(), Input: Items{BlankItem{Ref: "blank"}}}
		complete := Execute(ctx, req)
		declared, ds := packet.AdmitDeclaration(packet.Declaration{Items: []packet.Item{&packet.PageItem{Count: 1}}})
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		_, fontDS := loadFonts(ctx, declared)
		tail := outcome.New("check")
		noteCancellation(ctx, &tail)
		groups := [][]diag.Diagnostic{complete.Diagnostics, admit(ctx, req).diagnostics, fontDS, tail.Diagnostics, build(ctx, declared, &composition{layout: &packet.Layout{}}, files.Roles{}).diagnostics}
		for _, diagnostics := range groups {
			found := false
			for _, d := range diagnostics {
				if d.Code != diag.Interrupted {
					continue
				}
				found = true
				want := "The operation was interrupted: " + ctx.Err().Error() + "."
				if d.Message != want || d.Detail != ctx.Err().Error() || strings.Contains(d.Message, "input path") || strings.Contains(d.Message, "PDF processing") {
					t.Fatalf("wrong cancellation explanation: %+v", d)
				}
			}
			if !found {
				t.Fatalf("stage lost cancellation: %+v", diagnostics)
			}
		}
	}
}
