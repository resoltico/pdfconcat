# Changelog

Notable changes to this project are documented in this file. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
