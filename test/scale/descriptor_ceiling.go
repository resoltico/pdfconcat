// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package scale

import (
	"crypto/sha256"
	"errors"
	"os"
	"syscall"
)

// DescriptorCeiling records preparation for one owned kernel-limited launch. Product execution
// and its independent oracles are separate required facts; this is not a sampled descriptor peak.
type (
	// NativeCompilerIdentity identifies the compiler and selected SDK for a test-only native tool.
	NativeCompilerIdentity struct {
		Compiler       string `json:"compiler"`
		CompilerPath   string `json:"compiler_path"`
		CompilerSHA256 string `json:"compiler_sha256"`
		SDK            string `json:"sdk,omitempty"`
	}

	// DescriptorCeiling records native cap preparation; execution and sample quality are separate facts.
	DescriptorCeiling struct {
		NativeCompilerIdentity

		SourceSHA256       string `json:"source_sha256"`
		LauncherSHA256     string `json:"launcher_sha256"`
		BinarySHA256       string `json:"binary_sha256"`
		InvocationSHA256   string `json:"invocation_sha256"`
		Token              string `json:"token"`
		Hygiene            string `json:"hygiene"`
		PID                int    `json:"pid"`
		UID                int    `json:"uid"`
		EUID               int    `json:"euid"`
		GID                int    `json:"gid"`
		EGID               int    `json:"egid"`
		Soft               uint64 `json:"soft"`
		Hard               uint64 `json:"hard"`
		Device             uint64 `json:"device"`
		Inode              uint64 `json:"inode"`
		Bytes              int64  `json:"bytes"`
		Mode               uint64 `json:"mode"`
		RaiseErrno         int    `json:"raise_errno"`
		ControlCloseOnExec bool   `json:"control_cloexec"`
		HygieneConfigured  bool   `json:"hygiene_configured"`
	}

	descriptorLaunch struct {
		reader     *os.File
		writer     *os.File
		directory  string
		binaryPath string
		expected   DescriptorCeiling
	}
)

const (
	descriptorHygieneDarwin = "same_pid_setexec_cloexec_default"
	descriptorHygieneLinux  = "close_range_and_proc_survivors"
)

var errDescriptorLaunch = errors.New("descriptor launch")

func (launch *descriptorLaunch) release() error {
	if launch == nil {
		return nil
	}

	var failures []error
	if launch.reader != nil {
		failures = append(failures, launch.reader.Close())
		launch.reader = nil
	}

	if launch.writer != nil {
		failures = append(failures, launch.writer.Close())
		launch.writer = nil
	}

	if launch.directory != "" {
		failures = append(failures, os.RemoveAll(launch.directory))
		launch.directory = ""
	}

	return errors.Join(failures...)
}

// DescriptorBoundVerified distinguishes the completed process's checked ceiling from sample quality.
// Its facts are produced by the owned native launch boundary, not by a requested RunSpec limit.
func (m *Measurement) DescriptorBoundVerified() bool {
	ceiling := m.DescriptorCeiling
	if m.ExitCode != 0 || ceiling == nil || ceiling.Soft != 64 || ceiling.Hard != 64 || m.MaxDescriptors > 64 {
		return false
	}

	return ceiling.PID > 0 && ceiling.UID > 0 && ceiling.UID == ceiling.EUID && ceiling.GID == ceiling.EGID &&
		ceiling.limitPreparationPresent() && ceiling.launchProvenancePresent()
}

func (ceiling *DescriptorCeiling) launchProvenancePresent() bool {
	if ceiling.Compiler == "" || ceiling.CompilerPath == "" || len(ceiling.Token) != 32 {
		return false
	}

	digests := []string{
		ceiling.BinarySHA256, ceiling.SourceSHA256, ceiling.LauncherSHA256,
		ceiling.CompilerSHA256, ceiling.InvocationSHA256,
	}
	for _, digest := range digests {
		if len(digest) != sha256.Size*2 {
			return false
		}
	}

	return true
}

func (ceiling *DescriptorCeiling) limitPreparationPresent() bool {
	if !ceiling.ControlCloseOnExec || !ceiling.HygieneConfigured || ceiling.RaiseErrno != int(syscall.EPERM) {
		return false
	}

	return ceiling.Hygiene == descriptorHygieneDarwin || ceiling.Hygiene == descriptorHygieneLinux
}
