# Product and UX design

## Product sentence

PDFConcat assembles existing PDF documents in an explicitly specified order and inserts generated blank pages at explicitly specified positions.

That sentence is the scope test for proposed features.

## Why the sequence is the interface

The user's real problem is positional: "put these documents in this order, and put a blank here." The interface therefore exposes the final sequence directly instead of encoding an instruction on some other object.

Rejected models include:

- `BLK` or another filename suffix: mutates document identity to store assembly state;
- `--blank-after file.pdf`: separates the directive from the position it modifies and becomes awkward with repeated filenames;
- page-number directives: force the user or agent to know source page counts before assembly;
- a persistent `_blank.pdf`: creates asset management, geometry, and discovery problems;
- implicit directory scanning and filename sorting: hides ordering policy in filesystem rules and shell behavior; and
- multiple equivalent blank syntaxes: increases agent ambiguity without increasing capability.

## One model, four ways to write it

The sequence is an ordered list of PDF paths and generated blanks. A job can be written as a plan file, as a plan on standard input, as the same plan inline (`--plan-json`), or, for the simplest jobs, as direct operands (PDF paths and `--blank` in order). All four compile to the same `assembly.Job` through the same strict decoder or the same short constructor, so equivalent jobs have identical resolved semantics, and a test checks that the resolved layouts and the output pages are identical across the four.

There is deliberately no second configuration language. Appearance is written once, in the JSON plan, with one precedence (per item, then plan defaults, then built-ins). The older flag family (`--blank-text`, `--blank-size`, and the rest), the `--blank=TEXT` shorthand, and line-oriented or `--set` style overrides were removed because each created a second place to say the same thing and a precedence rule between them. Direct operands cannot style a page; a styled job is a plan.

## Agents first

Agents are the primary audience; people are the second. The consequences, each verified by tests:

- **Compact JSON everywhere by default.** Results, failures, help and version are single-line JSON documents on standard output; `--format text` is an explicit human rendering that scripts never need to scrape. Nothing but the result goes to standard output.
- **A summary, not a dump.** `build` and `check` print a small versioned summary (status, publication state, counts, diagnostic count, at most five diagnostics) that stays under 2 KiB on a normal success, says that it is a summary, and names the exact command that continues. The complete result is `--details` on standard output or `--report FILE` on disk.
- **Targeted queries instead of re-reading everything.** `pdfconcat report FILE` answers one question about a saved report without reopening any PDF: one contribution by its stable id, the contribution covering an output page, or a paged list of contributions or diagnostics with explicit totals and next offset. Style, font and source identities are stored once and materialized into the selected record, so inspecting a part is one call. There is no query language: these selectors cover reviewing order and appearance and working through a large failure set.
- **One error shape.** Usage errors, plan errors, I/O failures and interruptions all use the report boundary: stage, stable snake_case code, location (a JSON pointer, byte offset, line and column for a plan, `argv:N` for an operand), and an actionable message. Messages are for reading; codes are the contract.
- **Truthful partial results.** Counts and ranges are null, never zero, until the layout that determines them is complete, and the report's phases say how far the run got. An incomplete layout rejects page queries instead of fabricating ranges.
- **Exit codes that mean something.** 0 success, 2 invalid instructions (including malformed plans), 1 I/O, backend or publication failure, 130 interruption.
- **No state and no daemon.** A deterministic offline executable composes with shells and agents; there is no server, cache, session or configuration file.

An agent can generate a plan, `check` it with `--report`, read the bounded summary, fetch exactly the failing locations, edit the plan, `check` again, `build`, and read the final publication state from the saved report.

## Scale

The design target is thousands of source files and thousands of generated pages in one run. The mechanisms, not promises about a particular machine:

