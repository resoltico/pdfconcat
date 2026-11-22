// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

func TestStagedReaderUsesOwnedHandleAndFixedLength(t *testing.T) {
	target := filepath.Join(t.TempDir(), "destination")
	if err := os.WriteFile(target, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0200); err != nil {
		t.Fatal(err)
	}
	stage, err := Create(target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stage.Abort() })
	assertPermissionBits(t, stage.Path(), 0200)
	if _, err := stage.WriteString("staged"); err != nil {
		t.Fatal(err)
	}
	reader, err := stage.Reader(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Seek(1, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := stage.WriteString(" suffix"); err != nil {
		t.Fatal(err)
	}
	if got, err := io.ReadAll(reader); err != nil || string(got) != "taged" {
		t.Fatalf("bounded reader returned %q, %v", got, err)
	}
	second, err := stage.Reader(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := io.ReadAll(second); err != nil || string(got) != "staged suffix" {
		t.Fatalf("fresh reader returned %q, %v", got, err)
	}
	assertPermissionBits(t, stage.Path(), 0200)
}

func TestNoReplacePreservesLateTargetAndAbortCleansTemp(t *testing.T) {
	target := filepath.Join(t.TempDir(), "output.pdf")
	f, err := Create(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString("new"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, []byte("other writer"), 0600); err != nil {
		t.Fatal(err)
	}
	published, err := f.Commit(false)
	if !errors.Is(err, ErrDestinationExists) || !errors.Is(err, fs.ErrExist) || published || f.Published() {
		t.Fatalf("late target was replaced: published=%v %v", published, err)
	}
	if err = f.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(f.Path()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("temp remains: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "other writer" {
		t.Fatalf("target %q %v", data, err)
	}
}

func TestPublishUsesFinalObservedPermissions(t *testing.T) {
	for _, role := range []packet.DestinationRole{packet.OutputDestination, packet.ReportDestination} {
		for _, tc := range []struct {
			name           string
			initial, final fs.FileMode
		}{
			{"stable", 0640, 0640},
			{"chmod", 0644, 0600},
			{"created", 0, 0600},
			{"removed", 0600, 0},
		} {
			t.Run(fmt.Sprintf("%d/%s", role, tc.name), func(t *testing.T) {
				dir := t.TempDir()
				target := filepath.Join(dir, "destination")
				writeMode := func(mode fs.FileMode) {
					t.Helper()
					if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(target, mode); err != nil {
						t.Fatal(err)
					}
				}
				if tc.initial != 0 {
					writeMode(tc.initial)
				}
				roles := Roles{InputsKnown: true, Overwrite: true}
				if role == packet.OutputDestination {
					roles.Output = target
				} else {
					roles.Report = target
				}
				stage, err := Create(target)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = stage.Abort() })
				if tc.initial != 0 {
					assertPermissionBits(t, stage.Path(), tc.initial)
				}
				if _, err := stage.WriteString("replacement"); err != nil {
					t.Fatal(err)
				}
				if tc.final == 0 {
					if err := os.Remove(target); err != nil {
						t.Fatal(err)
					}
				} else {
					writeMode(tc.final)
				}
				published, ds, err := roles.Publish(stage, role)
				if err != nil || len(ds) != 0 || !published || !stage.Published() {
					t.Fatalf("commit %v %v %v", published, ds, err)
				}
				if data, err := os.ReadFile(target); err != nil || string(data) != "replacement" {
					t.Fatalf("target %q %v", data, err)
				}
				want := tc.final
				if want == 0 {
					probe := filepath.Join(dir, "umask-probe")
					if err := os.WriteFile(probe, nil, 0666); err != nil {
						t.Fatal(err)
					}
					info, err := os.Stat(probe)
					if err != nil {
						t.Fatal(err)
					}
					want = info.Mode().Perm()
				}
				assertPermissionBits(t, target, want)
			})
		}
	}
}

func assertPermissionBits(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != want {
		t.Fatalf("mode %o, want %o", info.Mode().Perm(), want)
	}
}

func TestPublishRefusesLateRoleConflicts(t *testing.T) {
	for _, tc := range []struct {
		name string
		role packet.DestinationRole
		code diag.Code
	}{
		{"input-hardlink", packet.OutputDestination, diag.OutputIsInput},
		{"output-hardlink", packet.ReportDestination, diag.ReportIsOutput},
		{"unknown-inputs", packet.ReportDestination, diag.ReportExists},
		{"no-replace", packet.OutputDestination, diag.OutputExists},
		{"directory", packet.ReportDestination, diag.ReportNotRegular},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target, protected := filepath.Join(dir, "dest"), filepath.Join(dir, "protected")
			if err := os.WriteFile(protected, []byte("sentinel"), 0600); err != nil {
				t.Fatal(err)
			}
			roles := Roles{Output: protected, Report: target, InputsKnown: true, Overwrite: true}
			if tc.role == packet.OutputDestination {
				roles.Output, roles.Report = target, ""
				roles.Inputs = []string{protected}
			}
			stage, err := Create(target)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = stage.Abort() })
			switch tc.name {
			case "directory":
				err = os.Mkdir(target, 0700)
			case "unknown-inputs":
				roles.InputsKnown = false
				err = os.WriteFile(target, []byte("sentinel"), 0600)
			case "no-replace":
				roles.Overwrite = false
				err = os.WriteFile(target, []byte("sentinel"), 0600)
			default:
				err = os.Link(protected, target)
			}
			if err != nil {
				t.Fatal(err)
			}
			if published, ds, err := roles.Publish(stage, tc.role); err != nil || published || len(ds) != 1 || ds[0].Code != tc.code {
				t.Fatalf("commit %v %v %v", published, ds, err)
			}
			if tc.name != "directory" {
				if data, err := os.ReadFile(target); err != nil || string(data) != "sentinel" {
					t.Fatalf("target %q %v", data, err)
				}
			}
		})
	}
}
func TestNewOutputUsesConventionalPermissionsAndAbortRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	probe := filepath.Join(dir, "umask-probe")
	if err := os.WriteFile(probe, []byte("probe"), 0666); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Stat(probe)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "output.pdf")
	f, err := Create(target)
	if err != nil {
		t.Fatal(err)
	}
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != expected.Mode().Perm() {
		t.Fatalf("temp mode %o expected umask mode %o", info.Mode().Perm(), expected.Mode().Perm())
	}
	if err = f.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(f.Path()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("temp remains: %v", err)
	}
	if _, err = os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("abort created target")
	}
}

