// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
//go:build linux

package app_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/resoltico/pdfconcat/internal/capture"
)

const (
	linuxACLNamedUser = 65534
	linuxACLRead      = 4
)

// TestPublishedArtifactsConstrainInheritedLinuxACL proves the effective
// access policy on real Linux ACLs. A readable inherited control rejects a
// fixture whose parent did not actually grant another user access.
func TestPublishedArtifactsConstrainInheritedLinuxACL(t *testing.T) {
	dir := workDir(t)
	writePDF(t, dir, sourceA)
	for _, parent := range []string{filepath.Dir(dir), dir} {
		if err := os.Chmod(parent, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	installLinuxDefaultACL(t, dir)

	control := filepath.Join(dir, "acl-readable-control")
	file, err := os.OpenFile(control, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write([]byte("readable through inherited ACL")); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if !linuxACLGrantsRead(t, control) {
		t.Fatal("control did not inherit an effective named-user read ACL")
	}
	if os.Geteuid() == 0 && !linuxACLReadableAsOtherUser(t, control) {
		t.Fatal("named user cannot read the inherited ACL control")
	}

	staged, err := capture.CreatePrivateTemp(dir, "acl-stage-*")
	if err != nil {
		t.Fatal(err)
	}
	stagedPath := staged.Name()
	assertPrivateLinuxACL(t, stagedPath) // Before sensitive bytes are written.
	if _, err = staged.Write([]byte("private staged content")); err != nil {
		t.Fatal(err)
	}
	if err = staged.Close(); err != nil {
		t.Fatal(err)
	}
	assertPrivateLinuxACL(t, stagedPath)
	if err = os.Remove(stagedPath); err != nil {
		t.Fatal(err)
	}

	output := "acl-output.pdf"
	reportTarget := "acl-report.json"
	execute(t.Context(), t, appOf(newFake(t)), dir,
		commandBuild, outputFlag, output, reportFlag, reportTarget, sourceA).requireCode(t, 0, "")
	assertPrivateLinuxACL(t, filepath.Join(dir, output))
	assertPrivateLinuxACL(t, filepath.Join(dir, reportTarget))

	recoveryOutput := filepath.Join(dir, "acl-recovery-output.pdf")
	recoveryTarget := filepath.Join(dir, "acl-recovery-report.json")
	var recovery string
	runner := appOf(newFake(t))
	runner.AfterPDFCommit(func() {
		recovery, _ = aliasOutputWithStagedRecovery(t, dir, recoveryOutput, recoveryTarget)
	})
	execute(t.Context(), t, runner, dir,
		commandBuild, outputFlag, recoveryOutput, reportFlag, recoveryTarget, sourceA).
		requireCode(t, 1, reportPublishFailureCode)
	if recovery == "" {
		t.Fatal("postcommit report failure did not retain a recovery file")
	}
	assertPrivateLinuxACL(t, recovery)
}

// The version, entry widths and tags are the Linux POSIX ACL xattr UAPI in
// linux/posix_acl_xattr.h and linux/posix_acl.h. The named uid can read a
// normal file but the kernel must mask it out on owner-only creations.
func installLinuxDefaultACL(t *testing.T, dir string) {
	t.Helper()
	acl := make([]byte, 4+5*8)
	binary.LittleEndian.PutUint32(acl[:4], 2)
	entries := []struct {
		tag, permission uint16
		identity        uint32
	}{
		{1, 7, ^uint32(0)},
		{2, 5, linuxACLNamedUser},
		{4, 0, ^uint32(0)},
		{16, 5, ^uint32(0)},
		{32, 0, ^uint32(0)},
	}
	for index, entry := range entries {
		at := 4 + index*8
		binary.LittleEndian.PutUint16(acl[at:at+2], entry.tag)
		binary.LittleEndian.PutUint16(acl[at+2:at+4], entry.permission)
		binary.LittleEndian.PutUint32(acl[at+4:at+8], entry.identity)
	}
	if err := syscall.Setxattr(dir, "system.posix_acl_default", acl, 0); err != nil {
		t.Fatalf("install real inherited Linux ACL: %v", err)
	}
}

func linuxACLGrantsRead(t *testing.T, path string) bool {
	t.Helper()
	size, err := syscall.Getxattr(path, "system.posix_acl_access", nil)
	if errors.Is(err, syscall.ENODATA) {
		return false // Removing the inherited ACL is also private.
	}
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, size)
	if _, err := syscall.Getxattr(path, "system.posix_acl_access", data); err != nil {
		t.Fatal(err)
	}
	if len(data) < 4 || (len(data)-4)%8 != 0 || binary.LittleEndian.Uint32(data[:4]) != 2 {
		t.Fatalf("invalid native POSIX ACL on %s", path)
	}
	var namedPermission, mask uint16
	for at := 4; at < len(data); at += 8 {
		tag := binary.LittleEndian.Uint16(data[at : at+2])
		permission := binary.LittleEndian.Uint16(data[at+2 : at+4])
		identity := binary.LittleEndian.Uint32(data[at+4 : at+8])
		switch {
		case tag == 2 && identity == linuxACLNamedUser:
			namedPermission = permission
		case tag == 16:
			mask = permission
		}
	}
	return namedPermission&mask&linuxACLRead != 0
}

func assertPrivateLinuxACL(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("artifact is not an owner-only regular file: %s mode=%v", path, info.Mode())
	}
	if linuxACLGrantsRead(t, path) {
		t.Fatalf("inherited ACL grants another user read access: %s", path)
	}
	if os.Geteuid() == 0 && linuxACLReadableAsOtherUser(t, path) {
		t.Fatalf("another user read private artifact: %s", path)
	}
}

func linuxACLReadableAsOtherUser(t *testing.T, path string) bool {
	t.Helper()
	command := exec.CommandContext(t.Context(), "/bin/cat", path)
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{
		Uid: linuxACLNamedUser, Gid: linuxACLNamedUser,
	}}
	command.Stdout = io.Discard
	var stderr bytes.Buffer
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return true
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("inspect Linux ACL as unprivileged user: %v: %s", err, stderr.String())
	}
	return false
}
