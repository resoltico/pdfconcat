// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestSavedBuildIdentityMatchesVerifiedBytesAndQueryKeepsCapturedState(t *testing.T) {
	t.Parallel()
	dir := workDir(t)
	writePDF(t, dir, sourceA)
	runner := appOf(newFake(t))
	execute(t.Context(), t, runner, dir, commandBuild, outputFlag, outputFile, reportFlag, reportFile, sourceA).requireCode(t, 0, "")

	data, err := os.ReadFile(filepath.Clean(filepath.Join(dir, outputFile)))
	if err != nil {
		t.Fatal(err)
	}

	digest := sha256.Sum256(data)

	saved := readCompleteIdentityReport(t, filepath.Join(dir, reportFile))
	if saved.Publication.OutputDigest != hex.EncodeToString(digest[:]) {
		t.Fatalf("saved digest does not identify actual output bytes: %+v", saved.Publication)
	}

	requireProducer(t, saved)

	if removeErr := os.Remove(filepath.Join(dir, outputFile)); removeErr != nil {
		t.Fatal(removeErr)
	}

	queried := execute(t.Context(), t, runner, dir, commandReport, reportFile)
	if queried.code != 0 {
		t.Fatalf("query revalidated changed current output: %s", queried.stdout)
	}

	var envelope struct {
		Result report.Summary `json:"result"`
	}
	if decodeErr := json.Unmarshal([]byte(queried.stdout), &envelope); decodeErr != nil {
		t.Fatal(decodeErr)
	}

	if !envelope.Result.Publication.Published || envelope.Result.Publication.OutputDigest != saved.Publication.OutputDigest {
		t.Fatalf("query lost captured publication identity: %+v", envelope.Result.Publication)
	}
}

func TestSavedCheckIdentityHasNoUnverifiedOutputDigest(t *testing.T) {
	t.Parallel()
	dir := workDir(t)
	writePDF(t, dir, sourceA)
	execute(t.Context(), t, appOf(newFake(t)), dir, commandCheck, reportFlag, reportFile, sourceA).requireCode(t, 0, "")

	saved := readCompleteIdentityReport(t, filepath.Join(dir, reportFile))
	if saved.Publication.OutputDigest != "" || saved.Publication.Published {
		t.Fatalf("check claims verified output bytes: %+v", saved.Publication)
	}

	requireProducer(t, saved)
}

func readCompleteIdentityReport(t *testing.T, path string) *report.Report {
	t.Helper()

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}

	var saved report.Report
	if decodeErr := json.Unmarshal(data, &saved); decodeErr != nil {
		t.Fatal(decodeErr)
	}

	if validationErr := saved.Validate(); validationErr != nil {
		t.Fatal(validationErr)
	}

	return &saved
}

func requireProducer(t *testing.T, saved *report.Report) {
	t.Helper()

	producer := saved.Producer
	if producer == nil {
		t.Fatal("complete report has no producing identity")
	}

	identityMatches := producer.Tool == "pdfconcat" && producer.Version == "devel" && producer.Commit == "unknown" &&
		producer.Go == runtime.Version() && producer.Platform == runtime.GOOS+"/"+runtime.GOARCH
	if !identityMatches {
		t.Fatalf("incorrect producing identity: %+v", producer)
	}
}
