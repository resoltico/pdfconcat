// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const fuzzLifecycleSource = `package fixture
import "testing"
func FuzzPresent(f *testing.F) { f.Add(1); f.Fuzz(func(t *testing.T, n int) {}) }
func FuzzSkipped(f *testing.F) { f.Skip("fixture unavailable") }
`

func TestFuzzTargetRequiresActualPassingLifecycle(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"FuzzPresent", "FuzzSkipped", "FuzzMissing"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := fuzzLifecycleFixture(t)

			err := runFuzzTarget(t.Context(), dir, fuzzTarget{pkg: "fuzz.fixture", name: name}, "1x")
			if (err == nil) != (name == "FuzzPresent") {
				t.Fatalf("target %s: %v", name, err)
			}
		})
	}
}

func fuzzLifecycleFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	for name, data := range map[string]string{
		moduleFileName: "module fuzz.fixture\n\ngo 1.27.1\n",
		"fuzz_test.go": fuzzLifecycleSource,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), fileMode); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func TestFuzzCancellationStopsActualInFlightWorker(t *testing.T) {
	t.Parallel()
	dir := fuzzLifecycleFixture(t)

	source := `package fixture
import("testing";"os";"os/exec";"strconv";"time")
func recordPID(t *testing.T,name string) {
 if err:=os.WriteFile(name+".pending",[]byte(strconv.Itoa(os.Getpid())),0600);err!=nil {t.Fatal(err)}
 if err:=os.Rename(name+".pending",name);err!=nil {t.Fatal(err)}
}
func TestFuzzDescendant(t *testing.T) {recordPID(t,"descendant.pid");time.Sleep(time.Hour)}
func FuzzHang(f *testing.F) { f.Add(1); f.Fuzz(func(t *testing.T,n int) {
 child:=exec.Command(os.Args[0],"-test.run=^TestFuzzDescendant$")
 if err:=child.Start();err!=nil {t.Fatal(err)}
 recordPID(t,"worker.pid")
 time.Sleep(time.Hour)
}) }
`
	if err := os.WriteFile(filepath.Join(dir, "fuzz_test.go"), []byte(source), fileMode); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- runFuzzTarget(ctx, dir, fuzzTarget{pkg: "fuzz.fixture", name: "FuzzHang"}, "1x") }()

	pid := awaitFuzzWorker(t, dir, "worker.pid", done)
	descendant := awaitFuzzWorker(t, dir, "descendant.pid", done)

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lost cancellation: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("fuzz cancellation did not finish")
	}

	requireFuzzWorkerExited(t, pid)
	requireFuzzWorkerExited(t, descendant)
}

func awaitFuzzWorker(t *testing.T, dir, name string, done <-chan error) int {
	t.Helper()

	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()

	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()

	root, openErr := os.OpenRoot(dir)
	if openErr != nil {
		t.Fatal(openErr)
	}

	t.Cleanup(func() {
		if releaseErr := root.Close(); releaseErr != nil {
			t.Error(releaseErr)
		}
	})

	for {
		content, err := root.ReadFile(name)
		if err == nil {
			pid, parseErr := strconv.Atoi(string(content))
			if parseErr != nil || pid < 1 {
				t.Fatalf("invalid worker identity %q: %v", content, parseErr)
			}

			return pid
		}

		if !os.IsNotExist(err) {
			t.Fatal(err)
		}

		select {
		case runErr := <-done:
			t.Fatalf("fuzz runner stopped before readiness: %v", runErr)
		case <-timeout.C:
			t.Fatal("fuzz worker did not start")
		case <-poll.C:
		}
	}
}

func requireFuzzWorkerExited(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if fuzzWorkerStopped(pid) {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("owned worker %d survived fuzz cleanup", pid)
}

func TestFuzzLauncherPreservesOrdinaryCompletionAndExitFailure(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"package main\nimport \"fmt\"\nfunc main(){fmt.Print(\"captured\")}\n",
		"package main\nimport \"os\"\nfunc main(){os.Exit(7)}\n",
	} {
		dir := t.TempDir()

		file := filepath.Join(dir, "launcher.go")
		if err := os.WriteFile(file, []byte(source), fileMode); err != nil {
			t.Fatal(err)
		}

		run := &command{dir: dir, name: goTool, args: []string{"run", file}, fuzzLifecycle: true}
		output, err := run.output(t.Context())

		failure := strings.Contains(source, "os.Exit")
		if (err != nil) != failure || (!failure && output != "captured") {
			t.Fatalf("output=%q error=%v", output, err)
		}
	}
}

func TestFuzzNormalExitStopsStrayDescendant(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := `package main
import("os";"os/exec";"strconv";"time")
func main() {
 if len(os.Args)>1 {
  if err:=os.WriteFile("stray.pid.pending",[]byte(strconv.Itoa(os.Getpid())),0600);err!=nil {panic(err)}
  if err:=os.Rename("stray.pid.pending","stray.pid");err!=nil {panic(err)}
  time.Sleep(time.Hour)
 }
 child:=exec.Command(os.Args[0],"child")
 if err:=child.Start();err!=nil {panic(err)}
 for {
  if _,err:=os.Stat("stray.pid");err==nil {return}
  time.Sleep(time.Millisecond)
 }
}
`

	file := filepath.Join(dir, "launcher.go")
	if err := os.WriteFile(file, []byte(source), fileMode); err != nil {
		t.Fatal(err)
	}

	run := &command{dir: dir, name: goTool, args: []string{"run", file}, fuzzLifecycle: true}
	if err := run.run(t.Context()); err != nil {
		t.Fatal(err)
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if releaseErr := root.Close(); releaseErr != nil {
			t.Error(releaseErr)
		}
	})

	content, err := root.ReadFile("stray.pid")
	if err != nil {
		t.Fatal(err)
	}

	pid, err := strconv.Atoi(string(content))
	if err != nil {
		t.Fatal(err)
	}

	requireFuzzWorkerExited(t, pid)
}

func TestFuzzExitObserverAcceptsFastUnreapedChild(t *testing.T) {
	t.Parallel()

	observer, err := newFuzzExitObserver()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if releaseErr := observer.release(); releaseErr != nil {
			t.Error(releaseErr)
		}
	})

	child := exec.CommandContext(t.Context(), "/bin/sh", "-c", "exit 0")

	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err = child.Start(); err != nil {
		t.Fatal(err)
	}

	waited := false

	t.Cleanup(func() {
		if waited {
			return
		}

		if waitErr := child.Wait(); waitErr != nil {
			t.Error(waitErr)
		}
	})

	// EOF deliberately precedes the only Wait, exercising fast native exit/registration.
	if _, err = io.Copy(io.Discard, output); err != nil {
		t.Fatal(err)
	}

	if err = observer.attach(child.Process.Pid); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	if err = waitFuzzExit(ctx, &observer, child.Process.Pid); err != nil {
		t.Fatal(err)
	}

	err = child.Wait()
	waited = true

	if err != nil {
		t.Fatal(err)
	}
}
