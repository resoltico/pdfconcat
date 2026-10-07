// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

// Engine scale benchmarks. Run one case at a time, for example
//
//	go test -c -o pdfengine.test ./internal/pdfengine
//	/usr/bin/time -l ./pdfengine.test -test.run '^$' -test.bench 'Assemble/light/n=5000' -test.benchtime 1x
//
// /usr/bin/time (-l on macOS, -v on Linux) reports the process's peak resident set; the benchmark itself
// reports wall time, the largest number of open descriptors it sampled, and output size.

type (
	// scaleFiles holds distinct generated fixtures: single-page "S<i>" sources, two-page linked "L<i>"
	// sources, and a resource document with one page per index.
	scaleFiles struct {
		resource      string
		plain, linked []string
	}

	// descriptorSampler records the largest number of open descriptors seen, polling every millisecond. It
	// reads /dev/fd (macOS, BSD) or /proc/self/fd (Linux); where neither exists it reports -1.
	descriptorSampler struct {
		stop chan struct{}
		done chan struct{}
		peak atomic.Int64
	}
)

func writeScaleFiles(tb testing.TB, count, annotatedEvery int) scaleFiles {
	tb.Helper()

	dir := tb.TempDir()
	files := scaleFiles{resource: filepath.Join(dir, "resource.pdf")}

	for index := range count {
		files.plain = append(files.plain, writeDoc(tb, dir, fmt.Sprintf("s%05d", index), pdffixture.Plain(fmt.Sprintf("S%05d", index))))

		if annotatedEvery > 0 && index%annotatedEvery == 0 {
			files.linked = append(
				files.linked,
				writeDoc(tb, dir, fmt.Sprintf("l%05d", index), pdffixture.Links(fmt.Sprintf("L%05d", index))),
			)
		}
	}

	doc := pdffixture.Resource(count, func(index int) string { return fmt.Sprintf("G%05d", index) })
	if err := doc.WriteFile(files.resource); err != nil {
		tb.Fatal(err)
	}

	return files
}

func inspectAll(tb testing.TB, engine *pdfengine.Engine, paths []string) []pdfengine.SourceFile {
	tb.Helper()

	sources := make([]pdfengine.SourceFile, len(paths))

	for index, path := range paths {
		info, err := engine.Inspect(context.Background(), path)
		if err != nil {
			tb.Fatal(err)
		}

		sources[index] = pdfengine.SourceFile{Path: path, Info: info}
	}

	return sources
}

