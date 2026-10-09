/*
Copyright 2026 The pdfcpu Authors.
Licensed under the Apache License, Version 2.0.
*/
package model

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type resolverCheckpointContext struct{ remaining atomic.Int64 }

func (*resolverCheckpointContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*resolverCheckpointContext) Done() <-chan struct{}       { return nil }
func (*resolverCheckpointContext) Value(any) any               { return nil }
func (c *resolverCheckpointContext) Err() error {
	if c.remaining.Add(-1) < 0 {
		return context.Canceled
	}
	return nil
}

func lazyEntryResolverFixture(body string) (*XRefTable, types.Dict) {
	stream := types.NewObjectStreamDict()
	stream.Content = []byte(body)
	lazy := types.NewLazyObjectStreamObject(stream, 0, -1, func(ctx context.Context, text string) (types.Object, error) { return ParseObject(ctx, &text, 0) })
	xref := &XRefTable{Table: map[int]*XRefTableEntry{1: NewXRefTableEntryGen0(lazy)}}
	return xref, types.Dict{"K": *types.NewIndirectRef(1, 0)}
}

func TestContextTypedEntryResolversRetainActualLazyParserLifetime(t *testing.T) {
	tests := []struct {
		name, body string
		resolve    func(context.Context, *XRefTable, types.Dict) error
	}{
		{"boolean", "true", func(c context.Context, x *XRefTable, d types.Dict) error {
			_, _, err := x.DereferenceBooleanEntryContext(c, d, "K")
			return err
		}},
		{"integer", "12", func(c context.Context, x *XRefTable, d types.Dict) error {
			_, _, err := x.DereferenceIntegerEntryContext(c, d, "K")
			return err
		}},
		{"number", "12.5", func(c context.Context, x *XRefTable, d types.Dict) error {
			_, _, err := x.DereferenceNumberEntryContext(c, d, "K")
			return err
		}},
		{"name", "/Page", func(c context.Context, x *XRefTable, d types.Dict) error {
			_, _, err := x.DereferenceNameEntryContext(c, d, "K")
			return err
		}},
		{"string", "(source)", func(c context.Context, x *XRefTable, d types.Dict) error {
			_, _, err := x.DereferenceStringEntryContext(c, d, "K")
			return err
		}},
		{"page node", "<< /Type /Page >>", func(c context.Context, x *XRefTable, d types.Dict) error {
			_, err := x.DereferencePageNodeDictContext(c, d["K"].(types.IndirectRef))
			return err
		}},
		{"rectangle", "1", func(c context.Context, x *XRefTable, d types.Dict) error {
			_, err := x.RectForArrayContext(c, types.Array{d["K"], types.Integer(2), types.Integer(4), types.Integer(8)})
			return err
		}},
		{"dict entry", "<< /Type /Page >>", func(c context.Context, x *XRefTable, d types.Dict) error {
			_, err := x.DereferenceDictEntryContext(c, d, "K")
			return err
		}},
		{"string bytes", "(source)", func(c context.Context, x *XRefTable, d types.Dict) error {
			_, err := x.DereferenceStringEntryBytesContext(c, d, "K")
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			completed := false
			for budget := range int64(200) {
				xref, entry := lazyEntryResolverFixture(test.body)
				c := &resolverCheckpointContext{}
				c.remaining.Store(budget)
				err := test.resolve(c, xref, entry)
				if err == nil {
					completed = true
					break
				}
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("native entry lost cancellation budget%d: %v", budget, err)
				}
				if _, lazy := xref.Table[1].Object.(types.LazyObjectStreamObject); !lazy && test.name != "page node" && test.name != "rectangle" {
					t.Fatal("canceled lazy entry published a resolved object")
				}
				if err = test.resolve(t.Context(), xref, entry); err != nil {
					t.Fatalf("canceled native entry could not retry: %v", err)
				}
			}
			if !completed {
				t.Fatal("uncancelled native entry resolver never completed")
			}
		})
	}
}
