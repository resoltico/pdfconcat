# Architecture

## System view

```text
argv ─► internal/cli ─► cli.Command ─────────────────────────────┐
                                                                  ▼
plan file / stdin / inline JSON ─► internal/plan ─► assembly.Job ─► internal/app
        (one strict pass, with provenance)                          │
                                                                    ├─► internal/assembly   flatten, resolve geometry and
                                                                    │                       page ranges (domain, no I/O)
                                                                    ├─► internal/capture    private workspace, source and font
                                                                    │                       snapshots, file-role registry
                                                                    ├─► internal/typeset ─► internal/layout   fonts, shaping,
                                                                    │                       wrapping, overflow
                                                                    ├─► internal/genpage    the generated-pages resource PDF
                                                                    ├─► internal/pdfengine ─► pdfcpu   inspect, assemble, verify
                                                                    ├─► internal/report     report model, summary, queries
                                                                    └─► internal/publish    staging, atomic publication
```

`cmd/pdfconcat` connects the process to `internal/app`: signals, streams, the working directory, build metadata. Everything else is a library package with no process-global state.

## Package responsibilities

### `cmd/pdfconcat`

Executable wiring only: a signal-aware root context (the first interrupt or termination signal cancels it, and restores the default behavior so a second one ends a slow command), `SIGPIPE` ignored so a closed standard output surfaces as a write error the command can report, the working directory, release metadata (the version set at link time with `-X main.version`, else the embedded project version from `internal/app/version.txt`; the commit and its date from the build information the Go toolchain embeds), and detection of an interactive standard error with `golang.org/x/term`. It exits with the code `internal/app` returns. It is the only package that touches `os.Stdin`, `os.Stdout`, `os.Stderr`, or the process environment.

### `internal/cli`

The command grammar ([`CLI.md`](CLI.md)): `build`, `check`, `report`, `schema`, `version`, `help`, their options and value rules, and the structured help. It performs no filesystem access. `Parse` returns a typed `Command` or a `*UsageError` whose diagnostics share the stage/code/location/recovery vocabulary with job failures and which already knows whether the error is rendered as JSON or text.

### `internal/plan`

The plan format ([`PLAN.md`](PLAN.md)): strict decoding of one JSON object from the `encoding/json/jsontext` token stream in a single pass, with located diagnostics, the declared resource limits charged while reading, a cancellable read of blocking input, and the embedded JSON Schema. It produces an `assembly.Job` whose every node remembers where it was written.

### `internal/assembly`

Backend-independent domain semantics, with no I/O: units, colors, page sizes, the layered `BlankStyle`, the `Job` tree that plans and command-line operands both compile to, `Flatten` (groups expanded once, paths resolved, distinct sources, fonts and appearances), and `Flattened.Resolve` (page geometry, inherited sizes, the distinct generated pages, output page ranges and compact runs, with checked arithmetic). Diagnostics carry stage, stable code, exact effective declaration location, and complete affected-consumer references. Flatten retains references to the immutable job declaration layers instead of copying every field origin per contribution; callers keep those declarations unchanged for the run.

### `internal/capture`

The private job workspace and everything that must not be done twice or unsafely: a single-pass copy of each distinct source or font file with a content digest, opened without blocking and checked for being a regular file on the open handle, cancellable, with detection of observed changes; and the file-role registry, which rejects a file that would be used in two roles (plan, source, font, output, report) by filesystem identity, so hard links, symbolic links and case aliases collide.

### `internal/typeset` and `internal/layout`

`typeset` loads and validates TrueType fonts (content identity, embedding permissions), shapes and wraps text with the font's own metrics and positions one text block on a page; it knows nothing about PDF. `layout` joins it to the resolved pages: it checks geometry-independent script and glyph availability before source inspection, then places each distinct generated page and reports overflow at its effective declaration. Block geometry and actual glyph-ink bounds remain available on overflow rejection; unknown geometry is null. Placing text also reads the outline of every glyph it uses, once per glyph and shaper, so a font whose outline data is damaged fails early with an error that names the font, the character and the glyph (`font_invalid`) instead of rendering that text blank.

### `internal/genpage`

Writes the generated-pages resource document: one PDF with one page per distinct generated appearance, each font embedded once and shared, with text extraction through `/ToUnicode` and `/ActualText`. Its sequential producer shapes a page and writes it immediately, retaining only job-wide font/glyph mappings and report geometry.

