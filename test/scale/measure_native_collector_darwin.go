// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package scale

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

var (
	//go:embed testdata/descriptor_collector.c
	descriptorCollectorSource       string
	errDescriptorCollectorReadiness = errors.New("native descriptor collector readiness failed")
)

func compileDescriptorCollector(ctx context.Context) (_ *processSampler, failure error) {
	ctx, cancel := context.WithTimeout(ctx, descriptorCompileTimeout)
	defer cancel()

	identity, err := resolveNativeCompiler(ctx)
	if err != nil {
		return nil, err
	}

	directory, err := os.MkdirTemp("", "pdfconcat-descriptor-collector-")
	if err != nil {
		return nil, fmt.Errorf("prepare descriptor collector: %w", err)
	}

	sampler := &processSampler{directory: directory}

	defer func() {
		if failure != nil {
			failure = errors.Join(failure, sampler.release())
		}
	}()

	binary, err := compileCollectorSource(ctx, identity, directory)
	if err != nil {
		return nil, err
	}

	digest, err := fileDigest(binary)
	if err != nil {
		return nil, err
	}

	hash := sha256.Sum256([]byte(descriptorCollectorSource))
	sampler.collector = binary
	sampler.identity = &DescriptorCollectorIdentity{
		NativeCompilerIdentity: identity,
		SourceSHA256:           hex.EncodeToString(hash[:]),
		BinarySHA256:           digest,
	}

	if readyErr := sampler.verifyCollectorReadiness(ctx); readyErr != nil {
		return nil, readyErr
	}

	return sampler, nil
}

// Readiness targets the harness itself before product launch; it never contributes an app sample.
func (s *processSampler) verifyCollectorReadiness(ctx context.Context) error {
	pid := os.Getpid()
	s.identity.ReadinessPID = pid
	s.identity.ReadinessStarted = time.Now()
	output, err := exectest.Command(ctx, s.collector, "-p", strconv.Itoa(pid)).CombinedOutput()
	s.identity.ReadinessFinished = time.Now()

	s.identity.ReadinessFrame = string(output)
	if err != nil {
		return fmt.Errorf("native descriptor collector readiness: %w: %s", err, output)
	}

	reply, valid := parseDescriptorCollectorReply(string(output), pid)
	if !valid || reply.nativeError != 0 {
		return fmt.Errorf("%w: %s", errDescriptorCollectorReadiness, output)
	}

	return nil
}

func compileCollectorSource(ctx context.Context, identity NativeCompilerIdentity, directory string) (string, error) {
	source := filepath.Join(directory, "descriptor_collector.c")
	if err := os.WriteFile(source, []byte(descriptorCollectorSource), descriptorSourceMode); err != nil {
		return "", fmt.Errorf("write descriptor collector: %w", err)
	}

	binary := filepath.Join(directory, "descriptor-collector")
	args := []string{
		"-DDESCRIPTOR_INITIAL_SLOTS=" + strconv.Itoa(descriptorCollectorInitialSlots),
		"-DDESCRIPTOR_MAXIMUM_SLOTS=" + strconv.Itoa(descriptorCollectorMaximumSlots),
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Werror",
		"-o",
		binary,
		source,
		"-isysroot",
		identity.SDK,
	}

	output, err := exectest.Command(ctx, identity.CompilerPath, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("compile descriptor collector: %w: %s", err, output)
	}

	return binary, nil
}
