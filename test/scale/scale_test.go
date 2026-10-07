// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package scale_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/exectest"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
	"github.com/resoltico/pdfconcat/test/scale"
)

type (
	// result is one row of the acceptance output.
	result struct {
		ExitObserverRetries       scale.ObserverRetryEvidence `json:"exit_observer_retries,omitzero"`
		DescriptorState           string                      `json:"descriptor_state"`
		Outcome                   string                      `json:"outcome"`
		Case                      string                      `json:"case"`
		RSSState                  string                      `json:"rss_state"`
		Input                     string                      `json:"input"`
		Environment               string                      `json:"environment"`
		ReadingFailures           []scale.ReadingFailure      `json:"reading_failures,omitempty"`
		DescriptorTerminalSamples int64                       `json:"descriptor_terminal_samples"`
		TerminalSamples           int64                       `json:"terminal_samples"`
		PeakRSSMiB                float64                     `json:"peak_rss_mib"`
		StdoutBytes               int                         `json:"stdout_bytes"`
		MaxDescriptors            int64                       `json:"max_descriptors"`
		FixtureSeconds            float64                     `json:"fixture_s"`
		OutputObjects             int                         `json:"output_objects"`
		WallSeconds               float64                     `json:"wall_s"`
		ExitCode                  int                         `json:"exit_code"`
		InputMiB                  float64                     `json:"input_mib"`
		ScratchPeakMiB            float64                     `json:"scratch_peak_mib"`
		PlanMiB                   float64                     `json:"plan_mib"`
		StderrBytes               int                         `json:"stderr_bytes"`
		OutputMiB                 float64                     `json:"output_mib"`
		VerifySeconds             float64                     `json:"verify_s"`
		DescriptorLimit           uint64                      `json:"descriptor_limit,omitempty"`
		SourceFiles               int                         `json:"source_files"`
		Pages                     int                         `json:"pages"`
		DescriptorSamples         int64                       `json:"descriptor_samples"`
		SampleAttempts            int64                       `json:"sample_attempts"`
		DescriptorCoverage        float64                     `json:"descriptor_coverage"`
		Gated                     bool                        `json:"gated"`
	}

	// acceptanceCase is one executable run on one workload.
	acceptanceCase struct {
		build func(dir string) (*scale.Workload, error)
		name  string
		stdin bool
		gated bool
	}

	// caseEnvironment is what every acceptance case shares.
	caseEnvironment struct {
		binary      string
		description string
		tools       pdforacle.Tools
	}
)

const (
	// Environment variables of the acceptance test.
	envScale   = "PDFCONCAT_SCALE"
	envResults = "PDFCONCAT_SCALE_RESULTS"
	envDir     = "PDFCONCAT_SCALE_DIR"
	envHelper  = "PDFCONCAT_SCALE_HELPER"
	// envBinary names a prebuilt executable to measure instead of building ./cmd/pdfconcat, for example
	// a release archive's binary.
	envBinary = "PDFCONCAT_SCALE_BINARY"

	// Budgets of the light 5,000 + 5,000 case, declared by the project's acceptance criteria.
	outputDirectory               = "out"
	deterministicFixtureDirectory = "dir"
	gateRSSBytes                  = 1 << 30
	gateDescriptors               = 64
	gateWall                      = 60 * time.Second
	jobs                          = "4"

	// Sizes the measurement self-test's child process uses.
	helperMemoryBytes     = 64 << 20
	helperScratchBytes    = 3 << 20
	helperDescriptors     = 10
	helperOpenAttempts    = 100
	helperPageBytes       = 4096
	helperWait            = 60 * time.Second
	helperExitAllocated   = 3
	helperExitOpenFailed  = 4
	helperExitWriteFailed = 5
	helperExitHoldFailed  = 6
	helperExitLimitHit    = 7
	helperExitUnknownMode = 2
)

func TestMain(m *testing.M) {
	mode := os.Getenv(envHelper)
	if mode != "" {
		os.Exit(runHelper(mode))
	}

	m.Run()
	exectest.Cleanup()
}

func mib(size int64) float64 { return float64(size) / (1 << 20) }