### `internal/pdfengine`

The only package that imports pdfcpu. The `Engine` type provides:

```text
Inspect   read one captured source once: pages, effective version, page-local objects, form,
          destinations, material feature observations, and the visible size of the first and last pages
Assemble  import the generated-pages document and the sources into one pool, reorder the pool's
          page tree to the compiled order, write once, then validate and count the output
```

`Assemble` takes its own request types (`SourceFile`, `ResourceDocument`, compact `Run`s) and returns `*Error` values with stable codes; it imports neither `internal/assembly` nor `internal/plan`. A pure backend policy validator checks inspected source facts, compact order, distinct generated-page count and expected total for both `check` and `build`, before generated rendering. The assembler calls the same validator defensively and separately checks the actual resource and destination. The generated pages are rendered elsewhere into one resource PDF that the engine only reads. pdfcpu-specific types do not escape the package. The abstraction exists to contain a dependency, not to promise pluggable backends; the consumer-side interface lives in `internal/app`.

### `internal/report`

The report model and everything that reads or renders it: the builder, the compact summary, the complete report, structured response union and their schemas, strict decoding of a saved report as untrusted input (byte, nesting and node limits enforced before any large structure exists), and the `--part`, `--page` and `--view` queries. Reading a report never involves a PDF. Diagnostics carry mandatory error/warning severity in format 3. Captured record counts and predicted/committed warning context come from the same report authority; part/page previews refer to relevant records and findings. Query operation, saved-run outcome and selection counts are separate; navigation binds the currently invoking executable to the actual queried file, never to a historical publication target. Generic domain-location conversion lives here; plan-specific failure adaptation belongs to `app`.

### `internal/publish`

Destination policy and final file replacement. A file is staged beside its target, flushed, and made visible by one native rename; without `--overwrite` the rename is the platform's no-clobber primitive, so a destination created after the last check is not replaced:

- Linux: `renameat2(..., RENAME_NOREPLACE)`;
- macOS: `renameatx_np(..., RENAME_EXCL)`; and
- Windows: `MoveFileExW` without `MOVEFILE_REPLACE_EXISTING`.

With `--overwrite`, Linux and macOS use same-filesystem `rename`; Windows uses `SetFileInformationByHandle(FileRenameInfoEx)` with replacement and POSIX semantics so retained identity handles remain valid. Symbolic-link and non-regular destinations are refused. If a platform or filesystem cannot provide the selected primitive, PDFConcat fails rather than weakening the no-clobber contract. `Commit` publishes the PDF and then the report, and keeps a recovery copy of the report if only the report fails.

### `internal/app`

The use case. It owns the pipeline below, the decision of each command's outcome, and what is printed. Its only interface is the consumer-side `Engine` (inspect, assemble), which exists so orchestration can be tested with injected engine failures, verification failures, disk-full errors and cancellation at each stage; everything else it uses is the real package.

## The pipeline

A `build` or `check` runs these stages in order. A stage that fails reports every problem it found, in input order, and ends the run; later stages do not run, and the report says exactly how far the run got.

```text
decode the job once                      file, stdin, inline JSON, or operands; cancellable read
  → flatten                              resolve paths and appearance values, keep every origin
  → check destinations and aliases       output policy; plan, font, source, output and report are
                                         registered, so no file serves two roles
  → capture and load fonts               once per distinct file, before any PDF is read
  → capture and inspect sources          copy each distinct source into the private workspace, then
                                         inspect raw signature/rendering/form policy and material feature facts before repair; at most --jobs workers, never more than
                                         there are sources
  → resolve                              page geometry, inherited sizes, ranges, checked totals
  → validate backend policy              source/range and occurrence rules, generated references,
                                         total-page cap and arithmetic consistency; no assembly I/O
  → place text                           check: retain geometry; build: shape/write each distinct resource page
                                         retain bounds/findings; reject overflow unless explicitly allowed
  → check:  report                       describe the layout, publish the requested report
  → build:  merge                        use the already produced resource; one pool assembly into a staged
                                         file, verified (valid, page count) by the engine
          → stage the report             beside its own target, before the PDF is committed
          → publish                      recheck both destinations and cancellation, publish the
                                         PDF, then the report; remove the workspace
```

