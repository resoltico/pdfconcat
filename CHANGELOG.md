# Changelog

Notable changes to this project are documented in this file. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Contributor change: the fuzz gate now requires macOS or Linux so cancellation and completion can clean up owned Go workers. Other platforms refuse to launch fuzz targets; use a supported runner. Missing or skipped targets fail, and fuzz budgets must be positive durations or iteration counts.
- Breaking JSON contract: responses and complete reports use `format_version: 3`, with mandatory diagnostic severity and scoped error/warning counts; plans remain version 1. Consumers must read the current schemas. Unsupported saved formats are refused without modifying historical evidence. A successful query preserves the captured run's errors and warnings separately from its own operation outcome.
- Job attempts have unique identities. Report continuations use the invoking executable and actually queried report, with `--expect-attempt` correlation guards and explicit reconstruction references when exact argv exceed the summary budget.
- Help includes ordered merges with blanks, styled inline templates, and check/query/build guidance. Command errors omit irrelevant lifecycle fields; structured recovery and report-write facts explain safe repair, including the early-failure overwrite exception.
- `check` and `build` share source-known backend policy validation, rejecting unsupported legacy-destination occurrences and over-cap page totals at the offending contribution before rendering or assembly.
- Report requests reject invalid selectors, paging and attempt expectations before accessing the saved file. Compact text diagnostics include bounded input-path previews and retain full values in details.
- Static AcroForms preserve supported current appearance and ordinary subsequent editing across conflicting source default-resource names. Source-requested appearance regeneration is scoped before merging, preserving down/rollover states while generating supported text/choice normal appearances and valid editable checkbox/radio states. Actual font/encoding bindings and unrelated widget/page appearance scopes remain independent; unsupported regeneration shapes are refused. Qualified field names do not establish retained script behavior.
- Actual signed/certified/timestamped inputs are refused before assembly rewrites signed bytes. Use suitable unsigned inputs and sign the final PDF externally; supported empty unsigned signature fields remain allowed.
- Measured allowed-text overflow and material source-feature effects are visible as aggregated warnings, with exact consumers, predicted/committed context and attempt-bound detail routes. Catalog feature removal is distinguished from retained page-local actions and attachment payloads; output is not sanitized.
- A safe distinct failure report can record an unusable PDF target without fabricating report-target advice. Primary cause, shared no-clobber/alias rules and committed-output recovery remain authoritative.

### Added

- `schema response` describes structured responses and recovery guidance alongside the complete-report schema.

### Internal

- Structural gates now enforce physical source limits across owned tests, fixtures, generated and tagged files, with no grandfathering. Compiler-selected checks cover all six targets; rejection controls close trailing-comment, nested-directory and source-symlink discovery bypasses. The exported-type limit includes aliases and generic types.
- Mutation integration preserves access to the verified development checker in clean snapshots. The pinned executor retains complete receipts across native exit transitions and uses one bounded cleanup deadline without suppressing unknown or live-process errors.
- Lint-exception enforcement rejects unregistered mapped exemptions, disabled default error checks, predicate collisions, duplicate scopes, and unrepresentable cross-OS exclusions. Checker identity is validated against the pinned native source variant.
- Complete reports stream into owned staged files without an additional report-sized serialized buffer, retaining publication, cancellation and recovery safeguards.

## [0.1.0] - 2026-10-07

- First release.
