// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFuzzStorageConfiguresRealGoAndOwnsChildTemporaryFiles(t *testing.T) {
	t.Parallel()
	storage, err := newFuzzStorage()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := storage.close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	dir := fuzzLifecycleFixture(t)
	source := `package fixture
import("os";"path/filepath";"strings";"testing")
func FuzzOwned(f *testing.F) {f.Add(1);f.Fuzz(func(t *testing.T,n int){
 root:=os.Getenv("PDFCONCAT_EXPECT_FUZZ_STORAGE")
 for _,key:=range []string{"GOTMPDIR","TMPDIR","TMP","TEMP"} {
  if !strings.HasPrefix(os.Getenv(key),root+string(os.PathSeparator)){t.Fatalf("%s escaped owned storage: %s",key,os.Getenv(key))}
 }
 file,err:=os.CreateTemp("","owned-child-");if err!=nil{t.Fatal(err)}
 if err=file.Close();err!=nil{t.Fatal(err)}
 if err=os.WriteFile("child-storage",[]byte(filepath.Dir(file.Name())),0600);err!=nil{t.Fatal(err)}
 if os.Getenv("PDFCONCAT_FUZZ_FAIL")=="1"{t.Fatal("retained corpus failure")}
})}
`
	if err = os.WriteFile(filepath.Join(dir, "fuzz_test.go"), []byte(source), fileMode); err != nil {
		t.Fatal(err)
	}
	environment := append(storage.environment(), "PDFCONCAT_EXPECT_FUZZ_STORAGE="+storage.path)
	verify := goCommand(dir, "env", "-json", "GOCACHE", "GOTMPDIR", "GOCACHEPROG")
	verify.env = append([]string{"GOCACHEPROG=unavailable-external-cache"}, environment...)
	output, err := verify.output(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var configuration map[string]string
	if err = json.Unmarshal([]byte(output), &configuration); err != nil {
		t.Fatal(err)
	}
	if configuration["GOCACHE"] != filepath.Join(storage.path, "cache") || configuration["GOTMPDIR"] != filepath.Join(storage.path, "tmp") || configuration["GOCACHEPROG"] != "" {
		t.Fatalf("real Go storage configuration: %v", configuration)
	}

	// Keep this control cheap by reusing the compiler cache; verify production's
	// private compiler configuration separately above.
	ambient := goCommand(dir, "env", "GOCACHE")
	cache, err := ambient.output(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	environment = append(environment, "GOCACHE="+strings.TrimSpace(cache))
	run := &command{dir: dir, name: goTool, fuzzLifecycle: true, env: environment, args: []string{testVerb, jsonFlag, runSelectionFlag, "^$", "-fuzz", "^FuzzOwned$", "-fuzztime", "1x", fuzzFixturePackage, "-args", "-test.fuzzcachedir=" + filepath.Join(storage.path, "cache", "fuzz")}}
	if output, err = run.output(t.Context()); err != nil {
		t.Fatalf("real owned-temp fuzz lifecycle: %v\n%s", err, output)
	}
	child, err := readInRoot(dir, "child-storage")
	if err != nil || string(child) != filepath.Join(storage.path, "tmp") {
		t.Fatalf("child temp receipt: %q, %v", child, err)
	}
	// Failure inputs belong to the source package and survive scratch cleanup.
	corpus := filepath.Join(dir, "testdata", "fuzz", "FuzzOwned")
	if err = os.MkdirAll(corpus, dirMode); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(corpus, "retained-input")
	if err = os.WriteFile(input, []byte("go test fuzz v1\nint(1)\n"), fileMode); err != nil {
		t.Fatal(err)
	}
	run.env = append(environment, "PDFCONCAT_FUZZ_FAIL=1")
	if output, err = run.output(t.Context()); err == nil || !strings.Contains(output, "retained corpus failure") {
		t.Fatalf("real failure corpus did not reject: %v\n%s", err, output)
	}
	if err = storage.close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(storage.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned scratch remained: %v", err)
	}
	if data, readErr := readInRoot(corpus, filepath.Base(input)); readErr != nil || !strings.Contains(string(data), "int(1)") {
		t.Fatalf("failure corpus removed: %q, %v", data, readErr)
	}
}

func TestFuzzStorageMakesRelativeTemporaryParentAbsolute(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("TMPDIR", "scratch")
	if err := os.Mkdir("scratch", dirMode); err != nil {
		t.Fatal(err)
	}
	storage, err := newFuzzStorage()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := storage.close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	if !filepath.IsAbs(storage.path) {
		t.Fatalf("Go cache requires an absolute owned directory: %s", storage.path)
	}
}