- **No per-blank rewrite.** Blanks are never inserted into an already-merged file. Resolution produces the final order as compact runs (a blank of a million pages is one run), and the engine assembles once.
- **Render once, share.** One generated-pages PDF holds one page per *distinct* resolved appearance, with each font embedded once and shared; every page that uses an appearance refers to it. Page order is then compiled into a pool of the imported documents, so cost grows with the size of the output, not with blanks times output size.
- **Snapshots, once.** Each distinct source and font file is copied once into a private workspace with a content digest computed during the copy, and inspected and assembled from that copy. The bytes that were validated are the bytes that are assembled, and no descriptors stay open across thousands of sources.
- **Bounded concurrency.** Source capture and inspection use at most `--jobs` workers (default the smaller of 4 and the CPU count, never more than there are sources), because every PDF parser can hold a large object graph. Assembly stays sequential.
- **All failures at once.** A stage reports every problem it found, in input order, even when stdout carries only five of them.
- **Early failure.** Plan syntax, appearance values, destination and file-role conflicts, fonts, supported scripts and glyph availability are checked before any source PDF is copied or opened. Effective field declaration locations survive inheritance and deduplication.
- **Arguments are not the transport.** The command line cannot carry thousands of paths on Windows, so plans can be read from a file or from standard input.
- **Declared bounds.** Plans have documented limits on size, group depth, structural nodes, flattened contributions and generated pages, charged while the plan is read, so a compact file cannot expand into an unbounded graph.

Memory is proportional to the size of the imported documents, because the embedded PDF engine builds the result in memory; there is no streaming mode, and no fixed envelope is claimed.

### Measured results

This is the only place in the documentation that states throughput, page-count or memory figures; other documents link here. They are measurements, not guarantees, and a later run of the scale harness will refresh them.

**Reproduce.** The harness in [`test/scale`](../test/scale/doc.go) generates deterministic fixtures (distinct source files and distinct generated styles, not one pathname repeated), runs the real executable on file and standard-input plans, measures wall time from launch to exit, peak resident memory, open descriptors and scratch disk, and checks the output with qpdf and Poppler:

```text
PDFCONCAT_SCALE=1 go test ./test/scale -run TestScaleAcceptance -count=1 -timeout 30m
```

Add `PDFCONCAT_REQUIRE_QA_TOOLS=1` and `-v` to make missing qpdf or Poppler fail instead of skip and to see each case, and set `PDFCONCAT_SCALE_RESULTS` to a file to receive one JSON object per case. The light 5,000-source, 5,000-generated-page cases are gated at 1 GiB peak memory, 64 open descriptors at `--jobs 4`, and 60 seconds; these are declared acceptance budgets. The mixed 15,000-page case and the resource-heavy corpus are measured and reported, not gated. The engine's own benchmarks: `go test ./internal/pdfengine -run '^$' -bench . -benchmem`.

**Final acceptance measurements (7 October 2026, local time).** Each platform ran all ten cases sequentially, with no other heavy work on the host, using the real executable at `--jobs 4` and `GOMAXPROCS=4`. The implementation and module-file bytes were identical across the measured source snapshots. These are single observations, not throughput guarantees. Wall time runs from launch through command completion, including standard-stream copying; fixture generation and independent qpdf/Poppler verification run outside that interval.

The macOS run used an Apple M4 with 10 cores and 32 GiB RAM, macOS 27.0 (26A428), Go 1.27.1, pdfcpu v0.16.1, qpdf 12.4.2 and Poppler 26.10.0. The Linux arm64 run executed as UID 501 in a Docker Desktop Linux 7.0.14 VM on the same Mac, with 10 logical CPUs, about 7.75 GiB VM memory, no per-container CPU/memory cgroup limit, Go 1.27.1, qpdf 12.2.0 and Poppler 25.03.0. Its fixtures and output were bind-mounted from macOS; scanning the output directory for scratch measurements adds substantial filesystem overhead. Its times describe that VM setup, not bare-metal Linux. Native Windows has not been measured.

