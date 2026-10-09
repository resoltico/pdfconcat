// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

const (
	outputFilename   = "out.pdf"
	fixturePlainA    = "plainA"
	fixturePlainB    = "plainB"
	fixtureTitled    = "titled"
	fixtureMulti     = "multi"
	linkedPageMarker = "LINKS"
	fixtureNamed     = "named"
	fixtureActions   = "actions"
	fixtureOutline   = "outline"
	fixtureCrop      = "crop"
	fixtureUserUnit  = "unit"
	croppedPageBox   = "[10 20 310 420]"
	fixtureVersion14 = "v14"
	pdfVersion14     = "1.4"
	fixtureVersion20 = "v20"
	pdfVersion20     = "2.0"
	fixtureTagged    = "tagged"
	fixtureAttached  = "attach"
	sourceLetterBox  = "[0 0 612 792]"
	smallPageBox     = "[0 0 300 400]"
	absentFilename   = "absent.pdf"
)

func newEngine(tb testing.TB) *pdfengine.Engine {
	tb.Helper()

	engine, err := pdfengine.New()
	if err != nil {
		tb.Fatalf("create engine: %v", err)
	}

	return engine
}

// writeDoc writes a fixture into dir and returns its path.
func writeDoc(tb testing.TB, dir, name string, doc *pdffixture.Doc) string {
	tb.Helper()

	path := filepath.Join(dir, name+".pdf")
	if err := doc.WriteFile(path); err != nil {
		tb.Fatalf("write fixture %s: %v", name, err)
	}

	return path
}

// inspectDoc writes a fixture and inspects it, failing the test on error.
func inspectDoc(tb testing.TB, engine *pdfengine.Engine, name string, doc *pdffixture.Doc) (string, pdfengine.SourceInfo) {
	tb.Helper()

	path := writeDoc(tb, tb.TempDir(), name, doc)

	info, err := engine.Inspect(context.Background(), path, nil)
	if err != nil {
		tb.Fatalf("inspect %s: %v", name, err)
	}

	return path, info
}

// encrypt re-writes the PDF at path with qpdf, using AES-256 with the given user password.
func encrypt(tb testing.TB, tools pdforacle.Tools, path, userPassword string) {
	tb.Helper()

	if err := tools.Encrypt(path, userPassword); err != nil {
		tb.Fatal(err)
	}
}

// requireCode fails unless err carries the engine failure code.
func requireCode(tb testing.TB, err error, want pdfengine.Code) {
	tb.Helper()

	if err == nil {
		tb.Fatalf("got no error, want code %s", want)
	}

	engineErr, found := errors.AsType[*pdfengine.Error](err)
	if !found {
		tb.Fatalf("%v is not an *Error", err)
	}

	if engineErr.Code != want {
		tb.Fatalf("code %q (%v), want %q", engineErr.Code, err, want)
	}
}

// requireFailure is requireCode that also returns the engine failure for further checks.
func requireFailure(tb testing.TB, err error, want pdfengine.Code) *pdfengine.Error {
	tb.Helper()

	requireCode(tb, err, want)

	engineErr, _ := errors.AsType[*pdfengine.Error](err)

	return engineErr
}

func isCanceled(err error) bool { return errors.Is(err, context.Canceled) }

// canceledContext returns a context that is already canceled: an assembly that gets past request
// validation stops at its first context check instead of doing the work.
func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	return ctx
}