func TestRealNoReplaceCommit(t *testing.T) {
	target := filepath.Join(t.TempDir(), "output.pdf")
	f, err := Create(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString("complete"); err != nil {
		t.Fatal(err)
	}
	published, err := f.Commit(false)
	if err != nil || !published {
		t.Fatalf("no-replace commit %v %v", published, err)
	}
	if err = f.Abort(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "complete" {
		t.Fatalf("published target %q %v", data, err)
	}
}

func TestLongDestinationNames(t *testing.T) {
	for _, extension := range []string{".pdf", ".json"} {
		for _, overwrite := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/overwrite=%t", extension, overwrite), func(t *testing.T) {
				dir := t.TempDir()
				target := filepath.Join(dir, strings.Repeat("n", 255-len(extension))+extension)
				// Establish that the destination itself is legal on this filesystem.
				if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
				if !overwrite {
					if err := os.Remove(target); err != nil {
						t.Fatal(err)
					}
				}
				f, err := Create(target)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = f.Abort() })
				if filepath.Dir(f.Path()) != dir {
					t.Fatal("stage must remain beside the destination")
				}
				if _, err = f.WriteString("complete"); err != nil {
					t.Fatal(err)
				}
				published, err := f.Commit(overwrite)
				if err != nil || !published {
					t.Fatalf("commit %v %v", published, err)
				}
				if err = f.Abort(); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(target)
				if err != nil || string(data) != "complete" {
					t.Fatalf("published target %q %v", data, err)
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 1 {
					t.Fatalf("unexpected staging entries %v %v", entries, err)
				}
			})
		}
	}
}

func TestPublicationStatePrecedesTrailingErrors(t *testing.T) {
	late := errors.New("late failure")
	for _, publicationErr := range []error{nil, late} {
		f := &Staged{}
		called := false
		published, err := f.finishCommit(true, publicationErr, func() error {
			called = true
			if !f.Published() {
				t.Fatal("visibility was not recorded before sync")
			}
			return late
		})
		if !published || !f.Published() || !errors.Is(err, late) || called != (publicationErr == nil) {
			t.Fatalf("state/error order %v %v called=%v", published, err, called)
		}
		f.recordPublication(false)
		if !f.Published() {
			t.Fatal("publication state went backwards")
		}
	}
}

