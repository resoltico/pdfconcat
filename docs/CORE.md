# Application core

`internal/core` is the in-module application boundary for check/build and shared
schema/version queries. It is not an external SDK stability commitment. The CLI
owns argument grammar, stdin lifetime and presentation, and uses core for
application logic. In-module callers submit the same declarations and receive
the same complete facts.

## Requests and declarations

```go
sheet, err := core.ParseSheet("A4")
// Handle err before submitting user intent.
request := core.NewRequest(core.Build,
    core.FromPlan(core.FilePlan("packet.json")),
    core.Options{WorkingDir: absoluteHostDirectory, FitTo: &sheet})
outcome := core.Execute(ctx, request)
// Inspect outcome.Output.Published separately from outcome.Status.
```

Requests, inputs, items, sheets, plan sources and documents have private
representations. `Execute` rejects zero/unsupported envelopes and duplicate
explicit direct references before I/O with `request_invalid`. Rejection results
encode under result v4 even for invalid UTF-8 metadata or unsupported operations.
Constructors copy bytes, item slices and optional sheet overrides. Results are
complete caller-owned values; there is no caller-supplied validated job/backend
state. All nested DTO types can be named through core.

`Options.WorkingDir` is an explicit absolute host directory. Output/report
options resolve there. File plans resolve there and their declarations resolve
relative to the plan's lexical parent. `BytesPlan(data, label, base)` and
`LoadPlan(label, base, loader)` use WorkingDir when base is empty and resolve a
relative base there. Labels are virtual provenance, not physical reservations.
No call changes the process working directory.

`FromItems(core.Source(path, reference), core.Blank(reference))` is the small
direct-source/blank language. It shares declaration budgets, defaults and path
semantics with plan v3, without synthetic JSON or a JSON byte ceiling. References
are opaque provenance and unique occurrence keys. Explicit references are
reserved first; empty references become `item:1`, `item:2`, …, skipping reserved
values. Rich authoring uses plan JSON, not a parallel rich DTO validator.

## Plan acquisition and editing

`ReadPlan(ctx, source, workingDir)` and `DecodePlan(data, label, absoluteBase)`
perform static declaration validation. They do not open fonts or PDFs and do not
establish a successful Check. A valid `PlanDocument` retains owned original bytes,
base, physical opened-file origin and derived effective input reservations. It
contains no cached job or backend resources. `JSON()` copies the original bytes,
including BOM, formatting, omitted fields and explicit clears. `ReplaceJSON`
validates edits while preserving physical origin/base; failure returns a zero
document and leaves the original value valid. Each execution decodes afresh.

A `PlanLoader` is invoked synchronously at most once per Execute, zero times when
already canceled, with the authoritative 64 MiB original-JSON limit. Core checks
context/wrapped cancellation first, actual oversize second and ordinary errors
third. Bytes returned with an error are never partially decoded. Callback bytes
must be owned/stable on return; core takes a copy. The host must obey context and
own callback termination, repeatability and reentrancy. Core starts no detached
reader/closer work. Only the CLI stdin adapter uses process-lifetime read/close
goroutines to return promptly after cancellation. Filesystem reads and pdfcpu
inner work have no universal interruption deadline.

## Execution and results

Independent file/byte/document/direct requests with disjoint publication paths
can run concurrently, including reuse of a document. They share no mutable
job/backend cache and need no frontend business lock. Changing external files and
host loaders have their own acquisition contracts.

Execution has explicit admission, preparation, composition, build and final
report stages. Failed admission retains known reservations. Preparation loads
consumed fonts/text and inspects immutable whole-source/per-occurrence facts.
Composition resolves the single physical layout and measures generated text,
contents and footers. Build owns create/abort/assemble/verify/commit and returns
actual publication/hash/byte/rename facts. Finalization attempts the full report
once, rechecking current file roles. Unknown stage facts remain null.

Core returns uncapped normalized diagnostics, consumers, parts and sections, and
writes complete report JSON. Component error messages are made valid UTF-8 at the
diagnostic boundary. CLI summary/terminal rendering works on copied views, caps
diagnostics/consumers/preview rows and never recomputes PDF policy. Exit status
uses complete diagnostics. A transport write error cannot roll back publication.
`InvocationFailure` adapts closed usage/cwd faults without execution or report
I/O. The parser retains its complete normalized failure for rendering. Acquired
host errors are made valid UTF-8 and embedded NUL is escaped before entering that
strict metadata factory; environmental failures keep their failure class and
empty optional references do not create empty consumers. Help/version/schema do not require a working-directory provider.

`Schema(PlanSchema|ResultSchema)` returns owned bytes and rejects unknown
selectors. `Version(releaseOverride)` validates metadata and shares the result
wire version source. Plan v3 and result/report v4 remain the wire contracts.

## Input reservations and failure reports

Known missing plan/source/consumed-font paths are protected physical reservations.
A report in a verified different existing directory is separate. Equal unresolved
leaf names under an identical native directory identity collide. Other unresolved
same-directory relationships are refused as `report_identity_unverified`; use a
different existing report directory. Inspection/identity failures do not authorize
writing. Native Windows identity errors retain their operation and queried path,
including an input or ancestor path distinct from the report destination.
Existing target links/nonregular objects are refused before identity following.
Unused default font declarations are not physical inputs, while selected fonts
are inputs even for empty text.

Initial checks do not demand a report parent or guess identity between absent
output/report names. PDF publication can succeed before report publication
fails; inspect `output.published`. The mandatory late report check protects the
actual PDF. A successful Check report does not certify universal separation from
an absent output under alternative spellings. Stable trusted namespaces and
existing-byte protection remain the scope, without adversarial-race or
universal network-filesystem guarantees.