// TestScaleAcceptance runs the cases one after another: they are measured, so they never share the
// machine with another case, and the documented invocation selects this test alone.
func TestScaleAcceptance(t *testing.T) {
	t.Parallel()

	if os.Getenv(envScale) != "1" {
		t.Skipf("scale acceptance not requested: set %[1]s=1 to build the CLI and run the 5,000+5,000, 15,000-page and "+
			"resource-heavy cases (minutes; hundreds of MiB of fixtures; set %[1]s=1 in the dedicated CI job so a skip "+
			"cannot pass)", envScale)
	}

	tools, err := pdforacle.FindTools()
	if err != nil {
		t.Fatalf("%s=1 requires qpdf and Poppler: %v", envScale, err)
	}

	binary := os.Getenv(envBinary)
	if binary == "" {
		binary = exectest.Build(t, "./cmd/pdfconcat")
	}

	shared := caseEnvironment{binary: binary, tools: tools, description: describeEnvironment(t.Context(), tools)}
	t.Log(shared.description)

	root := os.Getenv(envDir)
	if root == "" {
		root = t.TempDir()
	}

	var rows []result

	for _, tc := range acceptanceCases() {
		row, ok := runCase(t, &shared, filepath.Join(filepath.Clean(root), tc.name), &tc)
		if ok {
			rows = append(rows, row)
		}
	}

	writeResults(t, rows)
}

func acceptanceCases() []acceptanceCase {
	light := func(count int, styles scale.BlankStyles) func(string) (*scale.Workload, error) {
		return func(dir string) (*scale.Workload, error) { return scale.LightWorkload(dir, count, styles) }
	}

	return []acceptanceCase{
		{light(100, scale.DistinctBlankStyles), "light-100-distinct-file", false, false},
		{light(1000, scale.DistinctBlankStyles), "light-1000-distinct-file", false, false},
		{light(5000, scale.SharedBlankStyle), "light-5000-repeated-file", false, true},
		{light(5000, scale.DistinctBlankStyles), "light-5000-distinct-file", false, true},
		{light(5000, scale.SharedBlankStyle), "light-5000-repeated-stdin", true, true},
		{light(5000, scale.DistinctBlankStyles), "light-5000-distinct-stdin", true, true},
		{scale.MixedWorkload, "mixed-15000-file", false, false},
		{scale.HeavyWorkload, "resource-heavy-file", false, false},
		{scale.LongTextWorkload, "long-text-3000-file", false, false},
		{scale.LongTextWorkload, "long-text-3000-stdin", true, false},
	}
}

// runCase builds the case's fixtures, runs the executable on them, gates and independently verifies the
// output, and reports failures against the test. It returns the case's result row, and false when the
// case could not be completed.
func runCase(t *testing.T, shared *caseEnvironment, dir string, tc *acceptanceCase) (result, bool) {
	t.Helper()

	fixtureStart := time.Now()

	workload, prepared := prepareCase(t, dir, tc)
	if !prepared {
		return result{}, false
	}

	fixtureSeconds := time.Since(fixtureStart).Seconds()

	spec := caseSpec(shared.binary, dir, workload, tc)

	measured, err := scale.Measure(t.Context(), spec)

	row := newRow(tc, workload, &measured)
	if err != nil {
		t.Errorf("%s: %v", tc.name, err)

		row.Outcome = "measurement_failed"

		return row, true
	}

	row.Environment = shared.description
	row.FixtureSeconds = fixtureSeconds
	row.DescriptorLimit = spec.DescriptorLimit

	checkMeasurements(t, tc.name, &measured)

	if measured.ExitCode != 0 {
		t.Errorf(
			"%s: pdfconcat exited %d\nstdout: %s\nstderr: %s",
			tc.name,
			measured.ExitCode,
			tail(measured.Stdout),
			tail(measured.Stderr),
		)

		row.Outcome = "executable_failed"
		t.Logf("%+v", row)

		return row, true
	}

	if tc.gated {
		checkGates(t, tc.name, &measured)
	}

	verified, ok := verifyCase(t, shared.tools, dir, workload, tc.name, &row)
	if !ok {
		row.Outcome = "verification_failed"
		return row, true
	}

	return verified, true
}