The four phases of a report (`instructions`, `input_inspection`, `layout`, `output_verification`) are `not_run`, `incomplete` or `complete`, so a failed run claims no count or range it does not know: those are null, never zero.

**Private snapshots.** The bytes that are inspected are exactly the bytes that are assembled: every distinct source is copied once, with its digest computed during the copy, and later stages read only the copy. Capture is not a filesystem-wide atomic snapshot. Each file is checked to be a regular file on the open handle (opened without blocking, so a FIFO or device substituted after a check cannot hang the process) and the open handle's size and modification time are compared before and after the copy, and with the number of bytes read, so an ordinary change during the copy is reported as `source_changed`. A concurrent writer that edits a source in place between those observations, or a hostile change of a parent directory, is outside the supported contract and can change which bytes are assembled; sources must be stable while a run is in progress. Two files copied one after the other can also reflect different moments. `check` and `build` each capture afresh, so a passing `check` says nothing about files edited afterwards. A build's workspace is created beside the destination so staged files publish by rename on one filesystem; a check uses the system temporary directory. The workspace is removed on every exit path, and a cleanup failure is a warning on standard error naming the directory, because the outcome it cannot change has already been published or reported.

**Outcomes.** Every command ends in one status: `ok` (exit 0), `invalid` for invalid instructions including malformed plans (2), `failed` for I/O, backend and publication failures (1), or `interrupted` (130). Standard output carries the compact summary, the complete report with `--details`, or the same content as text.

**Reports.** `--report FILE` streams the complete report directly into an owned, target-local staged file under the report byte/node limits; it does not retain an additional serialized copy in memory. A failure report is also written there. Until every plan, font, source, output and report file is known and checked, a failure report is created only at a new path with the platform's no-clobber primitive, whatever `--overwrite` says: a malformed job might name the existing file as an input it never got to read. If saving the failure report fails, that failure is added as a secondary diagnostic and the primary diagnostics and exit status are kept.

**Publication and committed state.** Complete reports serialize directly into target-local owned private staging files; byte/node limits, cancellation, writer, close and flush failures are checked before PDF commit. Failed partial stages are removed only while their identity remains owned. Interrupted failure reports deliberately outlive the canceled job context; post-PDF report/recovery completion retains committed state. The report is staged first, describing the state the run ends in once it is published; the PDF is published; then the report. If the PDF was published and the report was not, the staged report is kept as one private recovery file, the result says `"published": true`, the report status `failed`, the recovery path, and a rebuild-free instruction, and the exit status is 1. Nothing rolls a published PDF back. If standard output cannot be written after work was committed, a bounded JSON record of what was published goes to standard error. Atomic visibility is not crash durability. Visibility: one native rename makes a file appear, so a reader sees the old file or the complete new one, never a partial file. Durability: the staged file is fsynced before the rename, and on Linux and macOS the parent directory is fsynced after it (macOS `fsync` in Go issues `F_FULLFSYNC`); Windows flushes file contents but does not establish directory-entry crash durability, and a filesystem that cannot flush a directory is tolerated. If a directory flush or owned rename-handle release fails after the rename, the file remains published. The successful published result is preserved and a warning goes to standard error; a report-publication failure remains a separate failed outcome. Whether a published file survives a power loss is decided by the operating system and the storage hardware; nothing stronger than the platform's flush semantics is promised.

**Cancellation.** The context is honored in the blocked read of standard input, during capture (between copy chunks), inspection, text layout, assembly (the engine checks between imports and before writing), and immediately before publication. A canceled run never replaces the destination and removes its scratch files. A signal that arrives after the PDF is visible does not change the outcome: the result describes what was committed.

**Progress.** Only when standard error is an interactive terminal (`golang.org/x/term`, which works on all three operating systems; a character device is not enough), one self-overwriting line shows the stage (`prepare`, `inspect`, `render`, `merge`, `publish`) and a counter for source inspection. Redraws are limited in rate and in number, nothing is written to standard output, and a write failure disables progress without failing the command. The engine verifies the written file as part of its assembly step, so `merge` includes verification.

## Test-support packages

