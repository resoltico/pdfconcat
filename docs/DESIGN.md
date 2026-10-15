# Design and rebuild acceptance

PDFConcat assembles a declared packet of source PDFs and generated pages using
unmodified upstream pdfcpu. The rebuild follows WP0–WP10 from the owner-approved
2026-10-15 bundle, with the peer-review corrections. Acceptance below describes
work actually run; pending work is not a passing check.

## Preconditions (WP0)

On 2026-10-15, the working tree was clean. The foundation branch and its remote
both identified `56260859095ceac53c4455486f84e2ac3547ad76`; main identified
`a74cd6fbf6ffcb5934e034a54d071d496b5794f6`. Fetch and a second remote observation
confirmed those tips. Neither branch contains commits newer than the bundle's
snapshot, so there are no additional commits to classify. PR #1 remains open.
The latest existing CI run failed:
<https://github.com/resoltico/pdfconcat/actions/runs/38659639579>.

The app's chat inventory showed the previous PDFConcat writer unloaded and the
audit chat idle; this chat is the sole implementation writer. The branch is
`rebuild/upstream-pdfcpu`, based on the foundation tip. All owner decisions
D1–D10 apply: keep the project, plan v2/page, curated lint, umask permissions,
100,000 generated-page limit, remove progress/query/jobs/inline plans, supersede
PR #1, add the contributor-scope pointer, draft upstream issues only, and let the
owner tag the release.

Go 1.27.2 runs on macOS arm64. qpdf and Poppler are installed. The comparison
binary and diagnostic logs live outside the checkout at
`/Users/erst/Tools/pdfconcat-implementation-evidence/`. Owner artifacts `dist/`,
`bin/`, `.tools/`, `pdfconcat`, `plan.test`, and ignored filesystem metadata are
protected. No cleanup command may remove them.

Baseline verification: the old executable was built to the external evidence
directory; `go build ./...`, `go vet ./...`, and `go test ./...` passed on macOS
arm64 (23 package results, including one package without tests). No campaign or
opt-in scale test was requested. This local result does not resolve the existing
hosted CI failure or establish native Linux/Windows behavior.

## Sequencing decisions

WP1 removes obsolete CI, release and GoReleaser configurations when their
dependencies are removed; WP8 recreates them. Leaving broken configurations
until WP8 would contradict WP1's acceptance and permit obsolete campaigns.
Every WP still requires build, vet, tests and a commit.

WP5 native Linux/Windows acceptance remains pending until replacement CI runs.
WP8 opens the single authorized draft PR after committing replacement tooling,
observes its CI, then comments on and closes PR #1 as superseded. WP10 updates
that same PR and verifies the delivered state. All GoReleaser snapshots using
`--clean` run in isolated clones, preserving the owner's `dist/`.

## Deletion review (WP1)

Reviewed package responsibilities, exported boundaries, relevant tests, workflows
and release hooks against the approved deletion inventory. Git history retains
the removed implementations and regression sources for WP4 individual triage.

| Group removed | Protected property and replacement |
| --- | --- |
| Dependency forks, base archives and patches | Cancellation uses upstream context APIs; small PDF refusals/adapters and WP4 end-to-end regressions preserve relevant fidelity. Fork-specific resource budgets are explicitly retired. Licenses remain for WP9 review. |
| Capture and publish | Inspected/assembled byte identity becomes SHA-256 comparison; regular input checks and SameFile roles remain. Atomic publication retains no-clobber and durability. Native private ACLs, recovery copies, namespace-race and identity bookkeeping guarantees are intentionally removed. |
| Report | Stable outcomes and bounded stdout become result v3 plus a full report. Query language, continuations and retention machinery are removed. Report alias safety and late publication-state truth remain required. |
| App, assembly, plan, layout and CLI | Product contracts move to job, plan, pipeline and CLI. Path layering, origin pointers, first-fault locations, overflow and refusal behavior remain explicit acceptance cases. Worker scheduling and inline plans are removed. |
| PDF engine | Upstream merge/resize replaces the object pool, fitting parser and appearance engine. Named destinations, damage/signature/encryption refusals and catalog policy remain WP4 obligations; form and coordinate limitations require evidence and disclosure. |
| Progress, observation, native progress fixtures and command transport | Progress and its native console/backpressure guarantees are removed by D6. The executable uses ordinary streams and signal context. |
| Repository policy, documentation contract machinery and permission/subprocess helpers | Stock lint/depguard replaces repository policy. Small product documentation checks return in WP9; in-process CLI checks and two binary smoke tests replace subprocess coverage machinery. ACL fault guarantees are removed by D4. |
| Quality tools, mutation registry, installer and scale harness | Stock Go tooling replaces test discovery/supervision. Fuzz seeds remain ordinary tests; campaigns and mutation/coverage gates are removed. WP7 owns one opt-in measured scale case. |
| Workflows, composite actions, scripts, lint and release configuration | Their deleted dependencies make them obsolete. WP8 installs six-job CI, native tool invocation and isolated release snapshots. Required CI keeps its context. |
| Verification/closeout documents | Their verification-platform contracts are obsolete; CONTRIBUTING and delivered-state evidence replace them. Published changelog history remains. |

