// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build unix

package scale

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

const (
	descriptorCeiling           = 64
	descriptorTokenBytes        = 16
	descriptorPreparationBytes  = 4096
	descriptorCompileTimeout    = 10 * time.Second
	descriptorObserveTimeout    = 5 * time.Second
	descriptorSourceMode        = 0o600
	darwinPlatform              = "darwin"
	prepareDescriptorError      = "prepare descriptor launch: %w"
	descriptorIdentityError     = "descriptor launch identity: %w"
	descriptorPrerequisiteError = "descriptor launch prerequisite: %w"
	descriptorPreparationError  = "read descriptor preparation: %w"
)

//go:embed testdata/descriptor_launcher.c
var descriptorLauncherSource string

func prepareDescriptorLaunch(ctx context.Context, command *exec.Cmd, ceiling uint64) (_ *descriptorLaunch, failure error) {
	if ceiling == 0 {
		return &descriptorLaunch{}, nil
	}

	if ceiling != descriptorCeiling || (runtime.GOOS != darwinPlatform && runtime.GOOS != "linux") {
		return nil, fmt.Errorf("%w: verified descriptor launch requires a 64-descriptor ceiling on Darwin or Linux", errDescriptorLaunch)
	}

	if err := errors.Join(checkLoaderEnvironment(command.Env), checkLoaderEnvironment(nil)); err != nil {
		return nil, fmt.Errorf(prepareDescriptorError, err)
	}

	launch := &descriptorLaunch{}

	defer func() {
		if failure != nil {
			failure = errors.Join(failure, launch.release())
		}
	}()

	if err := launch.identify(command); err != nil {
		return nil, fmt.Errorf(prepareDescriptorError, err)
	}

	launcher, err := launch.compile(ctx)
	if err != nil {
		return nil, fmt.Errorf(prepareDescriptorError, err)
	}

	launch.reader, launch.writer, err = os.Pipe()
	if err != nil {
		return nil, fmt.Errorf(prepareDescriptorError, err)
	}

	command.ExtraFiles = append([]*os.File{launch.writer}, command.ExtraFiles...)
	command.Args = append([]string{launcher, launch.expected.Token, "64", launch.binaryPath}, command.Args[1:]...)
	command.Path = launcher

	return launch, nil
}

func (launch *descriptorLaunch) identify(command *exec.Cmd) error {
	binary, err := filepath.Abs(command.Path)
	if err != nil {
		return fmt.Errorf(descriptorIdentityError, err)
	}

	launch.binaryPath = binary

	info, err := os.Stat(binary)
	if err != nil {
		return fmt.Errorf(descriptorIdentityError, err)
	}

	stat, valid := info.Sys().(*syscall.Stat_t)
	if !valid || !info.Mode().IsRegular() || info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return fmt.Errorf("%w: descriptor launch requires a frozen non-setid regular executable", errDescriptorLaunch)
	}

	digest, err := fileDigest(binary)
	if err != nil {
		return fmt.Errorf(descriptorIdentityError, err)
	}

	launch.expected = DescriptorCeiling{
		BinarySHA256: digest, Device: descriptorArtifactDevice(stat), Inode: stat.Ino, Bytes: info.Size(), Mode: uint64(stat.Mode),
		UID: os.Getuid(), EUID: os.Geteuid(), GID: os.Getgid(), EGID: os.Getegid(), Soft: descriptorCeiling, Hard: descriptorCeiling,
	}

	token := make([]byte, descriptorTokenBytes)
	if _, err = rand.Read(token); err != nil {
		return fmt.Errorf(descriptorIdentityError, err)
	}

	launch.expected.Token = hex.EncodeToString(token)
	hash := sha256.Sum256([]byte(strings.Join(command.Args, "\x00")))
	launch.expected.InvocationSHA256 = hex.EncodeToString(hash[:])

	return nil
}

func checkLoaderEnvironment(environment []string) error {
	if environment == nil {
		environment = os.Environ()
	}

	effective := make(map[string]string)

	for _, entry := range environment {
		key, value, _ := strings.Cut(entry, "=")
		effective[key] = value
	}

	for key, value := range effective {
		if value != "" && (strings.HasPrefix(key, "LD_") || strings.HasPrefix(key, "DYLD_")) {
			return fmt.Errorf("%w: loader-interposition environment variable %s refused", errDescriptorLaunch, key)
		}
	}

	return nil
}

