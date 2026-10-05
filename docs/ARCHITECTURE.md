# Architecture

## System view

```text
argv ──► internal/cli ──► cli.Request ──┐
                                        ▼
plan file / stdin ──► internal/plan ──► internal/app ──► internal/publish ──► verified output
        (JSON)         plan.Document     orchestration
                                        │   ▲
                       assembly.Sequence│   │ assembly.DocumentInfo / Layout
                                        ▼   │
                                  internal/assembly      (domain model, no I/O)
                                        │
                                        ▼
                                 internal/pdfengine ──► pdfcpu API (stateless, offline)
```

`internal/cli` parses argv only. `internal/app` resolves a request operationally — it reads the plan, applies precedence, resolves paths — and delegates the plan *format* to `internal/plan`. A malformed or unreadable plan is therefore an operational failure (exit 1), not a command-line usage error.

## Package responsibilities

### `cmd/pdfconcat`

Executable wiring only: signal-aware root context, build metadata populated by release ldflags, process streams, mapping usage versus operational failures to exit codes. It is the only place that touches `os.Stdout`, `os.Stderr`, or `os.Stdin`. Business rules do not belong here.

### `internal/cli`

Owns the public command grammar ([`CLI.md`](CLI.md), [`BLANK_PAGES.md`](BLANK_PAGES.md)): the ordered sequence directives, the options and their value rules, help and version text. It performs no filesystem I/O and returns a typed `Request` (or a terminal action). Malformed command lines are `*UsageError`.

### `internal/plan`

Owns the plan-file format ([`PLAN.md`](PLAN.md)): strict JSON decoding with line/column and JSON-path errors, version check, flattening of directory groups, path resolution against the plan's directory, and the embedded JSON Schema. It converts the file's strings into `internal/assembly` values and exposes only `Decode`, `Document`, and `Schema`.

### `internal/assembly`

Owns backend-independent domain semantics, with no I/O:

- `Sequence` of `PDF(path)` and `Blank(style, count)` items;
- the layered, partial `BlankStyle` (an `Option[T]` per field) and its resolution to a comparable `BlankSpec`;
- value types and their parsers: `Length`, `Color`, `PageSize`, `Font`, `Anchor`, `TextAlign`; and
- `BuildLayout`, which turns a `Sequence` plus per-document `DocumentInfo` into an ordered list of `Part`s with output page ranges, resolving each blank's inherited page size and style.

### `internal/pdfengine`

The only package that imports pdfcpu. The `PDFCPU` type provides:

```text
Inspect        validate one PDF; return page count and first/last displayed page size
ValidateBlank  report whether a blank can be rendered (character repertoire)
Assemble       render each distinct blank once; merge sources and blanks in one pass
```

The blank-page generator (`blank_text.go`, `blank_pdf.go`, `winansi.go`) lays out wrapped text with the standard fonts' metrics and serializes a one-page PDF. pdfcpu-specific types do not escape the package. The abstraction exists to contain a dependency, not to promise pluggable backends; the consumer-side interface lives in `internal/app`.

### `internal/publish`

Owns destination policy and final file replacement. The assembled PDF is staged inside a temporary directory located in the destination directory, keeping publication on the same filesystem. Existing symbolic-link destinations are refused.

Without `--overwrite`, publication uses native create-if-absent rename semantics so a destination created after preflight is not silently replaced:

- Linux: `renameat2(..., RENAME_NOREPLACE)`;
- macOS: `renameatx_np(..., RENAME_EXCL)`; and
- Windows: `MoveFileExW` without `MOVEFILE_REPLACE_EXISTING`.

With `--overwrite`, Linux and macOS use same-filesystem `os.Rename`; Windows uses `MoveFileExW` with `MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH`. Replacement is attempted only after output verification. If a platform/filesystem cannot provide the selected native primitive, PDFConcat fails rather than weakening the no-clobber contract.

### `internal/app`

Coordinates the use case:

```text
resolve request  (argv sequence, or plan file/stdin; -o over plan output;
                  --blank-* over plan blank defaults)
  → reject output-as-input / output-as-plan; check destination policy
  → validate every blank style (resolve + renderability), before reading any PDF
  → inspect all distinct PDFs concurrently (collect every failure)
  → build layout (page ranges, inherited sizes, totals)
  → [dry-run: report and stop]
  → create destination-local workspace
  → assemble (render distinct blanks once; single merge pass)
  → inspect the result: validity + page count == layout total
  → publish the verified staged file; remove the workspace
  → report (text or JSON)
```

### Test-support packages

- `internal/pdffixture` creates small synthetic PDFs for tests.
- `internal/repopolicy` holds tests for repository-wide rules: no inline lint exceptions, and every entry in the lint-exception registry is justified.

## Dependency rules

Enforced by `depguard` in `.golangci.yml`:

- pdfcpu is imported only by `internal/pdfengine`;
- `golang.org/x/sys` only by `internal/publish`; and
- no CLI framework, `pkg/errors`, or `testify`.

## One-pass assembly

Blanks are never inserted into an already-merged file; that would rewrite the whole file once per blank. `BuildLayout` produces the final order directly:

```text
A(2 pages) --blank B(3 pages) --blank --blank C(1 page)
        │
        ▼
Part: A        pages 1-2
Part: blank    page  3        (size of A's last page)
Part: B        pages 4-6
Part: blank    pages 7-8      (count 2; size of B's last page)
Part: C        page  9
```

`Assemble` renders each distinct blank spec once, then hands pdfcpu one list — `A, blank, B, blank, blank, C` — to merge in a single pass. The order is final from the start, so there is no boundary arithmetic and no dependence on insertion order.

Size inheritance is specified in [`BLANK_PAGES.md`](BLANK_PAGES.md#page-size); `BuildLayout` implements it.

## Stateless pdfcpu use

The adapter loads pdfcpu with `ConfigurationModeStateless`, explicitly selects relaxed validation, forces offline operation, and disables automatic merge-bookmark creation. PDFConcat therefore does not initialize, read, or write a user pdfcpu configuration directory, initiate backend network activity, or create bookmarks merely as a side effect of concatenation.

## Verification invariant

Publication requires both:

1. the staged assembled PDF passes pdfcpu validation; and
2. `actual pages == sum(source pages) + sum(blank counts)`.

A mismatch is a hard failure and the requested output is not published.

## Concurrency

Only source inspection is concurrent: a worker per CPU, each with its own pdfcpu configuration clone, results stored by index so error order is deterministic. Assembly is a single sequential pass. The race detector runs in CI.

## Dependency policy

Runtime dependencies stay minimal. Direct dependencies:

- `github.com/pdfcpu/pdfcpu v0.16.0` — PDF parsing, validation, merging, and the standard-font metrics used to lay out blank-page text;
- `golang.org/x/sys v0.48.0` — native publication primitives on Linux, macOS, and Windows.

Do not introduce a framework when a small standard-library implementation is clearer. In particular, the ordered `--blank` grammar is intentionally parsed without Cobra, and the plan format is JSON precisely because the standard library reads it.