All generated-page writer tests were moved with the writer, rather than discarded.
All fixture, oracle and typesetting tests remain; the two oracle subprocess helper
calls use exec.CommandContext directly. Package names/imports follow the new
responsibilities. The minimal command intentionally refuses work until WP6.
Upstream pdfcpu v0.16.2 is still the latest stable module (requeried in WP1), so
there is no intervening changelog to reconcile. The initial configuration pins
that dependency through the actual library boundary.

## Contract decisions to verify before implementation

The peer audit supplies executable counterexamples outside this repository.
The following corrections must be reproduced before their affected changes.

- Integrate null rejection with strict decoding; a blanket null scan masks
  earlier faults. Accept one leading BOM and count its bytes in offsets. Detect
  v1 deliberately and explain both `version: 2` and `blank` → `page`.
- Required version omission maps to `plan_version_unsupported`; missing items
  to `plan_type`; empty items to `plan_value`. Wrong JSON types map to
  `plan_type`; noninteger/low count to `plan_value`; excessive count (including
  integer overflow) to `plan_limit`. Groups require dir and nonempty items.
  Warning classes are empty and cannot change outcome status.
- Standard JSON Schema cannot distinguish 3, 3.0 and 3e0. Lexical counts,
  duplicate members, encoding, converted dimensions, paths and aggregate limits
  remain decoder checks. Positive numeric font size/width bounds belong in
  schema too. Complete documentation examples must validate.
- Named plan/font/source inputs require Stat before open, then f.Stat before
  read. Follow input symlinks to regular files; stdin is the stream exception.
- Recheck report roles immediately before publication, including against an
  output that was initially absent. Unsafe report refusal remains invalid;
  attempted I/O failure is failed. Attempt safe reports after failed builds;
  never recursively report a report failure.
- Publication state is independent of Commit error. Set it after rename/link,
  before cleanup or directory sync, retain published bytes on later failure,
  and never let Abort undo publication.

## PDF boundaries to verify before implementation

- Walk raw page trees before validation, rejecting a Page with effective
  non-null Kids, including an empty array. Keep canonical dictionary-name
  identity protection with a narrow refusal where upstream double-decodes.
- Triage every fork regression individually. Preserve valid-input fidelity
  through small adapters/refusals; do not restore forks, appearance machinery
  or the fitting parser. Resource-exhaustion budgets are outside the accepted
  trusted-local-input threat model; ordinary API cancellation remains required.
- Prune forbidden name trees from both catalog and context caches. Reopen the
  serialized output and check its catalog. Close named/direct outlines, GoTo
  chains, repeated/distinct name trees and legacy destinations with oracles.
- Classify upstream password errors as source_encrypted even without a context.
  Synchronize cached effective version before validation/writing. Retain a
  warning for relative URI links whose catalog Base is removed.
- Upstream does not generally merge AcroForm DR and namespaces later fields
  even without collisions. Establish a narrow compatible boundary/refusal,
  report actual mappings, document limitations, and draft an upstream issue.
- Test minimal UserUnit normalization after Resize against physical geometry
  and pixels; otherwise explicitly refuse the unsupported case. Destination
  coordinates after fitting remain an explicit upstream limitation.
- Preserve bitmap-only font refusal, no-subsetting restrictions, and ActualText
  for glyph aliases and multi-glyph/multi-character clusters. Job-wide font
  identity includes widths and ToUnicode, not only the font program.

## Tooling and delivery boundaries

Build the pinned linter for the host once and run that binary under each target
GOOS; cross-building a module tool cannot execute it. Stock depguard also
enforces diag/job/typeset leaf boundaries. Keep six CI jobs and Required CI.
Compare fresh outputs with an evidence-preserving harness that asserts expected
exits; do not generate unused scale fixtures. Run only the authorized WP7 scale
case. Release readiness does not create tagged binaries or change @latest.
Changelog outcomes are relative to published v0.2.0: repeated named destinations
and damaged-tree refusal already shipped; repeated legacy Dests support is new.
Review actual linked licenses, NOTICE files and archive contents.

WP1 verification: `go build ./...`, `go vet ./...`, and `go test ./...` passed
on macOS arm64. All four salvaged package test suites ran; no tests were trimmed.
No replace directives remain. The forbidden-reference search is empty outside
history and documentation scheduled for WP9. Protected owner artifacts remain.
