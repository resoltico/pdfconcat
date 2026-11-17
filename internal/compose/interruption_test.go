// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package compose

import (
	"context"
	"testing"
	"time"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

func TestLayoutCancellationOwnsInterruptionSentence(t *testing.T) {
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
		layout := packet.Layout{Parts: []packet.Part{{Leaf: packet.Leaf{Ref: "page", Kind: packet.PartPage, Page: &packet.PageStyle{}}}}}
		_, ds := LayOut(ctx, layout, nil)
		ds = append(ds, Diagnostic(ctx.Err(), "page"))
		for _, d := range ds {
			if d.Code != diag.Interrupted || d.Message != "The operation was interrupted: "+ctx.Err().Error()+"." || d.Detail != ctx.Err().Error() {
				t.Fatalf("wrong interruption explanation: %+v", d)
			}
		}
	}
}