- `internal/pdffixture` writes small deterministic PDFs (page markers, links, forms, named destinations, boxes, rotation, UserUnit, PDF versions) for tests and benchmarks.
- `internal/pdforacle` judges PDF output with qpdf and Poppler only, sharing no code with pdfcpu or the engine: page order and text, link and destination identity per occurrence, form membership, geometry, version. Tests that need these tools skip when they are missing, and fail when `PDFCONCAT_REQUIRE_QA_TOOLS=1`.
- `internal/exectest` builds the executable for tests that run it as a child process, instrumented when the coverage gate asks, so subprocess execution is part of merged coverage.
- `test/scale` holds the reproducible scale acceptance harness (`PDFCONCAT_SCALE=1`).
- `internal/doccontract` holds tests that keep the contract documents in agreement with the code: documented options, commands, usage codes and exit statuses against the parser, every `json` example against the plan decoder, every `console` example against the command parser, and every Python recipe against the Python compiler.
- `internal/repopolicy` holds tests for repository-wide rules: no inline lint exceptions, and every entry in the lint-exception registry is justified.

## Dependency rules

[`.golangci.yml`](../.golangci.yml) is the authoritative internal import and external-library policy. The pinned depguard applies production consumer rules with explicit exact-package allowances in the same rule as the project-prefix denial; one rule cannot override another rule's denial. Package responsibilities above explain the direction without maintaining a second edge table.

The computational domain and typesetting core do not own filesystem or process state. Parsing, layout, PDF generation, report transport and publication retain their separate responsibilities; `app` coordinates them and adapts plan decoding failures into report diagnostics. The backend and publication deliberately share `capture` for checked filesystem behavior. The quality gate shares its checked-file boundary to stream retained mutation execution evidence. Executable wiring owns process streams, signals and terminal detection, passing explicit resources and state to the application.

`go run ./tools/qualitygate architecture` derives production classification from that configuration and checks every owned production file against the compiled-file union of all six supported OS/architecture targets. It rejects unclassified or ambiguously classified files, unsupported build-tag gaps, empty discovery and package-loading errors. This is static coverage of policy and target variants; it does not establish native execution. Native lint jobs analyze each operating system's compiled code.

Analysis uses the reviewed physical-source loader patch to the pinned linter: compiler `//line` directives remain legal and unchanged, while import selectors and diagnostic predicates use actual source files and physical positions. The source installer verifies the upstream module archive and checksum-database identity, applies the pinned patch without changing its dependency files, tests the parser and builds with a distinct patch-derived version/cache identity. The patch is development tooling, separately licensed from the runtime; [`CONTRIBUTING.md`](../CONTRIBUTING.md) describes prerequisites and source verification. Source-epoch metadata identifies the upstream source, not the build time.

Tests and support packages have deliberate policies that permit independent fixture/oracle imports while production rules reject runtime imports of test helpers. [`.quality-exceptions.yml`](../.quality-exceptions.yml) is the only exception authority, including the narrow italic-font and native FIFO test controls. New dependencies or responsibility changes update the policy, affected callers and real rejection controls together.

## One-pass assembly

Blanks are never inserted into an already-merged file; that would rewrite the whole file once per blank. Resolution produces the final order directly, as compact runs:

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

