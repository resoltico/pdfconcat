# Architecture

PDFConcat keeps one CLI and two JSON exchanges. No public SDK or additional UI is
provided. The framework-free packet model lives in `internal/core` (package
`packet`); its diagnostics and complete factual outcomes live below that package.

| Package | Responsibility |
| --- | --- |
| `cmd/pdfconcat` | Signals, streams, working directory, environment, GOMAXPROCS and build metadata capture |
| `internal/cli` | Argument grammar, help, stdin, terminal/JSON rendering, caps and exit mapping |
| `internal/app` | Acquisition, original input reservation, inspection, composition, digest rechecks and publication ordering |
| `internal/core` | Typed declarations, semantic validation, owned traversal, layout, vocabulary and domain facts |
| `internal/core/diag`, `internal/core/outcome` | Diagnostic definitions, outcome precedence and complete execution facts |
| `internal/plan` | Strict, located plan v6 decoding with an explicit native path resolver |
| `internal/compose` | Measured generated pages, contents and numbering |
| `internal/typeset` | Font value parsing, shaping, wrapping, placement and embedding preparation |
| `internal/pdf` | PDF graph admission, selection, assembly, serialization and strict output verification from acquired bytes |
| `internal/files` | Native path semantics, regular-file acquisition, role checks and staged-file lifecycle |
| `internal/wire` | Result v7 DTOs, domain projection and schema |
| `internal/legal` | Embedded license notices |
| `internal/oracle`, `internal/pdffixture` | Test-only independent observations and synthetic PDFs |

The production import DAG is checked by
[`internal/architecture`](../internal/architecture/dependencies_test.go), including
negative controls for distinct forbidden properties. Depguard also confines
pdfcpu, shaping and native adapters. Third-party source in `third_party/pdfcpu`
is a separate module, narrowly outside first-party lint and suppression rules.
Development artifact generators have their own file I/O and are not product
runtime adapters.

```text
core    -> standard/value libraries, core/diag
outcome -> core, core/diag
plan    -> core
files   -> core
compose -> core, typeset
typeset -> value libraries
pdf     -> core, compose, typeset, pdfcpu
wire    -> core, core/outcome
app     -> core, plan, files, compose, pdf, wire
cli     -> app, core, wire, legal
cmd     -> cli
```

Core and typeset read no files or process settings. Plan receives native path
resolution explicitly; relative bases must already be absolute. Only files opens
product input/output paths. PDF assembly receives the application's reread
original bytes, checks their inspected digest, and re-applies source admission.
The application reopens staged output and passes its bytes to strict verification.

## Frontend contract

`app.Execute(ctx, request)` accepts `Check` or `Build`, an absolute `WorkingDir`,
and one sealed input variant:

- `PlanFile{Path}` reads a named plan relative to `WorkingDir`.
- `PlanBytes{Data, Label, BaseDir}` decodes supplied bytes; `Label` supplies display
  provenance, and `BaseDir` resolves relative to `WorkingDir` when supplied.
- `Declared{Declaration}` supplies all source, page, contents, section, directory,
  style, metadata, fitting and numbering capabilities without JSON. Core semantic
  admission clones and validates the declaration before native resolution.
- `Items` contains `SourceItem{Ref, Path}` and `BlankItem{Ref}` values. Each ref
  must be nonempty and unique within the request.

`Output`, `Report`, `Overwrite` and an optional `FitTo` supply execution overrides.
`app.ParseSheet` constructs a validated fitting size. The frontend captures the
raw `SOURCE_DATE_EPOCH` value in `Request.SourceDateEpoch`; core parses it only
when the plan has no authored timestamp. Core reads no process environment.
Invalid request metadata returns `request_invalid` before file I/O. Input storage
must remain stable during execution. The core belongs to this repository's
internal Go API; the CLI and the plan/result schemas define the external interfaces.

`app.Schema` returns an owned copy of the plan or result schema. `cli.Version`
projects explicit host build metadata, and `cli.ExitCode` maps the complete
outcome status. The CLI represents invocation failures through
`cli.InvocationFailure`; the CLI owns stdin acquisition before calling Execute.

## Vocabulary and declarations

An item is one sealed source, page, contents or group declaration. A titled group
is a section. A part is one contiguous physical span of kind source, page,
divider, contents or padding. A ref is an opaque item identity: a plan JSON
Pointer or an `argv:N` operand reference.

Packet enums define their wire values once. A `Setting[T]` stores a value and
an explicit presence bit and independent provenance. Explicit zero/empty values
therefore override defaults without invented JSON pointers.
`Font{}` identifies the bundled font. The plan decoder resolves directory scopes
into absolute paths. Contents uses the root `PageDefaults.Text.Font` setting,
including its declaration provenance; all its heading, row and label measurements
use that font. Its other layout rules remain owned by compose, and page-number
overlays retain the bundled font.