| Job (`build`) | macOS wall / peak RSS | Linux VM wall / peak RSS | Sampled descriptors and disk |
| --- | ---: | ---: | --- |
| 100 sources + 100 generated pages | 0.162 s / 30.38 MiB | 0.328 s / 28.87 MiB | 7 / 11 descriptors |
| 1,000 sources + 1,000 generated pages | 0.511 s / 56.16 MiB | 2.286 s / 62.04 MiB | 10 / 12 descriptors |
| 5,000 sources + 5,000 generated pages; all four file/stdin and repeated/distinct-style variants | 2.047–2.602 s / 134.62–190.16 MiB | 11.199–21.652 s / 154.08–201.48 MiB | at most 13 descriptors on each platform; observed output-directory peaks 6.23–12.79 MiB; `ulimit -n 64` enforced |
| Mixed job, exactly 15,000 pages and 5,000 source files | 2.487 s / 226.42 MiB | 23.832 s / 238.03 MiB | 11 / 12 descriptors |
| Heavy corpus: 240 source files, 2,800 pages, 100.79 MiB of image-rich input | 0.674 s / 253.55 MiB | 4.293 s / 262.68 MiB | 12 / 14 descriptors; observed output-directory peak about 202 MiB |
| 3,000 distinct generated texts of 10,000 characters each, file plan | 9.797 s / 269.86 MiB | 16.168 s / 269.97 MiB | 7 / 8 descriptors; 28.779 MiB plan |
| Same 3,000-text workload, stdin plan | 11.191 s / 267.98 MiB | 11.069 s / 283.23 MiB | 6 / 8 descriptors; 28.779 MiB plan |

Both platforms passed every output oracle and all four light-case budgets. Their executable stderr was empty. macOS recorded 778 successful descriptor readings in 785 observation attempts, with seven separately recorded owned-exit observations; Linux recorded 9,459 successful readings in 9,459 attempts. Successful-reading spans covered 57.3–99.6% of macOS launch-to-completion intervals and 98.7–99.97% on Linux. These spans and sampled descriptor/disk maxima are observational lower bounds: they do not prove exhaustive coverage between samples or at the interval edges. RSS comes from the kernel's peak resource usage. An interrupted macOS exit-observer syscall is retried only for `EINTR`, with bounded count, cause and first/last timestamps; this final run recorded zero such retries. Live read errors, missing tools, unknown observer errors and scratch-inspection errors invalidate acceptance. Earlier failed campaigns remain failure evidence, rather than contributing accepted figures.

Independent verification took 15.16–16.31 s for the macOS long-text cases and 17.32–17.63 s on Linux, after builds completed. It checks every complete normalized text page. Heavy-case appearance checks compare full rendered source/output pages from the first, middle and last image-bearing sources; marker-only and image-free substitutes are negative controls. The per-case JSON also records fixture time, plan/input/output bytes, object counts, scratch peaks, successful samples, total attempts and terminal causes.

Earlier exploratory measurements are retained separately for context and have not been rerun against the final tree:

| Earlier probe on macOS | Recorded observation |
| --- | --- |
| `check` of 5,000 sources + 5,000 generated pages | 0.6 s / 71 MiB |
| 100,000 contributions (the plan limit) | 2.1 s / 566 MiB |
| One blank with count 1,000,000 (the generated-page limit) | about 20 s / about 3.2 GiB |
| 3,000 distinct generated texts, earlier buffered build | `check` 42 s / 227 MiB; `build` 105 s / 1.7 GiB, on a busy machine |

Those earlier profiles showed growing garbage-collection pressure at multi-gigabyte heaps; their relative phase shares and memory estimates are not claims about the final build. Generated text now shapes and writes each distinct page sequentially, retaining report bounds/findings and shared font/glyph mappings. Source PDF pooling remains proportional to imported bytes. Sources carrying annotations, forms or destinations are imported once per occurrence to preserve their page-local relationships.

## Choosing the plan format

Plans are strict JSON.