// verifyCase checks the case's output files and judges the output independently of the executable,
// completing the result row.
func verifyCase(t *testing.T, tools pdforacle.Tools, dir string, workload *scale.Workload, name string, row *result) (result, bool) {
	t.Helper()

	output := filepath.Join(dir, outputDirectory, "out.pdf")

	size, err := scale.FileSize(output)
	if err != nil {
		t.Errorf("%s: no output: %v", name, err)

		return result{}, false
	}

	_, err = scale.FileSize(filepath.Join(dir, outputDirectory, "report.json"))

	reportPresent := err == nil
	if err != nil {
		t.Errorf("%s: no report: %v", name, err)
	}

	verification, err := scale.Verify(tools, workload, output)
	if err != nil {
		t.Errorf("%s: %v", name, err)

		return result{}, false
	}

	row.Outcome = "verified"
	if !reportPresent || verification.Pages != workload.Pages || len(verification.Findings) != 0 {
		row.Outcome = "verification_failed"
	}

	row.OutputMiB = mib(size)
	row.VerifySeconds = verification.Elapsed.Seconds()
	row.OutputObjects = verification.Objects

	reportVerification(t, name, workload, &verification)
	t.Logf("%+v", row)

	return *row, true
}

// prepareCase creates the case's directories, fixtures and plan file.
func prepareCase(t *testing.T, dir string, tc *acceptanceCase) (*scale.Workload, bool) {
	t.Helper()

	workload, err := tc.build(dir)
	if err != nil {
		t.Errorf("%s: build workload: %v", tc.name, err)

		return nil, false
	}

	err = workload.Stage()
	if err != nil {
		t.Errorf("%s: write fixtures: %v", tc.name, err)

		return nil, false
	}

	return workload, true
}

// caseSpec is the measured command line of a case: the plan arrives as a file or on standard input.
func caseSpec(binary, dir string, workload *scale.Workload, tc *acceptanceCase) scale.RunSpec {
	plan := "job.json"
	if tc.stdin {
		plan = "-"
	}

	spec := scale.RunSpec{
		Binary: binary, Dir: dir, ScratchDir: filepath.Join(dir, outputDirectory),
		Args: []string{"build", "--plan", plan, "-o", "out/out.pdf", "--report", "out/report.json", "--jobs", jobs},
	}

	if tc.stdin {
		spec.Stdin = bytes.NewReader(workload.Plan)
	}

	if tc.gated {
		spec.DescriptorLimit = gateDescriptors
	}

	return spec
}

// newRow records what the run cost; the caller adds the output size, fixture time and verification.
func newRow(tc *acceptanceCase, workload *scale.Workload, measured *scale.Measurement) result {
	input := "file"
	if tc.stdin {
		input = "stdin"
	}

	return result{
		ExitObserverRetries:       measured.ObserverRetries,
		ReadingFailures:           measured.ReadingFailures,
		TerminalSamples:           measured.TerminalSamples,
		DescriptorTerminalSamples: measured.DescriptorTerminalSamples,
		Case:                      tc.name,
		Input:                     input,
		Pages:                     workload.Pages,
		SourceFiles:               workload.SourceFiles,
		InputMiB:                  mib(workload.InputBytes),
		PlanMiB:                   mib(int64(len(workload.Plan))),
		WallSeconds:               measured.Wall.Seconds(),
		PeakRSSMiB:                mib(measured.PeakRSSBytes),
		MaxDescriptors:            measured.MaxDescriptors,
		ScratchPeakMiB:            mib(measured.PeakScratchBytes),
		StdoutBytes:               len(measured.Stdout),
		StderrBytes:               len(measured.Stderr),
		Gated:                     tc.gated,
		RSSState:                  measured.RSSState,
		DescriptorState:           measured.DescriptorState,
		DescriptorSamples:         measured.DescriptorSamples,
		SampleAttempts:            measured.SampleAttempts,
		DescriptorCoverage:        measured.DescriptorCoverage,
	}
}

func reportVerification(t *testing.T, name string, workload *scale.Workload, verification *scale.Verification) {
	t.Helper()

	if verification.Pages != workload.Pages {
		t.Errorf("%s: output has %d pages, want %d", name, verification.Pages, workload.Pages)
	}

	for _, finding := range verification.Findings {
		t.Errorf("%s: independent verification: %s", name, finding)
	}
}

