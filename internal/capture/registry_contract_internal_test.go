// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryInvalidArgumentsNeverChangeClaims(t *testing.T) {
	t.Parallel()
	for _, reserved := range []bool{false, true} {
		r := NewRegistry()
		dir := t.TempDir()
		if reserved {
			if err := r.ProtectOutput(filepath.Join(dir, "output.pdf")); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Add(RoleReport, filepath.Join(dir, "report.json")); err != nil {
				t.Fatal(err)
			}
		}
		before := maps.Clone(r.artifactBindings)
		claims := maps.Clone(r.byIdentity)
		for _, path := range []string{"relative.pdf", "", filepath.Join(dir, "bad\x00.pdf"), filepath.Join(dir, string([]byte{0xff}))} {
			operations := []func() error{
				func() error { _, err := r.Add(RoleReport, path); return err },
				func() error { return r.ProtectOutput(path) },
				func() error { return r.RetireReport(path) },
			}
			for _, operation := range operations {
				err := operation()
				var source *SourceError
				if !errors.Is(err, os.ErrInvalid) || !errors.As(err, &source) || source.Path != path {
					t.Fatalf("invalid argument lost typed validation: %q: %v", path, err)
				}
				if !maps.Equal(before, r.artifactBindings) || !maps.Equal(claims, r.byIdentity) {
					t.Fatal("invalid argument changed claims")
				}
			}
		}
		if err := r.RetireReport(filepath.Join(dir, "unused.json")); err != nil {
			t.Fatalf("valid no-op refused: %v", err)
		}
	}
}
