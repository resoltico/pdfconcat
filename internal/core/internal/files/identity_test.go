// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package files

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
)

// The injected failure is confined to the native query. Real Stat/Lstat calls
// must succeed for existing objects, so an inspection failure cannot mask a
// swallowed identity-query error.
func TestIdentityQueryErrorsRefuseReport(t *testing.T) {
	for _, stage := range []string{"first existing object", "second existing object", "missing input anchor", "report parent"} {
		for _, operation := range []string{"CreateFile", "GetFileInformationByHandleEx(FileIdInfo)", "CloseHandle"} {
			t.Run(stage+"/"+operation, func(t *testing.T) {
				inputDir, reportDir := t.TempDir(), t.TempDir()
				input := filepath.Join(inputDir, "source.pdf")
				report := filepath.Join(reportDir, "report.json")
				inputBytes, reportBytes := []byte("preserve input"), []byte("preserve report")
				missing := stage == "missing input anchor" || stage == "report parent"
				if !missing {
					if err := os.WriteFile(input, inputBytes, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(report, reportBytes, 0600); err != nil {
					t.Fatal(err)
				}
				failurePath := report
				switch stage {
				case "second existing object":
					failurePath = input
				case "missing input anchor":
					failurePath = inputDir
				case "report parent":
					failurePath = reportDir
				}
				unavailable := errors.New("native identity unavailable")
				var calls []string
				query := func(path string) (objectID, error) {
					calls = append(calls, path)
					if path == failurePath {
						return objectID{}, identityError(operation, path, unavailable)
					}
					return identity(path)
				}
				roles := Files{Report: report, Inputs: []string{input}, InputsKnown: true, Overwrite: true}
				ds := roles.checkReport(true, query)
				status, exit := diag.Outcome(ds)
				if len(ds) != 1 || ds[0].Code != diag.ReportInspectionFailed || !roleErrors(ds) || status != "failed" || exit != 1 {
					t.Fatalf("identity failure authorized publication or became a collision/separation: %+v", ds)
				}
				if ds[0].File != report || !strings.Contains(ds[0].Message, operation) || !strings.Contains(ds[0].Message, failurePath) {
					t.Fatalf("queried path/operation lost: %+v", ds[0])
				}
				wantCalls := 1
				if stage == "second existing object" || stage == "report parent" {
					wantCalls = 2
				}
				if len(calls) != wantCalls || calls[len(calls)-1] != failurePath {
					t.Fatalf("query boundary not reached as intended: %v", calls)
				}
				after, err := os.ReadFile(report)
				if err != nil || string(after) != string(reportBytes) {
					t.Fatalf("existing report changed: %q %v", after, err)
				}
				if !missing {
					after, err = os.ReadFile(input)
					if err != nil || string(after) != string(inputBytes) {
						t.Fatalf("input changed: %q %v", after, err)
					}
				} else if _, err = os.Stat(input); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("missing input created: %v", err)
				}
				entries, err := os.ReadDir(reportDir)
				if err != nil || len(entries) != 1 {
					t.Fatalf("unexpected report/staging publication: %v %v", entries, err)
				}
				// Raw relation errors must retain errors.Is/As and never authorize either
				// a collision or separation, even when a failed query returns a zero ID.
				var rawErr error
				switch stage {
				case "first existing object", "second existing object":
					same, err := samePathOrFile(report, input, query)
					if same {
						t.Fatal("failed native query asserted identity")
					}
					rawErr = err
				case "missing input anchor":
					reserved, err := reserve(input, query)
					if reserved.object != (objectID{}) || len(reserved.missing) != 0 {
						t.Fatal("failed query returned a usable reservation")
					}
					rawErr = err
				case "report parent":
					reserved, err := reserve(input, identity)
					if err != nil {
						t.Fatal(err)
					}
					collision, unproved, err := missingInputRelation(report, reserved, query)
					if collision || unproved {
						t.Fatal("failed query asserted a location relation")
					}
					rawErr = err
				}
				var located *os.PathError
				if !errors.Is(rawErr, unavailable) || !errors.As(rawErr, &located) || located.Path != failurePath || located.Op != operation {
					t.Fatalf("native error identity/context lost: %v", rawErr)
				}
			})
		}
	}
}

func TestSecondQueryErrorCannotAssertEqualZeroIdentities(t *testing.T) {
	a, b := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	for _, path := range []string{a, b} {
		if err := os.WriteFile(path, []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	unavailable := errors.New("native identity query failed")
	query := func(path string) (objectID, error) {
		if path == b {
			return objectID{}, unavailable
		}
		return objectID{}, nil
	}
	same, err := samePathOrFile(a, b, query)
	if same || !errors.Is(err, unavailable) {
		t.Fatalf("equal zero IDs masked query failure: %t %v", same, err)
	}
}