func checkGates(t *testing.T, name string, measured *scale.Measurement) {
	t.Helper()

	if measured.PeakRSSBytes > gateRSSBytes {
		t.Errorf("%s: peak RSS %.0f MiB exceeds the %d MiB budget", name, mib(measured.PeakRSSBytes), gateRSSBytes>>20)
	}

	if measured.MaxDescriptors > gateDescriptors {
		t.Errorf("%s: %d open descriptors exceed the budget of %d", name, measured.MaxDescriptors, gateDescriptors)
	}

	if measured.Wall > gateWall {
		t.Errorf("%s: wall time %v exceeds the %v budget", name, measured.Wall, gateWall)
	}
}

func tail(text string) string {
	const limit = 2000
	if len(text) > limit {
		return "..." + text[len(text)-limit:]
	}

	return text
}

func describeEnvironment(ctx context.Context, tools pdforacle.Tools) string {
	version := func(program string, args ...string) string {
		output, err := exectest.Command(ctx, program, args...).CombinedOutput()
		if err != nil {
			return "unknown"
		}

		first, _, _ := strings.Cut(string(output), "\n")

		return strings.TrimSpace(first)
	}

	return fmt.Sprintf("%s/%s, %d CPUs, GOMAXPROCS %d, %s; qpdf %s; pdftotext %s",
		runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.GOMAXPROCS(0), runtime.Version(),
		strings.TrimPrefix(version(tools.QPDF, "--version"), "qpdf version "), version(tools.PDFToText, "-v"))
}

func writeResults(t *testing.T, rows []result) {
	t.Helper()

	path := os.Getenv(envResults)
	if path == "" {
		return
	}

	var out bytes.Buffer

	encoder := json.NewEncoder(&out)
	for index := range rows {
		err := encoder.Encode(&rows[index])
		if err != nil {
			t.Fatal(err)
		}
	}

	err := scale.WriteFile(path, out.Bytes())
	if err != nil {
		t.Fatalf("write results: %v", err)
	}
}

// runHelper is the child process of the measurement self-test.
func runHelper(mode string) int {
	switch mode {
	case "allocate":
		return allocateHelper()
	case "open-many":
		return openManyHelper()
	default:
		return helperExitUnknownMode
	}
}

// allocateHelper touches 64 MiB, holds ten extra descriptors, leaves a scratch file, announces that it is
// in that state by creating the file named by the _READY variable, stays until the file named by the
// _RELEASE variable exists and exits 3.
func allocateHelper() int {
	block := make([]byte, helperMemoryBytes)
	for index := 0; index < len(block); index += helperPageBytes {
		block[index] = 1
	}

	files := make([]*os.File, 0, helperDescriptors)

	for range helperDescriptors {
		file, openErr := os.Open(os.DevNull)
		if openErr != nil {
			return helperExitOpenFailed
		}

		files = append(files, file)
	}

	err := scale.WriteFile(os.Getenv(envHelper+"_SCRATCH"), make([]byte, helperScratchBytes))
	if err != nil {
		return helperExitWriteFailed
	}

	err = scale.WriteFile(os.Getenv(envHelper+"_READY"), nil)
	if err != nil {
		return helperExitWriteFailed
	}

	release := os.Getenv(envHelper + "_RELEASE")

	for {
		_, err = scale.FileSize(release)
		if err == nil {
			break
		}

		if !errors.Is(err, fs.ErrNotExist) {
			return helperExitHoldFailed
		}

		time.Sleep(time.Millisecond)
	}

	runtime.KeepAlive(block)
	runtime.KeepAlive(files)

	return helperExitAllocated
}

// openManyHelper opens the executable repeatedly and exits 7 when an open fails, as it does under a
// descriptor limit.
func openManyHelper() int {
	files := make([]*os.File, 0, helperOpenAttempts)

	for range helperOpenAttempts {
		file, openErr := os.Open(os.DevNull)
		if openErr != nil {
			return helperExitLimitHit
		}

		files = append(files, file)
	}

	runtime.KeepAlive(files)

	return 0
}