`Assemble` imports the generated-pages document and each source into one pool, compiles the final order into a flat page tree, and writes the file once; see [PDF backend behavior](#pdf-backend-behavior). The order is final from the start, so there is no boundary arithmetic and no dependence on insertion order. A blank of a million pages is one run. Size inheritance is specified in [`BLANK_PAGES.md`](BLANK_PAGES.md#page-size) and implemented by `Flattened.Resolve`.

## PDF backend behavior

`internal/pdfengine` is documented in full in its package comment; this section states what a user can rely on. The contract below requires tests at the independent PDF boundary, including qpdf object inspection, Poppler rendering and ordinary form refilling. Syntax/page counts alone cannot establish appearance or editability.

**Method.** Every document is merged into one pool with pdfcpu's exported merge primitive, then the pool's page tree is replaced by one flat node holding the final order. Page objects keep their identity, so links, destinations and `/P` back-references stay consistent, while fonts, images and content streams stay shared. pdfcpu's `Collect` was evaluated and rejected: it leaves destinations of repeated linked pages pointing at orphan page copies. The pool and the written file are held in memory; there is no streaming mode, and memory grows with the size of the imported documents and about a page dictionary per output page. The engine benchmarks (`go test -bench` in `internal/pdfengine`) and `test/scale` measure wall time, peak memory and descriptors; the numbers are measurements, not guaranteed bounds.

**Page-local objects and repeats.** A source with annotations, page actions, article beads, a form or destinations is imported again for every use, so each occurrence owns its annotations, widgets and destinations. Such a source must be used whole each time; a partial page range of it is rejected, because links, fields or destinations would point at dropped pages. At most one source occurrence with a legacy catalog `/Dests` dictionary is supported per output, including occurrences of different files with distinct keys. This conservative policy avoids merging dictionaries whose keys can overwrite references; both `check` and `build` reject the second occurrence. Other sources are imported once and repeated pages share all objects but the page dictionary.

| Feature | Behavior in the output |
| --- | --- |
| Ordinary link annotations and GoTo/URI actions | Kept; occurrence-local page destinations follow their own imported pages |
| Page, annotation and widget actions | Retained, including reachable executable actions; material retained actions warn. The assembler does not execute them or establish their behavior in a viewer. |
| Ordinary static forms (AcroForm) | Kept with supported current appearance and ordinary field editing. Conflicting default-resource bindings are scoped per imported occurrence; effective default appearance/justification follows the field hierarchy. Widget `/AP` and page resources remain independent. Equal field names stay separate under qualified names; retained scripts are not rewritten or validated against these names. Unsupported semantic forms reject during shared inspection/import. |
| Named destinations | `/Names /Dests` kept and merged; a later occurrence's colliding name is renamed and its links follow |
| Outlines (bookmarks) | Dropped; nonempty observed outlines warn |
| Document information, XMP metadata, page labels, catalog open/additional actions, viewer preferences, permissions | Dropped. The output has a new `/Info` with pdfcpu as producer and the current date, so output is not byte-reproducible. Material page-label/action removal warns. |
| Optional content (`OCProperties`, OCG/OCMD or `/OC` references), document output-color configuration (`OutputIntents`), dynamic forms (`AcroForm/XFA`, `NeedsRendering: true`) | Rejected during inspection and import (`pdf_rendering_unsupported`): the pool cannot reconcile these rendering controls safely. Raw catalog checks precede validator repairs that can delete XFA-only form state. A false `NeedsRendering` flag and ordinary static AcroForm widgets remain supported; a malformed flag is rejected. Existing destinations remain untouched. |
| Tagged-PDF structure | Dropped; material structure removal warns and the output is not tagged |
| Non-destination catalog name trees, including EmbeddedFiles and JavaScript | Dropped; removal of material attachment indices/actions warns |
| Catalog associated-file (`/AF`) indices | Dropped; actual embedded-payload relationships warn, independently of EmbeddedFiles name trees |
| Page-local FileAttachment payloads | Kept when reachable through retained annotations, even if the same payload also had a removed catalog index. Payload retention is distinct from index removal; material retention warns. |
| Encryption | Encrypted sources are rejected, including files that open with an empty password; output is never encrypted |
| PDF version | PDF 2.0 if any input is PDF 2.0, otherwise PDF 1.7; a 2.0 source may appear anywhere in the order |
| Actual signatures, certification/usage-rights signatures and document timestamps | Rejected before validator repair and during import (`pdf_signature_unsupported`): rewriting signed bytes cannot preserve validity. Supported empty unsigned signature fields remain allowed. Use unsigned sources and sign the resulting final PDF externally. |
| Inherited MediaBox, CropBox, Rotate, Resources | Materialized onto every page |

**Static-form defaults.** The root AcroForm `/DR` owns default-appearance resources. The adapter retains distinct actual object bindings under collision-free occurrence names; equal font/display names are not used to deduplicate resources. Effective root, inherited parent, terminal-field and widget `/DA` and `/Q` defaults are materialized before merging. Widget `/AP` and ordinary page resources keep their own scopes.

Supported default-appearance operators are `Tf` (a bound Font name and finite size), `gs` (a bound ExtGState name), numeric text state `Tc`, `Tw`, `Tz`, `TL`, `Tr`, `Ts`, direct gray `g`/`G`, RGB `rg`/`RG`, and CMYK `k`/`K`. Operands must have the correct arity/type. The parser recognizes PDF names and syntax rather than replacing names inside comments or strings. Resource dictionaries may contain Font, ExtGState, XObject, Pattern, ColorSpace and Properties bindings, plus dictionary-shaped auxiliary Encoding bindings used by ordinary static-form producers; ProcSet is a union of names. Preserving a category does not mean every operator that uses it is supported in `/DA`.

**Appearance regeneration.** A source that requests `NeedAppearances: true` is normalized within its own scope before merging, so it cannot force viewers to redraw another source's existing appearances. Supported text/choice widgets with an absent, empty or normal-only `/AP` dictionary retain their effective defaults and omit their normal appearance so viewers regenerate it. When down/rollover appearances are present, PDFConcat generates a normal vector appearance and preserves the original alternate stream references and resource bindings. Supported checkbox/radio widgets receive real editable normal-state streams. Shared source appearance dictionaries are copied per widget before normal appearances are replaced. Existing source appearances are retained when regeneration was not requested; unrelated widget and page resources remain independent.

Text/choice normal-stream generation uses the actual source font objects and encoding, effective `/DA` and justification, field values and flags, choice export/display labels, selections and list viewport. It supports recognized simple encodings on Type1/TrueType fonts, including encoding differences; declared glyph widths take priority over standard-font metrics. Embedded fonts require an explicit simple encoding and declared glyph widths; TrueType fonts require declared widths. Unembedded Type1 standard fonts can use their supported built-in encoding and metrics. Composite fonts and `/ToUnicode` remapping are outside this normal-stream generation contract; valid existing source appearances that do not request this transformation remain independent. Unsupported font encodings, unavailable glyphs or metrics, malformed selections, unsupported appearance shapes and nonfinite drawing geometry are refused during shared inspection. Supported background/border colors, solid/dashed/beveled/inset/underlined borders, quarter-turn rotation, multiline/password/comb text and editable choices remain source-scoped.

The merged form does not retain a global regeneration request. An editor that writes a new normal appearance should make that appearance authoritative; an editor that requests viewer regeneration uses the preserved field defaults and actual resource bindings. These are different rendering policies. PDFConcat preserves current source appearance and ordinary editing data; it does not control how an external editor generates or replaces appearances.

For requested button regeneration, the supported contract is an unrotated positive square rectangle, explicit solid border with finite positive width, explicit RGB background/border colors in 0–1, an absent or whitespace-only effective `/DA` literal string, and a literal single-byte ZapfDingbats caption: `4` for checkbox or `l` for radio. Hex-string button defaults/captions are outside this regeneration contract. Normal appearances must contain real `Off` and one named on-state stream; `/V` is a name, inherited radio parent values are respected, and `/AS` must agree with the selected state. Push buttons, rotated/non-square or unsupported border/color/caption/default shapes, inconsistent state/value selections and nonfinite computed drawing geometry are refused during shared inspection/import. This restriction applies to requested regeneration; it does not remove valid ordinary source button appearances that do not request it. PDF real numbers are emitted without exponent notation. No appearance is replaced by a null or down-only state.

Other `/DA` operators, including color-space selection (`cs`/`CS`), resource color operations (`sc`/`SC`/`scn`/`SCN`), painting, text-showing and XObject invocation, are refused. Missing bindings, malformed appearance syntax, field-local `/DR`, cyclic/shared/inconsistent field ancestry and excessive field-tree depth are refused during shared prevalidation (`pdf_form_unsupported`). Text/choice fields require an effective font-bearing `/DA`. Use ordinary static forms whose defaults fit this contract; refusal does not flatten or silently substitute resources.

**Material consequences.** Source inspection records reachable feature observations once, before backend validation can repair catalog state. Stable codes use `source_<kind>_removed` or `source_<kind>_retained`, with kinds `bookmarks`, `tagged_structure`, `catalog_attachments`, `catalog_actions`, `page_actions`, `page_attachments`, `page_labels` and `other_catalog_names`. Application diagnostics aggregate each kind for a distinct source and identify its consumers. Consumer grouping takes one pass through declarations; ordinary sources without material features produce no warning records. Action/name/field observation follows reachable references with cancellation, cycle detection and a 64-level depth bound. Indirect action/subtype names are resolved, and JavaScript stream emptiness is checked after bounded library decoding. Ordinary links/forms and absent/empty features do not warn. Removal of a catalog attachment index does not assert that all payloads disappeared. Check and unpublished jobs carry `consequence_context: predicted`; committed builds carry `committed`. A saved query preserves that captured distinction. This policy preserves supported content rather than sanitizing output.

**Geometry.** A page's visible size is the effective CropBox clipped to the effective MediaBox, with width and height swapped for rotation 90 or 270, times UserUnit. Poppler renderings are the independent check (Poppler itself ignores UserUnit).

**Failure handling.** Errors carry a stable code and name the source where one is responsible. pdfcpu panics on some malformed inputs; the engine returns them as errors. Written output is validated and its page count compared with the request.

## Release identity

Release archive inspection checks physical executable headers and Go build metadata for every target, plus the native host's printed identity. The release build keeps recorded linker arguments so its actual consumed `-X main.version` assignment is inspectable; Go suppresses that metadata with `-trimpath`, so release builds omit the flag. Executables can contain compiler source/build paths; byte reproducibility requires a stable checkout path along with the pinned inputs. This does not change the offline runtime or PDF behavior. See [`RELEASING.md`](RELEASING.md#version-metadata).

## Stateless pdfcpu use

The adapter loads pdfcpu with `ConfigurationModeStateless`, explicitly selects relaxed validation, forces offline operation, and disables automatic merge-bookmark creation. PDFConcat therefore does not initialize, read, or write a user pdfcpu configuration directory, initiate backend network activity, or create bookmarks merely as a side effect of concatenation.

## Verification invariant

Publication requires both:

1. the staged assembled PDF passes pdfcpu validation; and
2. `actual pages == sum(source pages) + sum(blank counts)`.

The engine checks both against the written file before `internal/app` stages anything for publication. A mismatch is a hard failure, the requested output is not published, and the report says so (`output_page_count_mismatch`, `output_invalid`).

## Concurrency

Only source capture and inspection are concurrent: at most `--jobs` workers (default the smaller of 4 and `GOMAXPROCS`, never more than the number of distinct sources), each inspection with its own pdfcpu configuration clone. Results are stored by position and diagnostics are ordered by the first use of each source, so the outcome does not depend on completion order. Assembly is a single sequential pass, and each worker holds at most one source open. CI configures race-detector runs where the Go platform supports them; actual native job results establish execution.

## Scale and resources

Declared bounds, not guarantees: plans are limited in size, nesting, structural nodes, flattened contributions and generated pages (see [`PLAN.md`](PLAN.md#limits)). No fixed-memory or fixed-time claim is made. Three things set the cost of a job:

- **Disk.** A job needs scratch space for a copy of every distinct source and font file. A build puts its workspace beside the destination; `check` uses the system temporary directory. The workspace is removed on every exit path.
- **Memory.** The pool and the written file are held in memory, so memory grows with the size of the imported documents and with the page count, and there is no streaming mode.
- **Generated text.** A build shapes one distinct page and immediately writes it into the private shared resource document. Only bounds/findings remain after that page; fonts and used-glyph bitsets are job-wide. A check also drops shaped lines after each page. Ordinary supported text is shaped once; a missing nominal glyph requires an early shaping probe because font composition can supply it. Source PDF pooling still grows with imported document bytes.

The measurement method for wall time, peak memory, descriptors and scratch disk is in one place: [`DESIGN.md`](DESIGN.md#measured-results).

## Dependency policy

Runtime dependencies stay minimal. Direct dependencies:

- `github.com/pdfcpu/pdfcpu` — PDF parsing, validation, and merging;
- `github.com/go-text/typesetting` — text shaping and TrueType parsing for generated pages;
- `golang.org/x/image` — fixed-point arithmetic for text shaping;
- `golang.org/x/sys` — native publication and file-identity primitives on Linux, macOS, and Windows;
- `golang.org/x/term` — detecting an interactive terminal on standard error;
- `golang.org/x/text` — Unicode artifact-name matching, text direction validation and pdfcpu encodings; and
- `go.yaml.in/yaml/v3` — linked through pdfcpu, and used by the quality-policy tooling to read the central exception registry (the product itself reads and writes JSON).

[`THIRD_PARTY_NOTICES.md`](../THIRD_PARTY_NOTICES.md) lists the modules linked into release binaries; a test keeps it in agreement with `go.mod`. Do not introduce a framework when a small standard-library implementation is clearer. In particular, the command grammar is parsed without Cobra, and the plan format is JSON precisely because the standard library reads it.
