// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

type mutationExecutionRecord struct {
	Started         time.Time `json:"started"`
	Finished        time.Time `json:"finished"`
	Mutation        string    `json:"mutation"`
	JudgmentStatus  string    `json:"judgment_status"`
	DiscoveryStatus string    `json:"discovery_status"`
	ProcessError    string    `json:"process_error"`
	JudgmentError   string    `json:"judgment_error"`
	ArtifactError   string    `json:"artifact_error"`
	CachePolicy     string    `json:"cache_policy"`
	StdoutSHA256    string    `json:"stdout_sha256"`
	StderrSHA256    string    `json:"stderr_sha256"`
	StdoutBytes     int64     `json:"stdout_bytes"`
	StderrBytes     int64     `json:"stderr_bytes"`
	GoStarted       bool      `json:"go_started"`
}

const mutationMetadataByteLimit = 16 * 1024 * 1024

// MutationExecutionEvidenceIssues binds each judged outcome to complete captured stdout/stderr.
// The reviewed native executor judges behavior; this check proves artifact identity and integrity.
func MutationExecutionEvidenceIssues(report *MutationReport, open func(string) (io.ReadCloser, error)) []string {
	var problems []string

	seen := map[string]bool{}

	for _, mutant := range report.Mutants {
		if !validJudgedStatus(mutant.Status) {
			continue
		}

		if seen[mutant.ExecutionReference] {
			problems = append(problems, "duplicate judged execution reference: "+mutant.ExecutionReference)

			continue
		}

		seen[mutant.ExecutionReference] = true
		if err := mutationExecutionEvidence(mutant, open); err != nil {
			problems = append(problems, fmt.Sprintf("%s:%d:%d: execution evidence: %v", mutant.File, mutant.Line, mutant.Column, err))
		}
	}

	return problems
}

func mutationExecutionEvidence(mutant Mutant, open func(string) (io.ReadCloser, error)) error {
	key := fmt.Sprintf("%s:%d:%d:%s", mutant.File, mutant.Line, mutant.Column, mutant.Type)

	digest := sha256.Sum256([]byte(key))
	if mutant.ExecutionReference != hex.EncodeToString(digest[:]) {
		return fmt.Errorf("%w: execution reference differs from physical mutation key", ErrMutation)
	}

	record, err := readMutationExecutionRecord(mutant.ExecutionReference+".result.json", open)
	if err != nil {
		return err
	}

	if matchErr := record.matches(mutant, key); matchErr != nil {
		return matchErr
	}

	stdoutErr := checkMutationCapture(mutant.ExecutionReference+".stdout.json", record.StdoutSHA256, record.StdoutBytes, open)
	stderrErr := checkMutationCapture(mutant.ExecutionReference+".stderr.txt", record.StderrSHA256, record.StderrBytes, open)

	return errors.Join(stdoutErr, stderrErr)
}

func readMutationExecutionRecord(file string, open func(string) (io.ReadCloser, error)) (*mutationExecutionRecord, error) {
	reader, err := open(file)
	if err != nil {
		return nil, fmt.Errorf("open mutation result: %w", err)
	}

	limited := &io.LimitedReader{R: reader, N: mutationMetadataByteLimit + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()

	var record mutationExecutionRecord

	decodeErr := decoder.Decode(&record)
	if decodeErr == nil {
		var trailing any

		if endErr := decoder.Decode(&trailing); !errors.Is(endErr, io.EOF) {
			decodeErr = fmt.Errorf("%w: mutation result must contain one JSON document", ErrMutation)
		}
	}

	if limited.N == 0 {
		decodeErr = errors.Join(decodeErr, fmt.Errorf("%w: mutation metadata exceeds its byte limit", ErrMutation))
	}

	if resultErr := errors.Join(decodeErr, reader.Close()); resultErr != nil {
		return nil, fmt.Errorf("read mutation result: %w", resultErr)
	}

	return &record, nil
}

func (record *mutationExecutionRecord) matches(mutant Mutant, key string) error {
	if record.Mutation != key || record.JudgmentStatus != mutant.Status || record.DiscoveryStatus != mutant.DiscoveryStatus {
		return fmt.Errorf("%w: mutation result identity or status differs from report", ErrMutation)
	}

	if !record.GoStarted || record.Started.IsZero() || record.Finished.Before(record.Started) || record.CachePolicy == "" {
		return fmt.Errorf("%w: mutation result lacks process/time/cache facts", ErrMutation)
	}

	if record.ArtifactError != "" || record.JudgmentError != "" {
		return fmt.Errorf("%w: mutation result carries an artifact or judgment error", ErrMutation)
	}

	if (record.ProcessError == "") != (mutant.Status == statusLived) {
		return fmt.Errorf("%w: mutation result process error contradicts outcome", ErrMutation)
	}

	return nil
}

func checkMutationCapture(file, expected string, size int64, open func(string) (io.ReadCloser, error)) error {
	if !validExecutionReference(expected) || size < 0 {
		return fmt.Errorf("%w: invalid captured output digest or size", ErrMutation)
	}

	reader, err := open(file)
	if err != nil {
		return fmt.Errorf("open captured output %s: %w", file, err)
	}

	digest := sha256.New()
	count, readErr := io.Copy(digest, reader)

	if captureErr := errors.Join(readErr, reader.Close()); captureErr != nil {
		return fmt.Errorf("read captured output %s: %w", file, captureErr)
	}

	if count != size || hex.EncodeToString(digest.Sum(nil)) != expected {
		return fmt.Errorf("%w: captured output %s is truncated or differs from judgment", ErrMutation, file)
	}

	return nil
}