func fileDigest(path string) (_ string, failure error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("open digest root: %w", err)
	}
	defer func() { failure = errors.Join(failure, root.Close()) }()

	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return "", fmt.Errorf(descriptorPrerequisiteError, err)
	}

	hash := sha256.New()

	_, copyErr := io.Copy(hash, file)
	if err = errors.Join(copyErr, file.Close()); err != nil {
		return "", fmt.Errorf(descriptorPrerequisiteError, err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (launch *descriptorLaunch) compile(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, descriptorCompileTimeout)
	defer cancel()

	identity, err := resolveNativeCompiler(ctx)
	if err != nil {
		return "", fmt.Errorf(descriptorPrerequisiteError, err)
	}

	launch.expected.NativeCompilerIdentity = identity
	compiler := identity.CompilerPath

	launch.directory, err = os.MkdirTemp("", "pdfconcat-descriptor-launch-")
	if err != nil {
		return "", fmt.Errorf(descriptorPrerequisiteError, err)
	}

	source := filepath.Join(launch.directory, "descriptor_launcher.c")
	if err = os.WriteFile(source, []byte(descriptorLauncherSource), descriptorSourceMode); err != nil {
		return "", fmt.Errorf(descriptorPrerequisiteError, err)
	}

	hash := sha256.Sum256([]byte(descriptorLauncherSource))
	launch.expected.SourceSHA256 = hex.EncodeToString(hash[:])
	binary := filepath.Join(launch.directory, "descriptor-launcher")

	args := []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-o", binary, source}
	if launch.expected.SDK != "" {
		args = append(args, "-isysroot", launch.expected.SDK)
	}

	output, err := exectest.Command(ctx, compiler, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("compile native descriptor launcher: %w: %s", err, output)
	}

	launch.expected.LauncherSHA256, err = fileDigest(binary)

	return binary, err
}

func (launch *descriptorLaunch) observe(ctx context.Context, pid int) (DescriptorCeiling, error) {
	if launch.expected.Soft == 0 {
		return DescriptorCeiling{}, nil
	}

	raw, err := launch.readPreparation(ctx)
	if err != nil {
		return DescriptorCeiling{}, err
	}

	if len(raw) > descriptorPreparationBytes || !strings.HasSuffix(string(raw), "\n") {
		return DescriptorCeiling{}, fmt.Errorf("%w: missing or oversized descriptor preparation", errDescriptorLaunch)
	}

	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()

	var actual DescriptorCeiling
	if decodeErr := decoder.Decode(&actual); decodeErr != nil {
		return DescriptorCeiling{}, fmt.Errorf(descriptorPreparationError, decodeErr)
	}

	var extra any
	if extraErr := decoder.Decode(&extra); !errors.Is(extraErr, io.EOF) {
		return DescriptorCeiling{}, fmt.Errorf("%w: trailing descriptor preparation", errDescriptorLaunch)
	}

	if validateErr := launch.validate(&actual, pid); validateErr != nil {
		return DescriptorCeiling{}, fmt.Errorf(descriptorPreparationError, validateErr)
	}

	actual.Compiler = launch.expected.Compiler
	actual.CompilerPath = launch.expected.CompilerPath
	actual.CompilerSHA256 = launch.expected.CompilerSHA256
	actual.SDK = launch.expected.SDK
	actual.SourceSHA256 = launch.expected.SourceSHA256
	actual.LauncherSHA256 = launch.expected.LauncherSHA256
	actual.BinarySHA256 = launch.expected.BinarySHA256

	actual.InvocationSHA256 = launch.expected.InvocationSHA256
	if runtime.GOOS == darwinPlatform {
		actual.Hygiene = descriptorHygieneDarwin
	} else {
		actual.Hygiene = descriptorHygieneLinux
	}

	return actual, nil
}

func (launch *descriptorLaunch) readPreparation(ctx context.Context) ([]byte, error) {
	closeErr := launch.writer.Close()
	launch.writer = nil

	if closeErr != nil {
		return nil, fmt.Errorf("close parent control writer: %w", closeErr)
	}

	if err := launch.reader.SetReadDeadline(time.Now().Add(descriptorObserveTimeout)); err != nil {
		return nil, fmt.Errorf(descriptorPreparationError, err)
	}

	reader := launch.reader
	closed := make(chan error, 1)
	stop := context.AfterFunc(ctx, func() { closed <- reader.Close() })
	raw, err := io.ReadAll(io.LimitReader(reader, descriptorPreparationBytes+1))

	stopped := stop()
	if !stopped {
		err = errors.Join(err, <-closed, ctx.Err())
	}

	closeErr = reader.Close()
	if !stopped && errors.Is(closeErr, os.ErrClosed) {
		closeErr = nil
	}

	launch.reader = nil

	if err = errors.Join(err, closeErr, ctx.Err()); err != nil {
		return nil, fmt.Errorf(descriptorPreparationError, err)
	}

	return raw, nil
}

func (launch *descriptorLaunch) validate(actual *DescriptorCeiling, pid int) error {
	expected := &launch.expected
	ids := [5]int{actual.PID, actual.UID, actual.EUID, actual.GID, actual.EGID}

	owned := [5]int{pid, expected.UID, expected.EUID, expected.GID, expected.EGID}
	if ids != owned || !unprivilegedCeilingIdentity(actual) {
		return fmt.Errorf("%w: preparation contradicts owned process privilege identity", errDescriptorLaunch)
	}

	if actual.Token != expected.Token || actual.Soft != descriptorCeiling || actual.Hard != descriptorCeiling ||
		actual.RaiseErrno != int(syscall.EPERM) || !actual.ControlCloseOnExec || !actual.HygieneConfigured {
		return fmt.Errorf("%w: preparation contradicts token, limit or configured hygiene", errDescriptorLaunch)
	}

	identity := [3]uint64{actual.Device, actual.Inode, actual.Mode}

	frozen := [3]uint64{expected.Device, expected.Inode, expected.Mode}
	if identity != frozen || actual.Bytes != expected.Bytes {
		return fmt.Errorf("%w: preparation contradicts frozen executable identity", errDescriptorLaunch)
	}

	return nil
}

func (launch *descriptorLaunch) unchangedBinary() error {
	if launch.expected.Soft == 0 {
		return nil
	}

	digest, err := fileDigest(launch.binaryPath)
	if err != nil {
		return fmt.Errorf("recheck frozen executable: %w", err)
	}

	if digest != launch.expected.BinarySHA256 {
		return fmt.Errorf("%w: frozen executable changed during launch", errDescriptorLaunch)
	}

	return nil
}

func unprivilegedCeilingIdentity(actual *DescriptorCeiling) bool {
	return actual.UID > 0 && actual.UID == actual.EUID && actual.GID == actual.EGID
}