`packet.NewPlan` walks the item tree once, caching item order, section ancestry
and traversal events. Resolution consumes that traversal after source shapes and
contents page count are known. Padding has no declared item index.

## Execution and projection

`Execute` validates the request before I/O, then runs stages in order:

1. Admission resolves roles and acquires/decodes a plan or direct items.
2. Inspection loads fonts and checks text, then inspects distinct sources with
   the explicit positive `InspectionWorkers` budget. Inspection findings are merged in first-use order;
   one global structure policy is passed through grouped inspection and assembly
   re-admission. Original source security/page checks precede semantic detachment
   or narrowly validated index normalization. Owner-sensitive coverage retains
   an uncovered-content witness.
   Final results apply the shared [diagnostic priority](CLI.md#output), preserving
   stage/first-use order within peers.
3. Composition prepares contents, resolves physical layout, checks compatibility
   and number overlays, and lays out generated text.
4. Build prepares font resources, stages the PDF, assembles, verifies and publishes.
5. Projection creates the result from the facts each stage actually returned.
6. Report publication rechecks destination roles after any PDF publication.

Private source sites retain full source refs, original page ordinals and paths.
Admission captures catalogue/navigation sites before backend normalization;
selection/assembly carry typed change evidence before pruning or renumbering.
Only the existing PDFLocation projection crosses the diagnostic boundary.
Distinct findings survive until result coalescing; no producer deduplicates
solely by warning code/message.

Inspection, composition and build proceed only while preceding processing stages
have no errors. Build runs only for `Build` requests. After admission, projection
still runs when processing has failed, and a requested report can still be
published when its destination rules allow it.

Projection creates result parts, sections, counts and PDF publication facts from
the returned stage outcomes. It preserves readable refused sources' known hashes,
byte counts and page counts. Unknown facts remain null. Report writing records
its own visibility and any trailing failure.

Per-item inspection facts align with `Plan.Leaves()`; physical facts and rename
slots align with `Layout.Parts`. Each physical part retains its declared item
index, or `-1` for padding. Each source occurrence refers to one shared file-fact
record. Composition passes one typed part sequence to assembly. Fonts and PDF
assembly remain sequential.

The compose package owns measured geometry. `PageNumberLayout` shares the bundled
overlay font's digit measurements across exclusion geometry, pre-assembly checks,
font preparation and PDF painting. Contents measures its inherited font separately
for heading, row and label placement; the resolved label mapping is shared by
contents, PageLabels, reports and overlay painting. Generated blocks carry semantic
roles and link associations to the PDF adapter.

The typesetter owns wrapping policy. Emergency wrapping intersects Unicode
grapheme boundaries with the shaper's safe cluster boundaries, retaining shaped
glyphs rather than reshaping or inserting source characters. It preserves the
ordinary wrapping pass and leaves indivisible runs to the same overflow checks.

### PDF validation boundary

The PDF adapter owns source graphs, per-occurrence navigation and final PDF
validation. Source admission parses strictly and checks original page/content
references and signature evidence before applying structure policy. Explicit
discard removes semantic claims before backend tag validation. Preserve admits
ownership remnants without a hierarchy as untagged and validates narrowly allowed
derived index normalization; damaged declared semantics remain errors. Source viewer
directives are removed before semantic compatibility validation. Inspection and
assembly share this path, which returns accumulated findings alongside any failure.

The adapter resolves page geometry from the validated Kids traversal in linear
time. Each scoped view owns its effective frame, physical size and inherited
attributes; null entries inherit, while local zero rotation and empty resources
override ancestors. Declared rectangles normalize their opposite corners before
intersection. Computed intersections remain signed so empty source frames cannot
become visible artwork. Inspection, selection, fitting, generated links and
number overlays consume batched page views rather than repeated root lookups.
Views are rebuilt after tree or page mutations and are never cached on the backend
context.

Assembly uses pdfcpu's object model and merge operations. The adapter's serializer
writes the reachable unencrypted graph in canonical order, retaining encoded
streams and controlling metadata without introducing wall-clock dates or file
identifiers. Backend object identities remain private to the adapter.

Output verification uses strict validation without source normalization and
checks catalog policy before validation. A shared validation boundary checks page
order, live content-stream references and encoded bytes before and after
validation. Source admission and serialized output also check every page-content
encoding. Flate EOF/checksum checks use the standard library alongside backend
stage/predictor decoders and same-context budgets. Validated decoded caches are
immutable; original encoded data is retained.

Validation notices are evidence; their message text does not select policy.
Diagnostic wrappers preserve backend detail and PDF locations. Result coalescing
merges only equivalent findings and unions their consuming item references.
See [PDF policy](PDF-POLICY.md) for fidelity and resource limits.
