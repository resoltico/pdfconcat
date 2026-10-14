// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestFuzzDiscoveryKeepsPackageAttributionAndExecutesEachTarget(t *testing.T) {
	t.Parallel()
	dir := fuzzLifecycleFixture(t)
	for name, source := range map[string]string{
		"first": `package first
import("fmt";"os";"testing")
func TestMain(m *testing.M) {code:=m.Run();fmt.Println("ok fuzz.fixture/second");fmt.Println("FuzzSame");os.Exit(code)}
func FuzzSame(f *testing.F) {f.Add(1);f.Fuzz(func(t *testing.T,n int){if err:=os.WriteFile("executed",[]byte("first"),0600);err!=nil{t.Fatal(err)}})}
`,
		"second": `package second
import("os";"testing")
func FuzzSame(f *testing.F) {f.Add(1);f.Fuzz(func(t *testing.T,n int){if err:=os.WriteFile("executed",[]byte("second"),0600);err!=nil{t.Fatal(err)}})}
`,
	} {
		packageDir := filepath.Join(dir, name)
		if err := os.Mkdir(packageDir, dirMode); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(packageDir, "fuzz_test.go"), []byte(source), fileMode); err != nil {
			t.Fatal(err)
		}
	}
	packages := []string{fuzzFixturePackage + "/first", fuzzFixturePackage + "/second"}
	targets, err := discoverFuzzTargets(t.Context(), dir, packages, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != len(packages) {
		t.Fatalf("discovered %v, want exactly one target per fixture package", targets)
	}
	for _, pkg := range packages {
		if !slices.Contains(targets, fuzzTarget{pkg: pkg, name: "FuzzSame"}) {
			t.Fatalf("discovered %v, missing target in %s", targets, pkg)
		}
	}
	for _, target := range targets {
		if err = runFuzzTarget(t.Context(), dir, target, "1x", nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"first", "second"} {
		receipt, readErr := readInRoot(dir, filepath.Join(name, "executed"))
		if readErr != nil || string(receipt) != name {
			t.Fatalf("%s callback receipt: %q, %v", name, receipt, readErr)
		}
	}
}

func TestFuzzDiscoveryRejectsInvalidEventEvidence(t *testing.T) {
	t.Parallel()
	for name, events := range map[string]string{
		"malformed": `{"Action":`,
		"malformed trailing": `{"Action":"start","Package":"m/p"}
{"Action":"output","Package":"m/p","Output":"FuzzA\n"}
{"Action":"pass","Package":"m/p"}
{"Action":`,
		"unattributed": `{"Action":"start","Package":"m/p"}
{"Action":"output","Output":"FuzzA\n"}
{"Action":"pass","Package":"m/p"}`,
		"incomplete": `{"Action":"start","Package":"m/p"}
{"Action":"output","Package":"m/p","Output":"FuzzA\n"}`,
		"incomplete package without targets": `{"Action":"start","Package":"m/p"}
{"Action":"output","Package":"m/p","Output":"FuzzA\n"}
{"Action":"pass","Package":"m/p"}
{"Action":"start","Package":"m/other"}`,
		"missing start": `{"Action":"output","Package":"m/p","Output":"FuzzA\n"}
{"Action":"pass","Package":"m/p"}`,
		"skipped target package": `{"Action":"start","Package":"m/p"}
{"Action":"output","Package":"m/p","Output":"FuzzA\n"}
{"Action":"skip","Package":"m/p"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if targets, err := parseFuzzDiscovery(strings.NewReader(events)); err == nil {
				t.Fatalf("invalid discovery accepted: %v", targets)
			}
		})
	}
}

func TestFuzzListedNamesStillRequireActualPassingLifecycle(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		name, body     string
		discoveryFails bool
	}{
		{"FuzzInvented", `code:=m.Run();fmt.Println("FuzzInvented");os.Exit(code)`, true},
		{"FuzzPresent", `fmt.Println("FuzzPresent");os.Exit(0)`, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			dir := fuzzLifecycleFixture(t)
			source := `package fixture
import("fmt";"os";"testing")
func TestMain(m *testing.M) {` + fixture.body + `}
func FuzzPresent(f *testing.F) {f.Add(1);f.Fuzz(func(t *testing.T,n int){})}
`
			if err := os.WriteFile(filepath.Join(dir, "fuzz_test.go"), []byte(source), fileMode); err != nil {
				t.Fatal(err)
			}
			targets, err := discoverFuzzTargets(t.Context(), dir, []string{fuzzFixturePackage}, nil)
			if (err != nil) != fixture.discoveryFails {
				t.Fatalf("discovery failure=%t: %v", fixture.discoveryFails, err)
			}
			target := fuzzTarget{pkg: fuzzFixturePackage, name: fixture.name}
			if !fixture.discoveryFails && !slices.Contains(targets, target) {
				t.Fatalf("fixture did not list %v: %v", target, targets)
			}
			if err = runFuzzTarget(t.Context(), dir, target, "1x", nil); err == nil {
				t.Fatalf("listed target %v passed without its real fuzz lifecycle", target)
			}
		})
	}
}

func TestFuzzDiscoveryCancellationStopsTestMain(t *testing.T) {
	t.Parallel()
	dir := fuzzLifecycleFixture(t)
	source := `package fixture
import("testing";"os";"strconv";"time")
func TestMain(m *testing.M) {
 if err:=os.WriteFile("discovery.pid.pending",[]byte(strconv.Itoa(os.Getpid())),0600);err!=nil {panic(err)}
 if err:=os.Rename("discovery.pid.pending","discovery.pid");err!=nil {panic(err)}
 for {
  if _,err:=os.Stat("discovery.stop");err==nil {break}
  time.Sleep(10*time.Millisecond)
 }
 os.Exit(m.Run())
}
func FuzzPresent(f *testing.F) {f.Add(1);f.Fuzz(func(t *testing.T,n int){})}
`
	if err := os.WriteFile(filepath.Join(dir, "fuzz_test.go"), []byte(source), fileMode); err != nil {
		t.Fatal(err)
	}

	// Prepare compilation before observing the real discovery subprocess.
	build := &command{dir: dir, name: goTool, fuzzLifecycle: true, args: []string{
		testVerb, "-c", "-o", filepath.Join(dir, "discovery.test"), fuzzFixturePackage,
	}}
	if output, err := build.output(t.Context()); err != nil {
		t.Fatalf("prepare discovery fixture: %v\n%s", err, output)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := discoverFuzzTargets(ctx, dir, []string{fuzzFixturePackage}, nil)
		done <- err
	}()
	pid := awaitFuzzWorker(t, dir, "discovery.pid", done)
	workerExitObserved := false
	// Failure cleanup uses this fixture's private stop file. Never reopen or
	// signal a PID after its original process may already have terminated.
	t.Cleanup(func() {
		if workerExitObserved {
			return
		}
		if err := os.WriteFile(filepath.Join(dir, "discovery.stop"), nil, fileMode); err != nil {
			t.Error(err)
		}
		requireFuzzWorkerExited(t, pid)
	})
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || errors.Is(err, errFuzzCleanupUncertain) {
			t.Errorf("discovery lost cancellation or cleanup settlement: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("fuzz discovery cancellation did not finish")
	}
	requireFuzzWorkerExited(t, pid)
	workerExitObserved = true
}
