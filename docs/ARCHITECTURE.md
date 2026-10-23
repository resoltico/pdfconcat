# Architecture

The application core owns packet vocabulary and execution. Frontends submit a
`core.Request` and receive a complete `core.Result`. Plan and result wire contracts
are documented in [PLAN](PLAN.md) and [CLI](CLI.md).

## Packages and boundaries

| Package | Responsibility |
| --- | --- |
| `cmd/pdfconcat` | Signals, standard streams, working directory, linker version and process exit |
| `internal/cli` | Argument grammar, help, stdin acquisition and text/JSON presentation |
| `internal/core` | Admission, inspection, composition, build, result projection and reports |
| `internal/core/internal/packet` | Sealed items, enums, styles, units, limits and physical layout |
| `internal/core/internal/plan` | Located plan decoding and the embedded plan schema |
| `internal/core/internal/compose` | Pure page text, contents pagination and footer placement |
| `internal/core/internal/typeset` | Fonts, shaping, wrapping, placement and glyph pruning |
| `internal/core/internal/pdf` | pdfcpu inspection, policy, generation, assembly and verification |
| `internal/core/internal/files` | Regular-file reads, destination roles and staged publication |
| `internal/core/internal/diag` | Ordered code registry, diagnostic vocabulary and outcomes |
| `internal/core/internal/result` | Complete wire model, normalization and embedded result schema |
| `internal/pdffixture`, `internal/oracle` | Test-only synthetic inputs and independent PDF observations |

Go's nested `internal/` rule confines domain packages and adapters to the core
tree. Production CLI code imports core and performs no file, process or network
I/O. Depguard confines pdfcpu to pdf, go-text to typeset, native calls to files,
and test support to tests. See [contributor checks](../CONTRIBUTING.md).

Allowed production dependencies within the core tree are:

```text
core    -> diag, result, packet, plan, compose, typeset, pdf, files
plan    -> packet, diag
result  -> packet, diag
packet  -> diag
compose -> packet, typeset, diag
pdf     -> packet, compose, typeset, files, diag
files   -> diag
typeset -> external font libraries
diag    -> standard library
```

## Vocabulary and declarations

An item is one sealed source, page, contents or group declaration. A titled group
is a section. A part is one contiguous physical span of kind source, page,
divider, contents or padding. A ref is an opaque item identity: a plan JSON
Pointer or an `argv:N` operand reference.

Packet enums define their wire values once. A `Setting[T]` stores a value and
the ref that declared it, so explicit zero/empty settings survive layering.
`Font{}` identifies the bundled font. The plan decoder resolves directory scopes
into absolute paths.

`packet.NewPlan` walks the item tree once, caching item order, section ancestry
and traversal events. Resolution consumes that traversal after source shapes and
contents page count are known. Padding has no declared item index.

## Execution and projection

`Execute` validates the request before I/O, then runs stages in order:

1. Admission resolves roles and acquires/decodes a plan or direct items.
2. Inspection loads fonts and checks text, then inspects distinct sources with
   at most `GOMAXPROCS` workers. Findings are merged in first-use order.
3. Composition prepares contents, resolves physical layout, checks compatibility
   and footers, and lays out generated text.
4. Build prepares font resources, stages the PDF, assembles, verifies and publishes.
5. Projection creates the result from the facts each stage actually returned.
6. Report publication rechecks destination roles after any PDF publication.

A stage runs only when preceding stages have no errors. Projection alone creates
result parts, sections, counts and PDF publication facts. It preserves readable
refused sources' known hashes, byte counts and page counts. Unknown facts remain
null. Report writing records its own visibility and any trailing failure.

Per-item inspection facts align with `Plan.Leaves()`; physical facts and rename
slots align with `Layout.Parts`. Each physical part retains its declared item
index, or `-1` for padding. Each source occurrence refers to one shared file-fact
record. Composition passes one typed part sequence to assembly. Fonts and PDF
assembly remain sequential.

The compose package owns measured geometry. One Footer digit measurement serves
contents exclusion, pre-assembly checks, font preparation and PDF painting.
The PDF adapter owns source graphs, per-occurrence navigation and final PDF
validation. See [PDF policy](PDF-POLICY.md) for its fidelity and resource limits.