// waitFor polls condition until it holds, failing the test if it does not within helperWait.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(helperWait)

	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("gave up waiting for %s", what)
		}

		time.Sleep(time.Millisecond)
	}
}

// measureHeldHelper measures the allocating helper and keeps it in the state it measures until the sampler
// has read it twice since it said it was there, so the readings do not depend on how long a reading takes
// under load. The second reading began after the helper was ready: one reading may have been under way when
// it announced itself. It returns the measurement and how long the helper was held after it was ready.
func measureHeldHelper(t *testing.T) (scale.Measurement, time.Duration) {
	t.Helper()

	var (
		scratch = t.TempDir()
		ready   = filepath.Join(scratch, "ready")
		release = filepath.Join(scratch, "release")
		samples atomic.Int64
	)

	type outcome struct {
		err      error
		measured scale.Measurement
	}

	finished := make(chan outcome, 1)

	go func() {
		measured, err := scale.Measure(t.Context(), scale.RunSpec{
			Binary: os.Args[0], Dir: t.TempDir(), ScratchDir: scratch,
			Env: append(os.Environ(), envHelper+"=allocate", envHelper+"_SCRATCH="+filepath.Join(scratch, "scratch.bin"),
				envHelper+"_READY="+ready, envHelper+"_RELEASE="+release),
			OnSample: func() { samples.Add(1) },
		})
		finished <- outcome{err: err, measured: measured}
	}()

	// Whatever happens next, the helper is released so that it does not outlive the test.
	t.Cleanup(func() {
		err := scale.WriteFile(release, nil)
		if err != nil {
			t.Logf("release the helper: %v", err)
		}
	})

	waitFor(t, "the helper to be ready", func() bool {
		_, err := os.Stat(ready)

		return err == nil
	})

	readyAt := time.Now()
	seen := samples.Load()

	waitFor(t, "two readings of the ready helper", func() bool { return samples.Load() >= seen+2 })

	held := time.Since(readyAt)

	err := scale.WriteFile(release, nil)
	if err != nil {
		t.Fatal(err)
	}

	done := <-finished
	if done.err != nil {
		t.Fatal(done.err)
	}

	return done.measured, held
}

func TestMeasureReportsProcessCosts(t *testing.T) {
	t.Parallel()

	measured, held := measureHeldHelper(t)

	if measured.ExitCode != 3 {
		t.Fatalf("exit code %d, want the helper's 3", measured.ExitCode)
	}

	if measured.Wall < held {
		t.Errorf("wall %v is shorter than the %v the helper was held", measured.Wall, held)
	}

	if measured.PeakRSSBytes < 64<<20 {
		t.Errorf("peak RSS %d MiB, want at least 64", measured.PeakRSSBytes>>20)
	}

	if measured.PeakScratchBytes < 3<<20 {
		t.Errorf("scratch peak %d bytes, want at least 3 MiB", measured.PeakScratchBytes)
	}

	if _, lsofErr := exec.LookPath("lsof"); runtime.GOOS == "linux" || runtime.GOOS == "windows" || lsofErr == nil {
		if measured.MaxDescriptors < 10 {
			t.Errorf("sampled %d descriptors, the child held at least 10", measured.MaxDescriptors)
		}
	}
}

func TestMeasureEnforcesDescriptorLimit(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows has no descriptor limit to impose; handles are only sampled")
	}

	run := func(limit uint64) int {
		measured, err := scale.Measure(t.Context(), scale.RunSpec{
			Binary: os.Args[0], Dir: t.TempDir(), DescriptorLimit: limit, Env: append(os.Environ(), envHelper+"=open-many"),
		})
		if err != nil {
			t.Fatal(err)
		}

		return measured.ExitCode
	}

	if code := run(0); code != 0 {
		t.Fatalf("unlimited child exited %d", code)
	}

	if code := run(64); code != 7 {
		t.Fatalf("child under a 64-descriptor limit exited %d, want 7 (open failed)", code)
	}
}

func TestMeasureReportsStartFailure(t *testing.T) {
	t.Parallel()

	_, err := scale.Measure(t.Context(), scale.RunSpec{Binary: filepath.Join(t.TempDir(), "absent")})
	if err == nil {
		t.Fatal("no error for a missing executable")
	}
}

