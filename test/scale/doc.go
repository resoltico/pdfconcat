// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package scale is the reproducible scale acceptance harness. It generates deterministic fixtures,
// builds the real pdfconcat executable, runs it on large plans, measures wall time, peak resident
// memory, open descriptors and scratch disk, and then checks the output with qpdf and Poppler.
//
// Run it with
//
//	export PDFCONCAT_SCALE=1 PDFCONCAT_REQUIRE_QA_TOOLS=1
//	go run ./tools/qualitygate test -run ^TestScaleAcceptance$ -require TestScaleAcceptance -timeout 30m ./test/scale
//
// Without PDFCONCAT_SCALE=1 the acceptance test skips, saying so. With it set, a missing prerequisite
// (the executable does not build, qpdf or Poppler is missing) fails the test, so a dedicated CI job
// cannot pass by skipping. PDFCONCAT_SCALE_RESULTS names a file that receives one JSON object per run.
//
// Cases: 5,000 distinct one-page PDFs interleaved with 5,000 generated pages as a plan file and on
// standard input, with one repeated and with all-distinct blank styles; a 15,000-page job with repeated
// source occurrences, grouped paths, compact blank counts and linked sources; and a resource-heavy
// corpus of image-rich and multi-page PDFs; and 3,000 distinct generated pages with 10,000-character
// texts via file and stdin. Long-text cases verify complete extracted text and report their envelope,
// without claiming that their workload has the light-case budgets. The light cases are gated at 1 GiB peak resident memory,
// 64 open descriptors at --jobs 4 and 60 seconds; the other cases are measured and reported.
//
// Measurement covers the executable from launch to exit. Fixture generation and the independent
// verification (qpdf, pdftotext, pdftoppm) are timed separately and excluded. Peak memory comes from
// the kernel's accounting of the child (rusage on Unix, the peak working set on Windows); descriptors
// are sampled while the process runs. Windows peak RSS is a sampled lower bound, while Unix RSS
// uses launch-through-exit kernel accounting. Each result records valid/unavailable/error states,
// successful/attempted descriptor samples and the sampled temporal span as a fraction of wall time;
// that fraction does not prove every transient maximum was observed. Enabled acceptance rejects
// missing or failed required readings. On Unix the child also runs under a descriptor limit equal
// to the gate, so exceeding it fails the run instead of going unseen.
package scale
