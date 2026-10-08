// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdforacle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Tools are the independent QA programs: qpdf for structure, Poppler for text and geometry.
type Tools struct {
	QPDF, PDFToText, PDFInfo, PDFToPPM string
}

// RequireToolsEnv names the environment variable that turns missing QA tools from a skip into a failure.
// CI sets it to "1"; a developer without the tools only loses these checks locally.
const RequireToolsEnv = "PDFCONCAT_REQUIRE_QA_TOOLS"

var errQPDFWarnings = errors.New("qpdf --check reported a warning")

// FindTools locates the QA tools and reports the first one that is missing.
func FindTools() (Tools, error) {
	var tools Tools

	for name, target := range map[string]*string{
		"qpdf": &tools.QPDF, "pdftotext": &tools.PDFToText, "pdfinfo": &tools.PDFInfo, "pdftoppm": &tools.PDFToPPM,
	} {
		path, err := lookPath(name)
		if err != nil {
			return Tools{}, err
		}

		*target = path
	}

	return tools, nil
}

// RequireTools locates the QA tools. When one is missing it fails the test if RequireToolsEnv is "1"
// and otherwise skips it.
func RequireTools(tb testing.TB) Tools {
	tb.Helper()

	tools, err := FindTools()
	if err != nil {
		if os.Getenv(RequireToolsEnv) == "1" {
			tb.Fatalf("QA tools are required (%s=1): %v", RequireToolsEnv, err)
		}

		tb.Skipf("QA tools not found (%v); install qpdf and Poppler, or set %s=1 in CI to make this an error", err, RequireToolsEnv)
	}

	return tools
}

// lookPath finds a program on PATH, then in the usual Homebrew prefix, which GUI-launched shells omit.
func lookPath(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err == nil {
		return path, nil
	}
	// Required jobs must prove their declared PATH prerequisites, without a host-local fallback.
	if os.Getenv(RequireToolsEnv) == "1" {
		return "", fmt.Errorf("look up %s: %w", name, err)
	}

	fallback := "/opt/homebrew/bin/" + name
	if _, statErr := os.Stat(fallback); statErr == nil {
		return fallback, nil
	}

	return "", fmt.Errorf("look up %s: %w", name, err)
}

// run executes a tool and returns its standard output and standard error. The oracle is test
// infrastructure without a caller-supplied context, so the process is not cancelable.
func run(program string, args ...string) (string, string, error) {
	command := exec.CommandContext(context.Background(), program, args...)

	var out, errOut bytes.Buffer

	command.Stdout = &out
	command.Stderr = &errOut

	err := command.Run()

	return out.String(), errOut.String(), err
}

// Check runs qpdf's structural check on path. It fails when qpdf does, and when it succeeds but reports a
// warning, which for a generated fixture or an assembled output is also a defect.
func (t Tools) Check(path string) error {
	stdout, stderr, err := run(t.QPDF, "--check", path)
	diagnostics := strings.TrimSpace(stdout + stderr)

	if err != nil {
		return fmt.Errorf("qpdf --check: %w: %s", err, diagnostics)
	}

	if strings.Contains(strings.ToLower(diagnostics), "warning") {
		return fmt.Errorf("%w: %s", errQPDFWarnings, diagnostics)
	}

	return nil
}

// Encrypt rewrites the PDF at path in place with qpdf, using AES-256 with the given user password.
func (t Tools) Encrypt(path, userPassword string) error {
	encrypted := path + ".encrypted"

	_, stderr, err := run(t.QPDF, path, "--encrypt", userPassword, "owner-secret", "256", "--", encrypted)
	if err != nil {
		return fmt.Errorf("qpdf encrypt: %w: %s", err, stderr)
	}

	if err = os.Rename(encrypted, path); err != nil {
		return fmt.Errorf("replace with encrypted file: %w", err)
	}

	return nil
}

// exitCode returns the exit status of a failed command, or -1 when err is not an exit failure.
func exitCode(err error) int {
	if exitErr, isExit := errors.AsType[*exec.ExitError](err); isExit {
		return exitErr.ExitCode()
	}

	return -1
}
