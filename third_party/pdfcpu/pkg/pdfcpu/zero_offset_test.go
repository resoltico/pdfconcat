// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis
package pdfcpu

import (
	"bytes"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func TestZeroOffsetEntryPolicy(t *testing.T) {
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationStrict
	ctx, err := model.NewContext(bytes.NewReader(nil), conf)
	if err != nil {
		t.Fatal(err)
	}
	fields := []string{"0000000000", "00017", "n"}
	if err = parseXRefTableEntry(ctx.XRefTable, fields, 6, 0, 0); err == nil {
		t.Fatal("strict default admitted zero offset")
	}
	conf.ZeroOffsetPolicy = model.AdmitUnreferencedZeroOffsets
	if err = parseXRefTableEntry(ctx.XRefTable, fields, 6, 0, 2); err != nil {
		t.Fatal(err)
	}
	entry := ctx.Table[6]
	if entry == nil || !entry.Free || *entry.Generation != 17 || len(ctx.ZeroOffsetEntries) != 1 || ctx.ZeroOffsetEntries[0].Revision != 2 {
		t.Fatalf("lost tombstone: %+v %+v", entry, ctx.ZeroOffsetEntries)
	}
	for _, generation := range []string{"000000", "65536", "-0001", "00x00"} {
		if err := parseXRefTableEntry(ctx.XRefTable, []string{"0000000000", generation, "n"}, 7, 0, 0); err == nil {
			t.Fatalf("unrelated malformed generation admitted: %s", generation)
		}
	}

}