// mustWorkload returns a function that unwraps a workload constructor's result, failing the test on error.
func mustWorkload(t *testing.T) func(*scale.Workload, error) *scale.Workload {
	t.Helper()

	return func(workload *scale.Workload, err error) *scale.Workload {
		t.Helper()

		if err != nil {
			t.Fatal(err)
		}

		return workload
	}
}

func TestWorkloadTotals(t *testing.T) {
	t.Parallel()

	light := mustWorkload(t)(scale.LightWorkload(t.TempDir(), 5000, scale.DistinctBlankStyles))
	if light.Pages != 10000 || len(light.Expectation.Pages) != 10000 {
		t.Errorf("light workload: %d pages, %d expected", light.Pages, len(light.Expectation.Pages))
	}

	mixed := mustWorkload(t)(scale.MixedWorkload(t.TempDir()))
	if mixed.Pages != scale.MixedPages || len(mixed.Expectation.Pages) != scale.MixedPages {
		t.Errorf("mixed workload: %d pages, %d expected, want %d", mixed.Pages, len(mixed.Expectation.Pages), scale.MixedPages)
	}

	checkEverySourceUsedTwice(t, mixed)

	heavy := mustWorkload(t)(scale.HeavyWorkload(t.TempDir()))
	if heavy.Pages != len(heavy.Expectation.Pages) || heavy.Pages == 0 {
		t.Errorf("heavy workload: %d pages, %d expected", heavy.Pages, len(heavy.Expectation.Pages))
	}

	for _, workload := range []*scale.Workload{light, mixed, heavy} {
		checkPlanShape(t, workload)
	}
}

// checkEverySourceUsedTwice verifies that source occurrences are numbered per source and every source
// is used twice.
func checkEverySourceUsedTwice(t *testing.T, workload *scale.Workload) {
	t.Helper()

	occurrences := map[string]int{}

	for _, page := range workload.Expectation.Pages {
		if page.Source != "" && page.Occurrence > occurrences[page.Source] {
			occurrences[page.Source] = page.Occurrence
		}
	}

	for source, count := range occurrences {
		if count != 2 {
			t.Fatalf("source %s occurs %d times, want 2", source, count)
		}
	}
}

func checkPlanShape(t *testing.T, workload *scale.Workload) {
	t.Helper()

	var plan struct {
		Items   []any `json:"items"`
		Version int   `json:"version"`
	}

	err := json.Unmarshal(workload.Plan, &plan)
	if err != nil || plan.Version != 1 || len(plan.Items) == 0 {
		t.Errorf("%s: plan is not a version 1 plan with items: %v", workload.Name, err)
	}
}

func TestWorkloadsAreDeterministic(t *testing.T) {
	t.Parallel()

	first := mustWorkload(t)(scale.LightWorkload(deterministicFixtureDirectory, 50, scale.DistinctBlankStyles))
	second := mustWorkload(t)(scale.LightWorkload(deterministicFixtureDirectory, 50, scale.DistinctBlankStyles))
	sameFixtures := strings.Join(first.FixtureSignature(), ",") == strings.Join(second.FixtureSignature(), ",")

	if !bytes.Equal(first.Plan, second.Plan) || !sameFixtures {
		t.Fatal("two builds of the same workload differ")
	}

	other := mustWorkload(t)(scale.LightWorkload(deterministicFixtureDirectory, 50, scale.SharedBlankStyle))

	if bytes.Equal(first.Plan, other.Plan) {
		t.Fatal("distinct-style and repeated-style plans are identical")
	}
}

