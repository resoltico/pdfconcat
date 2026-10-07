// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build linux

package app_test

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

const (
	envDiskFullDirectory    = "PDFCONCAT_ENOSPC_DIR"
	tmpfsMagic              = 0x01021994
	maxDiskFullFixtureBytes = 4 << 20
	diskFullFillChunk       = 4096
)

var errDiskFullFilesystem = errors.New("ENOSPC fixture must be an isolated <=4MiB tmpfs; refusing to fill any other filesystem")

func TestRecoveryRefreshDiskFullKeepsOriginalLayoutAndPublishedPDF(t *testing.T) {
	t.Parallel()
	reports, root := diskFullFixture(t)
	dir := workDir(t)
	writePDF(t, dir, sourceA)

	target := filepath.Join(reports, reportFile)

	fill, createErr := root.Create("fill")
	if createErr != nil {
		t.Fatal(createErr)
	}

	t.Cleanup(func() {
		if closeErr := fill.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	runner := appOf(newFake(t))
	runner.AfterPDFCommit(func() {
		if mkdirErr := root.Mkdir(reportFile, 0o700); mkdirErr != nil {
			t.Fatal(mkdirErr)
		}

		fillUntilENOSPC(t, fill)
	})
	res := execute(t.Context(), t, runner, dir, commandBuild, outputFlag, outputFile, reportFlag, target, sourceA)
	parsed := res.requireCode(t, 1, reportPublishFailureCode)
	assertDiskFullRecovery(t, res, &parsed, dir, reports)
}

func diskFullFixture(t *testing.T) (string, *os.Root) {
	t.Helper()

	parent := os.Getenv(envDiskFullDirectory)
	if parent == "" {
		t.Skip("real ENOSPC requires isolated <=4MiB Linux tmpfs at PDFCONCAT_ENOSPC_DIR; authoritative run must require this exact test")
	}

	if err := validateDiskFullFilesystem(parent); err != nil {
		t.Fatal(err)
	}

	root, openErr := os.OpenRoot(parent)
	if openErr != nil {
		t.Fatal(openErr)
	}

	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	name := "pdfconcat-enospc-" + rand.Text()
	if err := root.Mkdir(name, 0o700); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if removeErr := root.RemoveAll(name); removeErr != nil {
			t.Error(removeErr)
		}
	})

	reports, openReportsErr := root.OpenRoot(name)
	if openReportsErr != nil {
		t.Fatal(openReportsErr)
	}

	t.Cleanup(func() {
		if closeErr := reports.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	return filepath.Join(root.Name(), name), reports
}

func validateDiskFullFilesystem(parent string) error {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(parent, &fs); err != nil {
		return fmt.Errorf("inspect ENOSPC filesystem: %w", err)
	}

	if fs.Bsize <= 0 {
		return fmt.Errorf("%w: invalid block size %d", errDiskFullFilesystem, fs.Bsize)
	}

	if fs.Type != tmpfsMagic || fs.Blocks > maxDiskFullFixtureBytes/uint64(fs.Bsize) {
		return errDiskFullFilesystem
	}

	return nil
}

func assertDiskFullRecovery(t *testing.T, res outcome, parsed *summaryView, dir, reports string) {
	t.Helper()

	var summary report.Summary
	decodeRecoverySummary(t, res.stdout, &summary)

	if summary.Publication.RecoveryState != report.RecoveryPending {
		t.Fatalf("disk-full recovery metadata: %+v", summary.Publication)
	}

	saved := readCompleteIdentityReport(t, parsed.Publication.RecoveryReport)
	if saved.Status != report.StatusOK || saved.Publication.ReportStatus != report.ReportWritten {
		t.Fatal("ENOSPC destroyed original complete planned layout")
	}

	assertArtifactTypes(t, filepath.Join(dir, outputFile), parsed.Publication.RecoveryReport)

	files, globErr := filepath.Glob(filepath.Join(reports, recoveryFilePattern))
	if globErr != nil || len(files) != 1 {
		t.Fatalf("ENOSPC refresh scratch leaked: %v %v", files, globErr)
	}
}

func fillUntilENOSPC(t *testing.T, file *os.File) {
	t.Helper()

	buffer := make([]byte, diskFullFillChunk)
	for written := 0; written <= maxDiskFullFixtureBytes; written += len(buffer) {
		_, err := file.Write(buffer)
		if errors.Is(err, syscall.ENOSPC) {
			t.Log("real filesystem write returned ENOSPC after PDF commit")
			return
		}

		if err != nil {
			t.Fatalf("expected real ENOSPC, got %v", err)
		}
	}

	t.Fatal("bounded tmpfs fixture did not reach ENOSPC")
}