| Candidate | Verdict |
| --- | --- |
| **JSON** | Chosen. The job is an ordered, heterogeneous array, often generated at scale; JSON arrays, strings and typed objects, and every language and shell (`jq`, PowerShell `ConvertTo-Json`, Python, Go) generate it correctly; JSON Schema gives editors completion and validation; the standard library's `encoding/json/jsontext` token stream gives a strict, single-pass, located decoder with no dependency to ship, license or audit. Weakness: no comments, which is an acceptable trade for a *description* whose file names carry meaning. |
| TOML | Can express a heterogeneous array of tables, so it is not incapable; a large generated mixed sequence is still clearer and more interoperable as JSON, and it needs a third-party parser and a second format to maintain. |
| YAML | Comments and terse lists, but implicit typing (`no`, `1e3`, `0755`, unquoted dates) is a hazard for file names and text, indentation errors are easy to make in generated output, and it needs a third-party parser. |
| Line-oriented text | Cannot carry per-blank text and style. |
| A DSL | A new language to document, parse, and get wrong. |

The plan is the way to express anything; direct operands and `--plan-json` express small jobs without a file.

### Why a strict, custom decoder

Struct decoding in a general JSON library accepts duplicate members, ignores case, replaces invalid Unicode, and silently tolerates trailing data, and it cannot say where in the file a value came from. A plan is an instruction, so the decoder rejects every one of those with a located diagnostic and keeps the position of every item through flattening, resolution, inspection, reporting and failure. The JSON Schema states what a schema can state; duplicate members, Unicode rules and unit-converted bounds are parser rules, and the documents say which is which.

### Why items are strings or objects

A PDF is by far the commonest item, so a bare string is a PDF path: a thousand-file plan is a thousand short lines. Blanks and groups are objects because they carry data. A string is never interpreted (no `"--blank"` sentinel inside a path list), so no file name can collide with a directive.

### Why groups have a `dir`

"The structure of directories" in a plan is the repeated prefix of paths. A group's `dir` removes the repetition without scanning anything: the plan still lists exactly the files that are assembled.

### Why no globbing or directory scanning