func startDescriptorSampler() *descriptorSampler {
	sampler := &descriptorSampler{stop: make(chan struct{}), done: make(chan struct{})}
	sampler.peak.Store(-1)

	go func() {
		defer close(sampler.done)

		for {
			for _, dir := range []string{"/proc/self/fd", "/dev/fd"} {
				if entries, err := os.ReadDir(dir); err == nil {
					sampler.peak.Store(max(sampler.peak.Load(), int64(len(entries))))

					break
				}
			}

			select {
			case <-sampler.stop:
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()

	return sampler
}

func (s *descriptorSampler) finish() int64 {
	close(s.stop)
	<-s.done

	return s.peak.Load()
}

func runAssembleBenchmark(b *testing.B, engine *pdfengine.Engine, request pdfengine.AssembleRequest) {
	b.Helper()

	for range b.N {
		request.Destination = filepath.Join(b.TempDir(), outputFilename)

		sampler := startDescriptorSampler()
		started := time.Now()

		err := engine.Assemble(context.Background(), &request)

		elapsed := time.Since(started)
		descriptors := sampler.finish()

		if err != nil {
			b.Fatal(err)
		}

		info, err := os.Stat(request.Destination)
		if err != nil {
			b.Fatal(err)
		}

		var memory runtime.MemStats

		runtime.ReadMemStats(&memory)

		b.ReportMetric(float64(request.ExpectedPages), "pages")
		b.ReportMetric(float64(info.Size())/(1<<20), "output-MiB")
		b.ReportMetric(float64(descriptors), "max-fds")
		b.ReportMetric(float64(memory.Sys)/(1<<20), "go-sys-MiB")
		b.ReportMetric(elapsed.Seconds(), "assemble-s")
	}
}

// BenchmarkAssemble measures one Assemble call (import, reorder, write, verify) over distinct inputs.
//
// light: n distinct one-page sources interleaved with n distinct generated pages (2n output pages).
// repeat: the same inputs, then every source a second time in reverse order, with leading and trailing
// generated runs (3n+4 output pages).
// annotated: like repeat, but every fifth source is a two-page linked document, imported per occurrence.
func BenchmarkAssemble(b *testing.B) {
	engine := newEngine(b)

	for _, count := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("light/n=%d", count), func(b *testing.B) {
			files := writeScaleFiles(b, count, 0)
			request := pdfengine.AssembleRequest{
				Sources: inspectAll(b, engine, files.plain), Resource: &pdfengine.ResourceDocument{Path: files.resource, Pages: count},
			}

			for index := range count {
				request.Order = append(request.Order, pdfengine.SourcePages(index, 1, 1), pdfengine.GeneratedPages(index, 1))
			}

			request.ExpectedPages = 2 * count

			b.ResetTimer()
			runAssembleBenchmark(b, engine, request)
		})

		b.Run(fmt.Sprintf("repeat/n=%d", count), func(b *testing.B) {
			files := writeScaleFiles(b, count, 0)
			request := repeatedRequest(inspectAll(b, engine, files.plain), files.resource, count)

			b.ResetTimer()
			runAssembleBenchmark(b, engine, request)
		})

		b.Run(fmt.Sprintf("annotated/n=%d", count), func(b *testing.B) {
			files := writeScaleFiles(b, count, 5)
			paths := make([]string, count)
			linked := 0

			for index := range count {
				paths[index] = files.plain[index]

				if index%5 == 0 {
					paths[index] = files.linked[linked]
					linked++
				}
			}

			request := repeatedRequest(inspectAll(b, engine, paths), files.resource, count)

			b.ResetTimer()
			runAssembleBenchmark(b, engine, request)
		})
	}
}

// repeatedRequest is the "repeat" shape over sources, each used whole.
func repeatedRequest(sources []pdfengine.SourceFile, resource string, count int) pdfengine.AssembleRequest {
	request := pdfengine.AssembleRequest{Sources: sources, Resource: &pdfengine.ResourceDocument{Path: resource, Pages: count}}
	pages := 0

	add := func(run pdfengine.Run) {
		request.Order = append(request.Order, run)
		pages += run.Count
	}

	whole := func(index int) pdfengine.Run { return pdfengine.SourcePages(index, 1, sources[index].Info.Pages) }

	add(pdfengine.GeneratedPages(0, 2))

	for index := range count {
		add(whole(index))
		add(pdfengine.GeneratedPages(index, 1))
	}

	for index := count - 1; index >= 0; index-- {
		add(whole(index))
	}

	add(pdfengine.GeneratedPages(count-1, 2))

	request.ExpectedPages = pages

	return request
}

// BenchmarkRepeatedPage measures compact repeat counts: one generated page repeated many times,
// where the work is the page-dictionary clone per output page.
func BenchmarkRepeatedPage(b *testing.B) {
	engine := newEngine(b)
	files := writeScaleFiles(b, 1, 0)

	for _, repeats := range []int{10_000, 100_000, 1_000_000} {
		b.Run(fmt.Sprintf("repeats=%d", repeats), func(b *testing.B) {
			request := pdfengine.AssembleRequest{
				Resource: &pdfengine.ResourceDocument{Path: files.resource, Pages: 1}, ExpectedPages: repeats,
				Order: []pdfengine.Run{pdfengine.GeneratedPages(0, repeats)},
			}

			b.ResetTimer()
			runAssembleBenchmark(b, engine, request)
		})
	}
}

// BenchmarkInspect measures Inspect over distinct one-page files.
func BenchmarkInspect(b *testing.B) {
	engine := newEngine(b)
	files := writeScaleFiles(b, 1000, 0)

	b.ResetTimer()

	for range b.N {
		for _, path := range files.plain {
			if _, err := engine.Inspect(context.Background(), path); err != nil {
				b.Fatal(err)
			}
		}
	}
}
