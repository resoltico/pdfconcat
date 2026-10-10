// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"context"
	"errors"
	"testing"
)

type validationContext struct {
	context.Context
	visits, stop int
}

func (c *validationContext) Err() error {
	c.visits++
	if c.visits >= c.stop {
		return context.Canceled
	}
	return nil
}
func TestSemanticValidationStopsOwnedSourceTraversal(t *testing.T) {
	builder := NewBuilder("check")
	builder.AddDiagnostic(0, Diagnostic{Stage: StageRead, Code: CodeReadFailed, Message: "synthetic failure"})
	saved := builder.Build(StatusFailed)
	saved.Sources = make([]Source, 10000)
	for index := range saved.Sources {
		saved.Sources[index].Path = "/source.pdf"
	}
	ctx := &validationContext{Context: t.Context(), stop: 50}
	err := saved.ValidateContext(ctx)
	if !errors.Is(err, context.Canceled) || ctx.visits > 52 {
		t.Fatalf("unbounded validation: calls=%d error=%v", ctx.visits, err)
	}
	if err := saved.Validate(); err != nil {
		t.Fatalf("noncancelled partial evidence invalid: %v", err)
	}
}
