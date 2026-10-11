// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/observation"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

func TestRejectedAssemblyReuseClearsReceiptAndHasNoRunOrSource(t *testing.T) {
	t.Parallel()
	tools := pdforacle.RequireTools(t)
	engine := newEngine(t)
	source, info := inspectDoc(t, engine, "receipt", pdffixture.Plain("receipt source"))
	request := pdfengine.AssembleRequest{
		Sources: []pdfengine.SourceFile{{Path: source, Info: info}},
		Order:   []pdfengine.Run{pdfengine.SourcePages(0, 1, 1)}, ExpectedPages: 1,
		Destination: filepath.Join(t.TempDir(), outputFilename),
	}
	if err := engine.Assemble(t.Context(), &request); err != nil {
		t.Fatal(err)
	}
	document, err := pdforacle.Load(tools, request.Destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := tools.Check(request.Destination); err != nil {
		t.Fatal(err)
	}
	if document.PageCount() != 1 || document.FirstLine(1) != "receipt source" {
		t.Fatal("assembled receipt fixture lost its independently checked page")
	}
	original, err := os.ReadFile(request.Destination)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(original)
	if request.OutputDigest == "" || request.OutputDigest != fmt.Sprintf("%x", digest) {
		t.Fatal("successful assembly receipt does not identify actual output bytes")
	}
	request.Order = nil
	err = engine.Assemble(t.Context(), &request)
	var refusal *pdfengine.Error
	if !errors.As(err, &refusal) || refusal.Code != pdfengine.CodeRequestInvalid || refusal.Reason != pdfengine.ReasonOrder || refusal.Run != -1 || refusal.Source != -1 {
		t.Fatalf("empty-order refusal invented run/source attribution: %v", err)
	}
	if request.OutputDigest != "" {
		t.Fatal("rejected reuse retained the earlier successful output receipt")
	}
	after, err := os.ReadFile(request.Destination)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("rejected assembly reuse changed the earlier output bytes: %v", err)
	}
}

func TestOutputWritingCancellationLeavesNoStagedArtifactOrCleanupFailure(t *testing.T) {
	t.Parallel()
	engine := newEngine(t)
	source, info := inspectDoc(t, engine, "cancel-writing", pdffixture.Plain("cancel source"))
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	trigger := &phaseCancellation{cancel: cancel, phase: observation.OutputWriting}
	request := pdfengine.AssembleRequest{
		Sources: []pdfengine.SourceFile{{Path: source, Info: info}},
		Order:   []pdfengine.Run{pdfengine.SourcePages(0, 1, 1)}, ExpectedPages: 1,
		Destination: filepath.Join(dir, outputFilename), Observer: trigger,
	}
	err := engine.Assemble(ctx, &request)
	var refusal *pdfengine.Error
	if !trigger.observed || !errors.As(err, &refusal) || refusal.Code != pdfengine.CodeCanceled || !errors.Is(err, context.Canceled) {
		t.Fatalf("output-writing cancellation lost its actual cause: observed=%t err=%v", trigger.observed, err)
	}
	if strings.Contains(err.Error(), "remove staged output") || request.OutputDigest != "" {
		t.Fatalf("canceled writing invented cleanup failure or verified receipt: digest=%q err=%v", request.OutputDigest, err)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("canceled writing retained an output artifact: entries=%v err=%v", entries, readErr)
	}
}