Both put an ordering policy (lexical? natural? locale? case-folded?) where nobody can see it, and the answer differs between Windows and Unix. A plan with explicit paths is reviewable (`check`), diffable, and identical on every platform. The cost, generating the list, is a short script that states its ordering; see [`PLAN.md`](PLAN.md#generating-plans) and [`CLI.md`](CLI.md).

## `--blank`, `@`, and other spellings

`--blank` stays. It is inert in every shell: `bash`, `zsh`, `fish`, `cmd.exe`, and PowerShell all pass it through untouched. Its one cost is that it shares the option namespace, so a file literally named `--blank` needs `--` or `./--blank`; the `--` escape is standard and cheap.

An `@blank` token was considered and rejected. `@` is meaningful to PowerShell (splatting: `@name`, array `@()`), and by long convention (`javac`, `gcc`, MSBuild) `@file` means "read arguments from this response file", a different thing from a directive. Both are traps for the people and agents most likely to generate command lines.

The rule that makes the grammar unambiguous: a separate option value may never be a recognized option, so `-o --blank` fails loudly instead of naming an output file `--blank`; values that legitimately start with `-` use `--option=value`.

## Blank appearance

The settings and their behavior are specified in [`BLANK_PAGES.md`](BLANK_PAGES.md). The reasoning:

- **Anchor plus offset is the only placement syntax.** Nine anchors with an offset express every position, absolute lower-left coordinates included, so there is one way to say it rather than two.
- **Offsets use the PDF convention** (`+x` right, `+y` up), so one coordinate system serves the plan and the page.
- **Size inherits from the neighbor by default**, so a blank after a landscape or rotated page looks like it belongs there. Inheritance comes only from source pages and uses the visible page rectangle (CropBox within MediaBox, rotation and UserUnit applied).
- **Every setting is either set or unset**, omission inherits and `null` is an error, so there is no ambiguous reset; `"none"` clears a background.
- **Text never silently changes.** The default overflow policy is an error: text that does not fit is rejected with the location of its effective text declaration, never shrunk, clipped or truncated; `"overflow": "allow"` is explicit and visible in the report.

### Why an embedded Unicode font

A generated page must be able to say what the documents around it say. A standard PDF font restricts text to one Western repertoire, and a font a viewer supplies cannot be measured exactly. PDFConcat embeds Noto Sans Regular (OFL 1.1) as the default and lets a plan name a TrueType file instead, so the same explicit font-resource path serves both. The font was chosen by proof, not by reputation: Go's own font lacks the combining marks (U+0301, U+0304, U+0327) needed for decomposed Latvian, so its precomposed coverage does not meet the contract. Liberation Sans and DejaVu Sans were inspected as well and also cover the minimum corpus, so the choice among passing candidates is a judgment: Noto Sans Regular is a maintained family under the SIL Open Font License, with the required coverage and mark positioning verified in its pinned unhinted static TrueType build (identified by SHA-256 in [`THIRD_PARTY_NOTICES.md`](../THIRD_PARTY_NOTICES.md)). Text is shaped with the font's own metrics and mark positioning, extracted through `/ToUnicode` and `/ActualText`, checked against an independent renderer and extractor, and never normalized to hide a missing mark. A sequence the font cannot shape into available glyphs is an error naming the character, never a box; font composition may supply a combined glyph without changing the original extracted sequence; scripts needing complex shaping or right-to-left layout are rejected early rather than set incorrectly. Nothing depends on fonts installed on the machine, and the standalone executable stays offline.

### Why blanks are generated as one resource document

Every distinct generated appearance becomes one page of a single resource PDF, with each font embedded once. A sequential producer shapes and writes that page immediately, avoiding retention of every distinct text's shaped lines. Assembly imports that document once, so thousands of distinct blanks cost thousands of small pages sharing one font, not thousands of font copies merged one file at a time. The generated pages are verified by the same validator as every output, with independent rendering/extraction tests. Overflow considers both the advance-based anchoring block and the separately positioned glyph ink, including bearings and combining-mark offsets; rejected overflow retains its computed geometry.

## Failure philosophy

PDFConcat prefers explicit failure over speculative repair.

It does not:

- enable backend network activity for this assembly workflow;
- create synthetic merge bookmarks as an undocumented side effect;
- normalize every PDF through a second renderer;
- silently switch backend;
- repair malformed inputs with Ghostscript;
- modify originals; or
- publish an output that has not passed final validation and page-count verification.

A backend incompatibility is an actionable error, not permission to transform source material in an undocumented way.

### Why publication is ordered as it is

A PDF and a report cannot be published in one filesystem transaction, so the order is chosen so that nothing is ever misdescribed. The complete report is staged beside its own target before the PDF is committed, which catches most report I/O problems while nothing is yet visible; the PDF is published; then the report. If only the report then fails, the staged report is kept as one recovery file and the result says `"published": true` with a rebuild-free instruction, because a published PDF is never rolled back and must never be described as untouched. The same truthfulness rule governs a signal after publication (the outcome is what was committed) and a closed standard output (what was committed goes to standard error).

A failure report is not a license to overwrite. Until every plan, font, source, output and report file is known, a malformed job might name an existing file as an input it never got to read, so the report is created only at a new path with the platform's no-clobber primitive; `--overwrite` governs it exactly as it governs the PDF only after that inventory is complete.

## Decisions and rejected alternatives

Each of these was weighed against a concrete counterexample. The decision stands until evidence overturns it.

| Alternative | Decision |
| --- | --- |
| Global styling flags (`--blank-text`, `--blank-size`, and the rest) | Removed. Styling lives only in the JSON plan; direct operands cannot style a page. One place to say each thing, one precedence rule (per item, then plan defaults, then built-ins). |
| A mutable "current blank style" on the command line, `--set` overrides, a line-oriented DSL, or a configuration file layered over the job | Rejected. Each is a second language and a precedence rule; `--plan-json` carries any per-page appearance as one argument. |
| TOML, YAML, JSON5, or a custom DSL for plans | Rejected; see [Choosing the plan format](#choosing-the-plan-format). |
| Implicit directory discovery, globbing, locale-dependent ordering | Rejected; the plan lists exactly what is assembled. |
| A persistent service, server, cache, or session for agents | Rejected. A deterministic offline executable composes with agents and shells; saved reports provide cheap follow-up queries without state. |
| Freshness by metadata only (size and modification time of the path at assembly time) | Rejected. Validating one file and assembling another by pathname leaves a window in which the validated bytes are not the assembled bytes. Sources are copied once into a private workspace with a digest, and the copy is inspected and assembled. The cost is scratch disk and one copy of each distinct source. |
| One input file per blank (merging a generated PDF per blank, in order) | Rejected. Each blank would carry its own font graph, and a merge that rewrites the growing result per blank is quadratic. All generated pages come from one resource document with fonts embedded once. |
| pdfcpu `Collect` for ordered, repeated page selection | Rejected after adoption probes failed, in favor of reordering the merged pool's page tree; see below. |
| Generic annotation-graph cloning to make repeated pages independent | Rejected. A source with page-local objects is imported once per occurrence instead, which is simpler and correct, and a partial page range of such a source is an error. |
| A language, framework, or PDF-engine rewrite | Not undertaken; no measurement showed a concrete benefit that the present design could not reach. The engine sits behind one package boundary, so a replacement stays possible without a backend selector. |
| Go's own font as the default | Rejected by proof, not preference; see [Why an embedded Unicode font](#why-an-embedded-unicode-font). |

### Why the pool is reordered instead of collected

The assembly primitive was chosen by testing, not by reputation. pdfcpu's public page-collection call accepts ordered repeated page numbers and shares immutable resources, which looked like exactly the right primitive. Adoption probes with real linked, form-bearing and cropped fixtures showed that it does not preserve what an assembly must preserve. For repeated pages of a linked source it left link destinations pointing at page copies that are not in the output, it shared annotation arrays between copies so that `/P` back-references named the first copy, it did not carry an inherited CropBox onto the pages, it started every result at PDF 1.7, and in a scale spike its cost grew faster than linearly. The design therefore imports every document into one pool with pdfcpu's exported merge primitive and replaces the pool's page tree with a flat tree in the final order: page objects keep their identity, so links, destinations and back-references stay consistent, and fonts, images and content streams stay shared. Optional content, document output-color configuration and dynamic XFA forms are rejected before assembly because the pool cannot safely reconcile their rendering controls across source documents. The raw catalog is checked before validation repairs can remove unsupported state, and the validated graph is checked again. Removing a source catalog entry must never expose hidden marks or silently change color intent. Other omissions are explicitly described in the architecture policy. The repository keeps the collection check as a permanent negative control: `internal/pdfengine/policy_test.go` collects repeated linked pages with the public call and requires the independent oracle (qpdf and Poppler, sharing no code with pdfcpu) to flag the result as corrupt.

## Unsupported by design

PDFConcat has no compatibility mode for earlier conventions, and none is planned:

- `BLK` in filenames, `_blank.pdf`, and timestamped `_concat_*` outputs;
- implicit alphabetical directory discovery;
- a line-oriented plan file with `--blank` as a line;
- the `--blank-*` option family, `--dry-run`, `--json` and `--print-schema`;
- Ghostscript fallback or normalization options;
- a GUI, a backend selector, a plugin system, OCR, or a service or daemon; and
- shell-function flags.

The plan format carries `"version": 1` so that a future incompatible change is explicit rather than silent.
