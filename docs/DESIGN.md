# Product design

PDFConcat assembles existing PDF documents in an explicit order and inserts generated blank pages at explicit positions. The plan is a description of the final packet, including repeated sources and generated-page appearance. It does not infer the author's intended order or choose files for them.

## The sequence is the interface

An ordered sequence keeps each insertion next to the documents it separates. A filename suffix, a directive attached to another filename, or a precomputed output page number hides that position or requires knowledge of source page counts. A generated page is part of the job rather than a separately managed blank PDF.

The plan can be a named JSON file, standard input, or one inline JSON argument. Direct PDF operands and `--blank` express small unstyled jobs. All transports resolve to the same assembly model. Appearance belongs in the plan, with item settings overriding group/root defaults and built-ins; a second style-flag language would create another precedence rule.

There is no implicit scan, glob, sort or source deduplication. File order and repeats remain visible in the instructions. Shell recipes in [`CLI.md`](CLI.md#large-jobs-and-shells) generate plans with an explicit ordering policy.

## JSON plans

The job is an ordered heterogeneous array of paths, generated pages and groups. JSON gives that array a portable typed representation that shells and programming languages can generate, and JSON Schema supplies editor validation. One syntax avoids duplicated parsers and divergent rules.

The strict token decoder rejects duplicate members, unknown keys, invalid Unicode and trailing data, retains declaration positions, and charges resource limits during reading. A schema cannot detect duplicate members or every unit-converted bound; [`PLAN.md`](PLAN.md) states these parser requirements. A bare string always names a PDF, including a string that resembles a CLI option. A group's `dir` supplies a path prefix without scanning or restricting filesystem access.

## Agent-visible evidence

Compact JSON is the default for results, failures, help and version. Human text carries the same outcome, location and repair facts. A bounded summary exposes publication state, captured counts and material diagnostics; complete reports retain precise declarations, digests, appearance findings and consumer references.

A successful job can carry warnings about measured clipping or consequential source-feature handling. Error and warning counts refer to captured diagnostic records, after aggregation. They do not count pages. A successful query can return errors from a failed saved run: the query operation and captured run have separate outcomes.

An attempt-bound continuation retrieves full diagnostics or a selected contribution without reopening source PDFs. Reports are captured evidence, not current filesystem observations or authentication. An agent compares intended order, page counts, source digests, provenance and warnings before accepting a packet. PDFConcat cannot infer that a valid explicitly named source is the wrong document for the user's purpose.

Counts and ranges remain null until their determining layout is known. Check/unpublished diagnostics describe predicted consequences; a published build records committed effects. Post-commit failures retain the actual publication and owned recovery state instead of suggesting a rollback or rebuild.

## Generated appearance

Nine anchors and PDF-coordinate offsets provide one placement model. Generated sizes inherit from neighboring source geometry unless explicitly set. Settings preserve declaration provenance through inheritance; clearing text retains the selected font and its validation.

The embedded Unicode font makes generated rendering independent of installed fonts. Shaping uses the selected font's metrics and mark positioning. Unsupported scripts, missing glyphs and invalid font data fail rather than producing fallback boxes. [`BLANK_PAGES.md`](BLANK_PAGES.md) defines supported text and whitespace.

Text is never silently shrunk or truncated. Overflow rejects by default. Explicit `allow` retains placement and records any measured consequence as a warning; an in-bounds placement does not warn merely because `allow` was selected. Block and glyph-ink findings share one layout authority.

## PDF fidelity and policy

One backend adapter owns PDF object semantics. Supported source pages, ordinary links and static forms retain their relationships. Static-form resource reconciliation preserves current appearance and ordinary subsequent field editing while keeping widget appearance scopes independent. Unsupported semantic shapes fail at shared inspection/import boundaries.

Assembly rewrites signed bytes, so actual signature-bearing inputs are refused. Use suitable unsigned sources and sign the final PDF in an external signing workflow. Retained page-local actions do not make output sanitized or establish script behavior after field names are qualified. Scope-specific keep/drop/refusal policy is in [`ARCHITECTURE.md`](ARCHITECTURE.md#pdf-backend-behavior) and [`SECURITY.md`](../SECURITY.md).

Runtime output verification validates the staged PDF and exact page count. Independent rendering, resource inspection, form refilling and signed-byte negative controls are development assurance; syntax validation alone cannot prove appearance or editability.

## Scale

Resolution emits compact runs rather than allocating a page object per planned repeat. The backend assembles once from a shared resource pool, with no per-blank rewrite. Distinct generated appearances share one resource document; each page is shaped and emitted sequentially, retaining report geometry and findings. Each distinct source and font is captured once. Sources with occurrence-sensitive annotations/forms/destinations are imported per occurrence to preserve relationships.

Source capture/inspection has bounded concurrency; assembly remains sequential. PDF pooling and the written output remain proportional to imported content and output page count. Reports serialize directly into owned target-local staging files. These mechanisms reduce redundant work; they do not establish a universal memory or elapsed-time envelope.

### Measured results

Measure the delivered executable and record its source/input identity, tool versions and host. Keep raw execution and independent-oracle evidence outside the repository; do not substitute older measurements for a changed binary. Published release outcomes belong in [`CHANGELOG.md`](../CHANGELOG.md).

The deterministic [`test/scale`](../test/scale/doc.go) harness exercises distinct sources and generated styles, file/stdin plans, repeated styles, long text and image-rich inputs. Run only an explicitly authorized scale campaign:

```text
PDFCONCAT_SCALE=1 PDFCONCAT_REQUIRE_QA_TOOLS=1 go test ./test/scale -run TestScaleAcceptance -count=1 -timeout 30m
```

Set `PDFCONCAT_SCALE_RESULTS` to retain JSON rows. Record launch-to-exit wall time, output/response bytes, RSS, sampled descriptors and scratch disk separately from fixture generation and independent verification. Descriptor/disk samples and Windows RSS are observed lower bounds; kernel peak RSS on Unix has a different measurement contract. A missing observation fails enabled acceptance rather than becoming zero.

The harness declares budgets for its light cases; other workloads are measurements. See [`CONTRIBUTING.md`](../CONTRIBUTING.md#gates) for exact discovery and acceptance controls. Native platform behavior requires execution on that platform; a cross-build establishes compilation only.

## Scope boundaries

The tool remains an offline standalone executable with no daemon, configuration directory, implicit correction, network dependency or second runtime PDF engine. It does not repair arbitrary PDFs, authenticate signatures, sanitize active content, validate an author's intent, perform OCR or convert other document formats. Keep separate parsing, layout, backend and publication responsibilities when extending the implementation.