func TestLinkFallbackStateAndNoClobber(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	f, err := Create(target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Abort() })
	temp := f.Path()
	if _, err := f.WriteString("complete"); err != nil {
		t.Fatal(err)
	}
	if err := f.closeFile(); err != nil {
		t.Fatal(err)
	}
	unsupported := unsupportedError()
	rename := func(string, string) error { return unsupported }
	late := errors.New("temp removal failed")
	published, err := publish(temp, target, false, rename, os.Link, func(string) error { return late })
	if !published || !errors.Is(err, late) {
		t.Fatalf("link state lost after cleanup failure: %v %v", published, err)
	}
	if data, e := os.ReadFile(target); e != nil || string(data) != "complete" {
		t.Fatalf("linked target %q %v", data, e)
	}
	f.recordPublication(published)
	if err = f.Abort(); err != nil {
		t.Fatal(err)
	}
	if data, e := os.ReadFile(target); e != nil || string(data) != "complete" {
		t.Fatalf("abort undid publication %q %v", data, e)
	}
	if _, err := os.Stat(temp); !errors.Is(err, fs.ErrNotExist) || !f.Published() {
		t.Fatalf("trailing cleanup did not preserve published state and remove the owned name: %v", err)
	}
	// The real link boundary also refuses an existing target.
	if err = os.WriteFile(temp, []byte("different"), 0600); err != nil {
		t.Fatal(err)
	}
	published, err = publish(temp, target, false, rename, os.Link, os.Remove)
	if published || !errors.Is(err, ErrDestinationExists) || !errors.Is(err, fs.ErrExist) {
		t.Fatalf("link fallback clobbered target: %v %v", published, err)
	}
	if !strings.Contains(err.Error(), "without replacing the destination") {
		t.Fatalf("fallback failure is not actionable: %v", err)
	}
}

func TestStageFollowsOwnedParentAfterMove(t *testing.T) {
	base := t.TempDir()
	parent, moved := filepath.Join(base, "parent"), filepath.Join(base, "moved")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "output.pdf")
	stage, err := Create(target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stage.Abort() })
	if _, err := stage.WriteString("staged bytes"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(parent, moved); err != nil {
		// Windows filesystems can refuse directory renames while children
		// are open. Exercise rooted cleanup in the original directory then.
		const windowsSharingViolation = syscall.Errno(32)
		if runtime.GOOS != "windows" || (!errors.Is(err, fs.ErrPermission) && !errors.Is(err, windowsSharingViolation)) {
			t.Fatal(err)
		}
		t.Logf("native Windows directory move blocked while stage was open: %v", err)
		if abortErr := stage.Abort(); abortErr != nil {
			t.Fatal(abortErr)
		}
		if _, err := os.Stat(stage.Path()); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("stage stayed in the original directory: %v", err)
		}
		return
	}
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	const sentinel = "unrelated replacement directory file"
	for _, name := range []string{target, stage.Path()} {
		if err := os.WriteFile(name, []byte(sentinel), 0600); err != nil {
			t.Fatal(err)
		}
	}
	roles := Roles{Output: target, Overwrite: true, InputsKnown: true}
	if published, ds, err := roles.Publish(stage, packet.OutputDestination); published || len(ds) != 0 || !errors.Is(err, ErrStageParentChanged) {
		t.Fatalf("publication into a replacement directory: %v %v %v", published, ds, err)
	}
	if published, err := stage.Commit(true); published || !errors.Is(err, ErrStageParentChanged) {
		t.Fatalf("direct commit into a replacement directory: %v %v", published, err)
	}
	for range 2 {
		if err := stage.Abort(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(moved, filepath.Base(stage.Path()))); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("staged file stayed with the moved directory: %v", err)
	}
	for _, name := range []string{target, stage.Path()} {
		got, err := os.ReadFile(name)
		if err != nil || string(got) != sentinel {
			t.Fatalf("replacement directory file changed: %q %v", got, err)
		}
	}
}

func TestStageRenameReleasesTemporaryName(t *testing.T) {
	target := filepath.Join(t.TempDir(), "output.pdf")
	stage, err := Create(target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stage.Abort() })
	if _, err := stage.WriteString("published"); err != nil {
		t.Fatal(err)
	}
	if published, err := stage.Commit(true); err != nil || !published {
		t.Fatalf("native rename: %v %v", published, err)
	}
	const sentinel = "subsequent file at the former stage path"
	if err := os.WriteFile(stage.Path(), []byte(sentinel), 0600); err != nil {
		t.Fatal(err)
	}
	if err := stage.Abort(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(stage.Path())
	if err != nil || string(got) != sentinel || !stage.Published() {
		t.Fatalf("renamed stage retained ownership: %q %v", got, err)
	}
}
