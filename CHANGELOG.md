# Changelog

Notable changes to this project are documented in this file. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Contributor change: the fuzz gate now requires macOS or Linux so cancellation and completion can clean up owned Go workers. Other platforms refuse to launch fuzz targets; use a supported runner. Missing or skipped targets fail, and fuzz budgets must be positive durations or iteration counts.
- Breaking JSON contract: structured responses and complete reports use `format_version: 2`; plans remain version 1. Report queries separate their outcome from the captured job, and reject old reports without changing historical evidence.
- Job attempts have unique identities. Report continuations use the invoking executable and actually queried report, with `--expect-attempt` correlation guards and explicit reconstruction references when exact argv exceed the summary budget.
- Help includes ordered merges with blanks, styled inline templates, and check/query/build guidance. Command errors omit irrelevant lifecycle fields; structured recovery and report-write facts explain safe repair, including the early-failure overwrite exception.

### Added

- `schema response` describes structured responses and recovery guidance alongside the complete-report schema.

### Internal

- Structural gates now enforce physical source limits across owned tests, fixtures, generated and tagged files, with no grandfathering. Compiler-selected checks cover all six targets; rejection controls close trailing-comment, nested-directory and source-symlink discovery bypasses. The exported-type limit includes aliases and generic types.
- Mutation integration preserves access to the verified development checker in clean snapshots. The pinned executor retains complete receipts across native exit transitions and uses one bounded cleanup deadline without suppressing unknown or live-process errors.
- Lint-exception enforcement rejects unregistered mapped exemptions, disabled default error checks, predicate collisions, duplicate scopes, and unrepresentable cross-OS exclusions. Checker identity is validated against the pinned native source variant.

## [0.1.0] - 2026-10-07

- First release.
