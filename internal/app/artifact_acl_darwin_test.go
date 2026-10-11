// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
//go:build darwin

package app_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishedArtifactsExcludeInheritedDarwinACL(t *testing.T) {
	dir := workDir(t)
	writePDF(t, dir, "source.pdf")
	command := exec.CommandContext(t.Context(), "/bin/chmod", "+a", "everyone allow read,readattr,readextattr,readsecurity,file_inherit,directory_inherit", dir)
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("install inheritance fixture: %v %s", err, data)
	}
	control, err := os.CreateTemp(dir, "inheritance-control-")
	if err != nil {
		t.Fatal(err)
	}
	if err = control.Close(); err != nil {
		t.Fatal(err)
	}
	if !darwinHasACL(t, control.Name()) {
		t.Fatal("negative control did not inherit ACL")
	}
	execute(t.Context(), t, appOf(newFake(t)), dir, "build", "-o", "output.pdf", "--report", "proof.json", "source.pdf").requireCode(t, 0, "")
	for _, name := range []string{"output.pdf", "proof.json"} {
		if darwinHasACL(t, filepath.Join(dir, name)) {
			t.Errorf("published artifact retained inherited other access: %s", name)
		}
	}
}

func darwinHasACL(t *testing.T, path string) bool {
	t.Helper()
	data, err := exec.CommandContext(t.Context(), "/bin/ls", "-le", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		t.Fatal("empty ACL inspection")
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "0:") {
			return true
		}
	}
	return false
}
