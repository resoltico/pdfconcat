# Changelog

Notable changes to this project are documented in this file. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] - 2026-10-15

### Changed

- **Breaking:** plan v2 requires `version: 2` and renames top-level/item `blank` to `page`; migrate existing plans. Counts must use integer tokens from 1 to 100,000, so spell `3.0` or `3e0` as `3`; total generated pages are limited to 100,000 instead of 1,000,000. The CLI `--blank` operand remains supported. See [plan migration and limits](https://github.com/resoltico/pdfconcat/blob/v0.3.0/docs/PLAN.md).
- **Breaking:** result/report v3 replaces the previous response/report contract. Stdout caps diagnostics at 20 and consumer pointers at 20 per diagnostic, with omission counts; `--report` retains full parts, ranges, known hashes/byte counts and actual field mappings. Update JSON consumers to the [result contract](https://github.com/resoltico/pdfconcat/blob/v0.3.0/docs/CLI.md#output) and use `schema result` instead of `schema response` or `schema report`.
- **Breaking:** the `report` query command and its selectors, `--progress`, `--jobs` and `--plan-json` are removed. Use `--plan FILE` or `--plan -` for plans and consume the full `--report` JSON directly; source inspection and assembly are sequential.
- **Breaking:** paths resolve lexically, and output/report permissions follow umask instead of private-by-default ACLs; replacements retain their permission bits. Private ACLs, captured source/font workspaces and report-recovery copies are removed; source bytes are re-read and digest-checked before assembly. Use stable, trusted local paths and inspect `output.published` before retrying after a late failure; network-filesystem durability is not guaranteed. See [file-safety limits](https://github.com/resoltico/pdfconcat/blob/v0.3.0/SECURITY.md).
- **Breaking:** unmodified upstream pdfcpu v0.16.2 replaces the maintained forks and their strict malformed-token/duplicate-key/free-head refusals and parser work/resource budgets. Relaxed parsing can accept repairs or malformed syntax; finite dictionary reals may serialize to twelve fractional digits, and arbitrary numeric/dictionary identity is not promised. Specific unsafe canonical-name and shared resource/annotation shapes are refused. See [PDF policy and limits](https://github.com/resoltico/pdfconcat/blob/v0.3.0/docs/DESIGN.md#pdf-policy).
- **Breaking:** multiple nonempty static forms require identical resolved `/DR`, `/DA` and `/Q` representations; conflicting defaults are refused instead of using the former appearance/resource engine. `NeedAppearances` remains a viewer-regeneration request with a warning, not a promise of generated appearances. Widgets without appearance dictionaries can fail assembly when any source raises the output to PDF 2.0; provide complete appearances or assemble separately. Widgets remain unsupported for fitting. See [form limits](https://github.com/resoltico/pdfconcat/blob/v0.3.0/docs/DESIGN.md#forms-fitting-and-catalog).
- **Breaking fidelity boundary:** fitting uses upstream resize. Destination page targets survive, but scroll coordinates are not transformed; former print-box, content-analysis and appearance guarantees are removed. UserUnit is normalized to preserve physical target sheet dimensions. Coordinate destinations can now be accepted within the documented limitation; review [fitting limits](https://github.com/resoltico/pdfconcat/blob/v0.3.0/docs/CLI.md#build-check-and-fitting) before relying on print or imposition fidelity.
- Fitting accepts the full sheet-size grammar, including explicit landscape dimensions, beyond the previous `A4`/`Legal` choices.
- Generated PDFs normally embed glyph-pruned TrueType fonts with stable glyph IDs and shared job-wide resources. Embeddable fonts that prohibit subsetting retain their complete original programs.

### Added

- Source bookmarks are preserved, including occurrence-specific direct and named destinations.
- Repeated legacy `/Dests` sources retain per-occurrence targets. Existing repeated named-destination and damaged-page-tree protections remain in place.
- Release packaging includes executable archives for macOS, Linux and Windows on amd64 and arm64, documentation, schemas and third-party notices/licenses. Tagged release publication provides checksums and build provenance for the archives. Runtime modules no longer use local replacements, enabling module-based installation of a compatible published tag; `v0.2.0` and earlier published releases contain source only.

### Internal

- The bespoke verification platform is replaced by stock Go tests, curated lint/depguard, actionlint and govulncheck with pinned tooling. Six-job CI retains `Required CI`; independent qpdf/Poppler checks cover destinations, forms, font extraction/pruning, fitting and publication/report state.

## [0.2.0] - 2026-10-09

### Changed

- Source builds and contributor checks require Go 1.27.2; update the SDK before rebuilding to include standard-library security fixes. The required version is declared in `go.mod`.
- Breaking JSON contract: responses and complete reports use `format_version: 2`, replacing `v0.1.0`'s `report_version: 1`. Diagnostics carry severity and scoped error/warning counts; queries distinguish their own outcome from the captured run. Plans remain version 1. Update consumers to the current schemas. Unsupported saved reports are rejected without modification; keep historical evidence and run a fresh `check` to a new report target when current evidence is needed.
- Breaking source policy: signed, certified, usage-rights-signed and document-timestamped inputs are refused before assembly rewrites signed bytes. Assemble suitable unsigned inputs and sign the final PDF externally. Ordinary assembly still permits supported empty unsigned signature fields; fitting refuses widgets. PDFConcat does not authenticate signatures.
- Breaking source policy: a source `/Page` with effective non-null `/Kids` is refused in ordinary and fitted assembly, including empty arrays, because readers can discard its content. Repair the conflicting page tree and recheck; absent and null entries remain supported.
- Breaking source policy: duplicate decoded dictionary keys and unsupported, malformed or unrepresentable numeric instructions are rejected rather than concealing earlier values or substituting zero. Finite real dictionary numbers retain exponent-free binary64 precision.
- Contributor fuzz runs require macOS or Linux for owned-worker cleanup; Windows refuses to launch fuzz targets. Budgets must be positive, missing/skipped targets fail, and deliberately detached descendants are unsupported. See [contributor verification](CONTRIBUTING.md).

### Added

- `build` and `check` accept `--fit-to A4|Legal` and plan `fit_to` to enlarge or shrink every page proportionally onto centered portrait sheets, preserving displayed orientation without an extra landscape rotation. CLI overrides the plan. Original clipping, supported print boxes, page order and occurrence-specific link coordinates are preserved in the one-pass assembly; no rasterization is used. See [fitting](docs/CLI.md#fitting-every-sheet).
- Fitting supports static content and narrowly defined unpainted links with stable absolute, nonmapped URI targets or local coordinate-free `Fit` destinations. Editable forms/widgets, painted or cropped hotspots, coordinate destinations and unsupported coordinate/resource state are refused. Explicit generated canvases retain authored overflow checks; implicit/inherited canvases use the target. Reports and queries expose original/final geometry and final text bounds/font size. Fitting does not preserve every bleed/imposition workflow or guarantee every reader's numeric range; [the fitting contract](docs/CLI.md#fitting-every-sheet) defines these limits.
- Opt-in `--progress json` emits attempt-bound NDJSON, real scoped counters and ten-second liveness on stderr; `auto` retains interactive text and `none` disables telemetry. Owned native transport controls backpressure and shutdown, with structured cleanup/committed-state facts. An aborted write can leave one unterminated terminal suffix and disable the channel; an unavailable sink cannot guarantee delivery. Windows waits for actual writer completion, so arbitrary drivers have no universal cancellation return-time guarantee. Progress never replaces the final result or saved report. See [progress output](docs/CLI.md#output).
- `schema response`, unique attempt identities and `report --expect-attempt` support structured agent workflows. Help, exact argv continuations and recovery guidance preserve full values when bounded previews are insufficient. Attempt checks guard correlation, not integrity or current PDF contents.
- Allowed generated-text overflow and material source-feature effects produce aggregated warnings with exact consumers and predicted/committed context. Saved queries retain historical severity and scoped counts. Catalog removal is distinguished from retained page-local actions and attachments; output is not sanitized, and retained script behavior is not verified. See [PDF feature policy](docs/ARCHITECTURE.md#pdf-backend-behavior).

### Fixed

- Ordinary assembly preserves supported AcroForm appearance and subsequent editing when imported default-resource names conflict. Requested regeneration supports qualified text, choice, checkbox and radio appearances while retaining down/rollover states and unrelated scopes. Breaking fix: unsupported regeneration semantics are refused; supply a supported static form. [Form limitations](docs/ARCHITECTURE.md#pdf-backend-behavior) describe the supported defaults and field behavior.
- Source dictionaries and object/generation references retain exact identity through validation, inheritance and cloning, preserving original page boxes, paint and links in shared resource dictionaries. Missing references cannot become generated objects or expose wrong-generation content. Indirect `/DecodeParms` arrays retain predictors, and null optional catalog versions preserve the header version.
- Retained relative URI actions using an omitted nonempty catalog URI base warn once per source with all affected occurrences. Keeping action bytes does not guarantee external URI resolution; no network destination is tested or rewritten.
- `check` and `build` share source-known restrictions, locating unsupported legacy destinations and excessive output-page totals before rendering or assembly. Report selectors, paging and attempt expectations are validated before saved-file access.
- Fixed-width generated-text overflow identifies the effective width declaration and recommends reducing it. Logical block bounds are distinguished from glyph ink; ordinary placement, word and vertical overflow keep their declaration locations and consumers.
- A safe, distinct failure report records an unusable PDF target without inventing report-target advice. Publication retains no-clobber/alias checks and truthful committed-output recovery; overlong paths have lossless recovery references, and complete reports stream into staged files without another report-sized serialized buffer.

### Internal

- Maintained PDF dependency copies have pinned upstream source/patch/license provenance and bounded cooperative parsing/decoding controls. Foreign-package verification uses independently pinned upstream fixtures in isolated modules; the runtime remains standalone and offline.
- Strict contributor checks cover physical owned source, tagged/generated files, aliases and generic types, with defended central exemptions and compiler-selected ownership across six targets. The pinned linter checks source attribution and concrete field reads; known corrupt-font and real linter rejection controls remain, with isolated fixture caches.
- The pinned mutation executor retains complete discovery/execution receipts and bounded cleanup across native exit transitions. Targeted nonintegration single-package runs limit baseline work to the selected package without changing root-relative mutation identities or exempting uncovered mutants. Scale acceptance retains native collector/compiler provenance and fails missing or live-error resource samples; descriptor samples are lower bounds, separate from enforced kernel ceilings.

## [0.1.0] - 2026-10-07

- First release.