// TestVerificationPipelineJudgesAnAssembledOutput runs the verification used by the acceptance test on a
// real output assembled by the engine from the workload's own sources, and checks it rejects a
// deliberately wrong expectation. Generated pages come from a fixture resource document, so their sizes
// are not asserted here.
func TestVerificationPipelineJudgesAnAssembledOutput(t *testing.T) {
	t.Parallel()

	const count = 12

	tools := pdforacle.RequireTools(t)
	dir := t.TempDir()
	workload := mustWorkload(t)(scale.LightWorkload(dir, count, scale.DistinctBlankStyles))

	err := workload.Write()
	if err != nil {
		t.Fatal(err)
	}

	destination := assembleWorkload(t, dir, count)

	for index := range workload.Expectation.Pages {
		workload.Expectation.Pages[index].Geometry = nil
	}

	verification := verifyOutput(t, tools, workload, destination)
	if len(verification.Findings) != 0 || verification.Pages != 2*count || verification.Objects == 0 {
		t.Fatalf("correct output rejected: %+v", verification)
	}

	workload.Expectation.Pages[0], workload.Expectation.Pages[2] = workload.Expectation.Pages[2], workload.Expectation.Pages[0]

	if len(verifyOutput(t, tools, workload, destination).Findings) == 0 {
		t.Fatal("swapped source pages were not detected")
	}

	workload.Appearances = []scale.Appearance{{Page: 1, X: 2, Y: 2, Color: [3]uint8{0, 0, 0}}}

	found := false

	for _, finding := range verifyOutput(t, tools, workload, destination).Findings {
		found = found || strings.HasPrefix(finding, "appearance:")
	}

	if !found {
		t.Fatal("a wrong rendered color was not detected")
	}
}

func verifyOutput(t *testing.T, tools pdforacle.Tools, workload *scale.Workload, path string) scale.Verification {
	t.Helper()

	verification, err := scale.Verify(tools, workload, path)
	if err != nil {
		t.Fatal(err)
	}

	return verification
}

// assembleWorkload interleaves the workload's count sources with count generated pages using the engine
// and returns the output path.
func assembleWorkload(t *testing.T, dir string, count int) string {
	t.Helper()

	engine, err := pdfengine.New()
	if err != nil {
		t.Fatal(err)
	}

	resource := filepath.Join(dir, "resource.pdf")

	err = pdffixture.Resource(count, func(index int) string { return fmt.Sprintf("BLANK %05d", index) }).WriteFile(resource)
	if err != nil {
		t.Fatal(err)
	}

	request := pdfengine.AssembleRequest{
		Resource:      &pdfengine.ResourceDocument{Path: resource, Pages: count},
		Destination:   filepath.Join(dir, "out.pdf"),
		ExpectedPages: 2 * count,
	}

	for index := range count {
		path := filepath.Join(dir, "src", fmt.Sprintf("s%05d.pdf", index))

		info, inspectErr := engine.Inspect(t.Context(), path)
		if inspectErr != nil {
			t.Fatal(inspectErr)
		}

		request.Sources = append(request.Sources, pdfengine.SourceFile{Path: path, Info: info})
		request.Order = append(request.Order, pdfengine.SourcePages(index, 1, 1), pdfengine.GeneratedPages(index, 1))
	}

	err = engine.Assemble(t.Context(), &request)
	if err != nil {
		t.Fatal(err)
	}

	return request.Destination
}

func checkMeasurements(t *testing.T, name string, measured *scale.Measurement) {
	t.Helper()

	if measured.RSSState != "valid" || measured.DescriptorState != "valid" {
		t.Errorf(
			"%s: required measurements invalid: RSS=%s descriptors=%s (%d/%d samples)",
			name,
			measured.RSSState,
			measured.DescriptorState,
			measured.DescriptorSamples,
			measured.SampleAttempts,
		)
	}
}

func TestMeasuredRowPlanBytesMatchActualStagedInstructions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	workload, buildErr := scale.LightWorkload(dir, 2, scale.DistinctBlankStyles)
	if buildErr != nil {
		t.Fatal(buildErr)
	}

	if stageErr := workload.Stage(); stageErr != nil {
		t.Fatal(stageErr)
	}

	info, statErr := os.Stat(filepath.Join(dir, "job.json"))
	if statErr != nil {
		t.Fatal(statErr)
	}

	for _, stdin := range []bool{false, true} {
		row := newRow(&acceptanceCase{stdin: stdin}, workload, &scale.Measurement{})

		want := float64(info.Size()) / (1 << 20)
		if row.PlanMiB != want || row.PlanMiB <= 0 || row.InputMiB <= 0 {
			t.Fatalf("plan/source byte counts lost or conflated: %+v wantplan=%g", row, want)
		}
	}
}
