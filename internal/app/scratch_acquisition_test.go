// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestScratchFailureDoesNotBlameTheSource(t *testing.T) {
	dir := t.TempDir()
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	t.Setenv("TMP", scratch)
	path := filepath.Join(dir, "source.pdf")
	doc := pdffixture.EncodedPages("scratch", []pdffixture.EncodedStream{{Data: bytes.Repeat([]byte{' '}, sourceMemoryThreshold)}})
	if err := doc.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	request := Request{Operation: Build, Input: Items{SourceItem{Ref: "source", Path: path}}, WorkingDir: dir,
		Output: filepath.Join(dir, "packet.pdf"), InspectionWorkers: 1}
	admitted := admit(ctx, request)
	if hasErrors(admitted.diagnostics) {
		t.Fatal(admitted.diagnostics)
	}
	inspected := inspect(ctx, *admitted.plan, 1)
	if hasErrors(inspected.diagnostics) {
		t.Fatal(inspected.diagnostics)
	}
	composed := composePacket(ctx, *admitted.plan, inspected, Build)
	if hasErrors(composed.diagnostics) {
		t.Fatal(composed.diagnostics)
	}

	missing := filepath.Join(dir, "absent")
	t.Setenv("TMPDIR", missing)
	t.Setenv("TMP", missing)
	request.Operation = Check
	check := Execute(ctx, request)
	built := build(ctx, *admitted.plan, composed, admitted.roles)
	t.Run("scratch failure", func(t *testing.T) {
		if check.Status != diag.StatusFailed || built.output.Published {
			t.Errorf("unexpected scratch outcomes: %v, %+v", check.Status, built)
		}
		for _, ds := range [][]diag.Diagnostic{check.Diagnostics, built.diagnostics} {
			if len(ds) != 1 || ds[0].Code != diag.SourceUnreadable || ds[0].File != path ||
				!strings.Contains(ds[0].Message, "temporary directory") ||
				!strings.Contains(ds[0].Detail, "create private source snapshot") ||
				!strings.Contains(ds[0].Detail, missing) {
				t.Errorf("scratch provenance lost: %+v", ds)
			}
		}
	})

	request.Input = Items{SourceItem{Ref: "missing", Path: filepath.Join(dir, "missing.pdf")}}
	unreadable := Execute(ctx, request)
	if unreadable.Status != diag.StatusInvalid || len(unreadable.Diagnostics) != 1 || unreadable.Diagnostics[0].Code != diag.SourceInputInvalid {
		t.Errorf("actual missing source lost its classification: %+v", unreadable)
	}

	t.Setenv("TMPDIR", scratch)
	t.Setenv("TMP", scratch)
	if err := pdffixture.Plain("changed digest").WriteFile(path); err != nil {
		t.Fatal(err)
	}
	changed := build(ctx, *admitted.plan, composed, admitted.roles)
	if changed.output.Published || len(changed.diagnostics) != 1 || changed.diagnostics[0].Code != diag.SourceChanged {
		t.Errorf("changed source lost its classification: %+v", changed)
	}
}
