# Contributing

## Get started

Prerequisites: Go (the version in the `go` directive of `go.mod`, the one source of the toolchain version), Git, a C compiler supported by Go's race detector when building the patched development tools on race-supported platforms, and Python 3 (a documentation test byte-compiles the documented Python recipe and fails without it). For the independent PDF checks, qpdf and Poppler are optional locally: tests that need them skip when they are missing and fail when `PDFCONCAT_REQUIRE_QA_TOOLS=1` is set, as CI does; `tools/versions.env` states the minimum versions.

```text
git clone https://github.com/resoltico/pdfconcat
cd pdfconcat
go test ./...
go build -o ./bin/ ./cmd/pdfconcat
./bin/pdfconcat version
go run ./tools/installtools        # development tools, into the ignored .tools/bin
```

`tools/versions.env` is the one place the development tools are pinned (golangci-lint, GoReleaser, govulncheck, gremlins, actionlint, Gitleaks, and the minimum qpdf/Poppler versions). `go run ./tools/installtools [name...]` installs them into `.tools/bin` (add it to `PATH`, or let `qualitygate` find it there):

- golangci-lint is built from its pinned upstream Go module archive, verified against the Go checksum database and the pinned module/archive digests, with the reviewed [physical-source attribution patch](tools/lint-patches/golangci-lint-physical-source.patch). The upstream `go.mod` and `go.sum` remain unchanged; dependency download, verification, parser tests and build use that exact graph in an isolated directory. Git applies the digest-checked patch. The build uses the installed Go toolchain (`GOTOOLCHAIN=local`), with ambient workspaces and private checksum exclusions disabled. A fresh source build needs access to the public Go module proxy and checksum database.
- The linter installer runs the physical parser tests with `-race`, which requires a C compiler. Go has no Windows/arm64 race detector, so ordinary parser tests remain mandatory there. The distinct linter version/cache identity derives from the upstream version and full patch digest; `lint-config` rejects the stock unpatched binary. Its date metadata explicitly names the upstream source-tag epoch, not the actual build time. See [the development linter source and license](tools/lint-patches/README.md).
- GoReleaser comes from its official release archive. The archive's SHA-256 must match the release's checksums file, and when the GitHub CLI is installed `gh release verify-asset` checks the release attestation too. Without `gh` only the digest is checked, and the installer says so.
- gremlins is built from its checksum-verified pinned upstream module with the reviewed [executor patch](tools/mutation-patches/README.md). Its upstream dependency files remain byte-for-byte unchanged. The installer runs the execution, coverage, engine, worker-pool, work-directory and report package tests with `-race` on supported targets and ordinary tests on Windows/arm64. The mutation gate requires the exact patch-derived version, module/main-package identity and native target; the upstream release binary is rejected. The patch preserves test-worker failures as errors, owns worker process cleanup, and records complete discovery/execution evidence. Baseline and workers use private Go caches with a post-command 4 GiB trim trigger and one-command growth overshoot; the global cache is not cleaned. Its source and Apache license remain separate from runtime archives.
- Gitleaks comes from its official release archive. Its pinned checksum-manifest SHA-256 and the selected archive digest must both match; its mutable upstream release has no immutable attestation. `go run ./tools/installtools gitleaks` followed by `go run ./tools/qualitygate secrets` checks intended source and reachable Git history with default scanner rules and full redaction. Inline allow comments and ignore fingerprints are disabled; no secret exceptions are configured. The reusable secret-hygiene check is a mandatory baseline CI dependency of source publication. Scanner detection does not cover every possible credential format.
- govulncheck and actionlint are built with `go install module@version`, which verifies every module against the Go checksum database. They publish no attested archives.

CI runs the same linter source installer and derives its expected identity from `tools/versions.env`; `qualitygate lint-config` verifies the native module/main-package metadata, complete version/patch/source-time identity, and checked-in patch digest before running diagnostic controls. These checks identify the intended source variant; the checksum-verified installer establishes its source provenance. The linter is a development tool with its upstream GPL license, separate from PDFConcat's MPL source and runtime components. Installing it does not change PDFConcat's runtime dependencies or offline operation.

Local application builds go in ignored `bin/`; on Windows, run `.\bin\pdfconcat.exe version` instead of `./bin/pdfconcat version`. Go creates the output directory when needed. Installed development tools stay in `.tools/bin/`, GoReleaser outputs stay in `dist/`, and test/coverage executables and CI smoke binaries stay in isolated temporary directories. The `go build ./...` commands below check compilation without producing a local application executable.

## Gates

Baseline CI runs compilation, tests, race checks, lint, coverage, dependency, vulnerability, secret and workflow checks on the configured runners. The separate `Campaigns` workflow is started explicitly with `workflow_dispatch`; it retains fuzzing, mutation testing, mutation negative controls and scale acceptance. A green baseline run does not establish campaign results. Run the checks relevant to a change locally. `go run ./tools/qualitygate` with no arguments lists the commands it provides.

```text
go build ./... && go vet ./...
go run ./tools/qualitygate test                  # go test ./... with discovery checks; add -race
go run ./tools/qualitygate format                # formatting, including owned fixtures; never rewrites files
go run ./tools/qualitygate lint                  # configured application version + every owned compiled package, including fixtures
go run ./tools/qualitygate secrets               # intended source and all reachable Git history, redacted
go run ./tools/qualitygate architecture          # classified production files across six target variants
go run ./tools/qualitygate lint-config           # .golangci.yml agrees exactly with .quality-exceptions.yml
go run ./tools/qualitygate lint-stale            # every diagnostic exclusion still matches a real diagnostic
go run ./tools/qualitygate coverage              # unit + executable coverage, merged, threshold enforced
go run ./tools/qualitygate fuzz -time 30s        # every Fuzz target found by `go test -list`
go run ./tools/qualitygate mutation              # gremlins in a clean snapshot (slow)
go run ./tools/qualitygate controls              # deliberate defects the tests must detect
govulncheck ./...
go mod verify && go mod tidy -diff
go run ./tools/qualitygate release-version -snapshot # canonical application version from internal/app/version.txt
goreleaser check && goreleaser release --snapshot --clean && go run ./tools/qualitygate archives dist
actionlint
```

Each gate runs independently. For a quick local build, run `go run ./tools/qualitygate lint` followed by `go build -o ./bin/ ./cmd/pdfconcat`; this does not start fuzzing or mutation testing. Add `lint-config` and `lint-stale` when checking lint policy and exception premises. A local build with selected checks does not establish full verification or change the required CI and release checks.

The test gate streams complete JSON events and separate process stderr to external temporary files. Failed runs retain both paths; successful runs remove temporary evidence. Use `test -events /outside/checkout/events.jsonl` to retain every run explicitly (the path must be new); stderr is saved at the same path with `.stderr` appended. Parsing failures still drain the child output, preserving diagnostic bytes without holding the complete log in memory.

What each proves, and how it can fail without a code defect:

- `test` runs every package (except `./test/scale`, which has its own job) as `go test -json` and fails if a package with test files passes no test, if a required test did not run, or if a package does not pass. Skipped tests are listed, not hidden.
- `lint-config` and `lint-stale` are described under Exceptions. `lint-stale` needs a tree that type-checks: with a compile error no linter runs and it refuses to judge.
- `coverage` measures statement coverage of the packages linked into the executable. Tests that run the executable must build it with `internal/exectest` (`exectest.Build`, `exectest.Command`): under the gate that builds with `go build -cover` and collects `GOCOVERDIR` data, which is merged with the unit profile (`go tool covdata textfmt`). A run that produced no executable coverage fails. The threshold is `coverage_threshold_percent` in `.quality-exceptions.yml`; it requires 100% of the reviewed reachable denominator, with exact central exceptions, and must not be lowered to pass. Raw and reachable percentages are printed separately; `-profile FILE` keeps the merged profile and a per-function report. Platform-specific files count only in their own OS's run.
- `fuzz` requires a macOS or Linux runner: other platforms fail before launching a fuzz subprocess because owned descendant cleanup is not established there. Each selected target must actually run and pass; zero targets, missing/skipped targets and incomplete event streams fail. `-time` must be a positive duration or iteration count (for example, `30s` or `1x`). Cancellation and normal completion terminate inheriting Go workers before reaping their process-group leader; Linux requires readable native `/proc` process-state metadata; unavailable or unknown state fails cleanup. Deliberately detached processes or workers that leave their process group/session are unsupported. JSON events and stderr are retained on failure. CLI fuzz inputs encode OS argument vectors losslessly with a NUL separator, including empty arguments and control characters. Run longer locally with `-time 5m`; a failing input is written under the package's `testdata/fuzz` and becomes a regression test when committed.
- `mutation` copies tracked and unignored files into a temporary snapshot, so it never mutates the working tree, and runs gremlins with every operator. Every discovered host mutant is executed, including those the coverage dry run marks uncovered. A final uncovered judgment fails; surviving or timed-out mutants fail unless a precise registry entry accepts them; a tool failure is a failure, never a pass. The denominators (killed, lived, not covered, timed out, not viable, other-platform files not judged, accepted) are printed. Mutants in files the host does not compile are judged by that platform's own run. Integration judging is enabled by default, including executable subprocess tests: all runtime packages belong to the command import graph, so changing runtime code invalidates its Go test cache. `-integration=false` is a targeted debugging mode and does not satisfy the full campaign. Integration campaigns validate the selected checker once and propagate its directory through child `PATH`, so clean snapshots can run required real-linter tests without copying ignored tools. Registry contents, module identity, pins, patch bytes and checker configuration come from the frozen snapshot; installed executables are located in the original tree or `PATH`. Registry path/function/anchor freshness is checked once on that clean snapshot before tool setup or discovery; registry validator rejection tests remain in the integration suite, while repository-instance freshness is owned by `lint-config` and the mutation preflight. Ordinary `go test ./...` does not independently audit those real-tree anchors. Snapshots reject listed symlinks/nonregular inputs, omitted owned Go sources, and omitted or changed native build/test embedding inputs discovered by Go. Tracked working-tree deletions remain absent, and ignored build artifacts remain excluded. Arbitrary dynamically read non-Go test fixtures must be tracked or unignored; compiler metadata cannot discover such implicit ignored inputs. `-timeout-coefficient` must be positive (default 25); zero does not select an upstream fallback. `-workers` counts actual execution workers (1 through 4, default 2), including in integration mode; the controller translates this to the pinned tool's pool input. `-max-duration` is an optional total command budget (default 0, unlimited), charged from command entry through setup, discovery and execution. Positive values must exceed the reserved 60 seconds for worker drain and cleanup; each tool invocation receives the remaining work time. `-max-duration=2h` applies a two-hour command budget without imposing that limit on other campaigns. Cancellation forwards Interrupt on POSIX and permits a bounded drain before forced termination. Windows uses the tool's internal deadline for budget expiry; direct Interrupt forwarding is unsupported, and native parent-cancellation rollback/cleanup remains a Windows verification prerequisite. Discovery is checked against the pinned tool’s dry run, including exact files, positions, and operators; incomplete reports cannot pass.
- `controls` applies each deliberate defect in `tools/mutation-controls.yml` (a text-anchored one-line change) to a snapshot and requires the package's tests to fail. When a refactor moves an anchor, the control reports the missing or ambiguous anchor; update the anchor, keep the property.
- Run scale acceptance with `PDFCONCAT_SCALE=1 PDFCONCAT_REQUIRE_QA_TOOLS=1 go run ./tools/qualitygate test -run '^TestScaleAcceptance$' -require TestScaleAcceptance -timeout 30m ./test/scale`. The manually dispatched Campaigns job enables both variables and preserves results. Set `PDFCONCAT_SCALE_RESULTS` to save JSON rows. Missing RSS/descriptor readings or failed intermediate samples fail enabled acceptance. Descriptor counts are sampled lower bounds; results include successful/attempted samples and the observed span divided by wall time. Unix RSS uses launch-through-exit kernel accounting; Windows RSS is a sampled peak lower bound. Fixtures and independent verification are timed separately.

A workflow file is configuration, not execution evidence. Retain the actual native workflow results and skipped/unavailable prerequisites for the candidate under review.

Format with `golangci-lint fmt --config .golangci.yml` (gofumpt, gci, goimports, golines). Build-tagged files are analyzed only for the host OS; CI lints on each OS.

## Scope and contracts

PDFConcat does one thing: assemble existing PDFs in an explicit order and insert generated blank pages at explicit positions. [`docs/DESIGN.md`](docs/DESIGN.md) explains current scope and design choices; read it before proposing a feature.

These documents are the product contracts: [`docs/CLI.md`](docs/CLI.md), [`docs/PLAN.md`](docs/PLAN.md), [`docs/BLANK_PAGES.md`](docs/BLANK_PAGES.md), and [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), together with the plan JSON Schema at `internal/plan/plan.schema.json`. A behavior change updates the relevant contract and its tests together.

Invariants worth knowing before you change code:

- Assembly uses a shared resource pool and a fixed number of passes. Blanks are never inserted one at a time into an already-merged file, because jobs run to thousands of sources and blanks.
- Sources and plan files are never modified. An output is published only after final validation and an exact page-count check, using the platform's native no-clobber rename.
- pdfcpu runs stateless and offline, with merge bookmarks disabled, and is imported only by `internal/pdfengine`. Nothing silently falls back to Ghostscript, qpdf, or another engine.
- macOS, Windows, and Linux share the supported behavior contract; OS-specific primitives are isolated in build-tagged files and require native verification.
- No implicit directory scanning or globbing, and one syntax per concept.

## Fidelity and diagnostic verification

Use synthetic PDFs and independent oracles. A valid file and correct page count do not establish correct form appearance, ordinary editability, signature policy or visible generated text. Form resource tests need differently bound fonts under the same name, inherited defaults, existing/missing appearances and independent same/changed-value refill rendering. Signature tests need signed-byte integrity controls and refusal before publication, with empty unsigned fields and text/name decoys as acceptance controls.

Keep schema, decoder, builder, summary and query contracts synchronized. Format 3 diagnostics carry error/warning severity; counts describe aggregated records. A query can succeed while selecting historical errors. Test error-only, warning-only and mixed saved runs, predicted/committed effects, consumer projections, bounded previews and complete diagnostic traversal. Use measured placement findings and reachable PDF object facts rather than duplicate geometry math or raw string searches.

Recovery tests must retry a remedy that repairs the named cause. A distinct safe report remains writable when the PDF target is unusable, while report/input/output aliases and unknown-inventory no-clobber safeguards still reject. After PDF commit, retain actual publication and owned report recovery; cancellation and failed stdout cannot imply rollback.

Keep work bills, fixtures generated for audits, binaries, credentials and raw measurement/campaign evidence outside commits. Reusable deterministic fixtures and failure-detecting controls belong in tests. Campaign commands below describe available checks; run fuzzing/mutation campaigns only when explicitly authorized, and never report an unrun or partial campaign as passed.

## Exceptions

`.quality-exceptions.yml` is the only place an exception to lint, coverage or mutation testing may be written. There are no inline `//nolint`, `#nosec`, `//lint:ignore` or similar comments (a test parses every Go file's comments and rejects them), and `.golangci.yml` carries no exception that the registry does not list.

The owned-source check rejects every Go file above 1,000 physical code-bearing lines or 20 exported-name type declarations, including aliases and generic types. Comments and blank lines do not count; trailing comments cannot hide code, and compiler display directives do not change physical positions. Tests, fixtures, generated sources and every build-tag variant are checked without grandfathering. The two flawed native revive algorithms are disabled in the central registry; their unchanged configuration limits remain checked against the mandatory owned-source authority. Review responsibilities and split coherent concerns before either limit, instead of mechanically moving lines.

Structural analyzers also run over all six compiler-selected target scopes. Native import and function-boundary controls verify rejection, including exactly 40 statements/60 lines accepted and 41/61 rejected. These metrics constrain size and complexity; they cannot prove semantic cohesion or an absence of god objects. Owned directory symlinks and files excluded from every supported target are rejected rather than silently omitted. Artifact-directory exclusions apply only at the repository root.

Fix the code first. A genuine exception is a registry entry with a stable id, the `tool`, a justification `kind`, the `retained_property` it still protects, the fields its tool needs, and a YAML comment directly above the entry explaining why repair is inappropriate. That comment is the rationale; there is no second inventory. Paths are exact files, never globs or directories; a lint diagnostic exclusion needs a message pattern and may add a source-line pattern. Duplicate effective lint scopes are rejected. A lint `goos` restriction requires compiler selection to exclude that file on every other supported OS, because the native configuration rule itself has no OS predicate. Setting items are scalar values; active structured suppression lists such as errorlint allowed-error mappings are rejected because the registry cannot express their complete predicates. Mutation entries require a unique trimmed source anchor and the exact positive physical byte `column` of its operator, including indentation and UTF-8 bytes; fresh discovery establishes the operator type. Unjudged `NOT COVERED` outcomes cannot be excepted.

- golangci-lint entries (`effect`): `disable-linter`, `setting-item` (an item in an ignore, disable or skip list), `exclude-diagnostic`. Lint configuration preserves the complete formatter set independently of list order, forbids `issues.fix`, and rejects custom gofmt rewrite rules so checking inputs and safe formatting cannot become semantic repairs. Explicit native exemptions such as escape hatches and whitelists require central registry entries. After editing the registry, `go run ./tools/qualitygate lint-config -print-rules` prints the exclusion `rules:` block `.golangci.yml` must carry; `lint-config` fails on any missing, extra or broader copy, including disable lists, presets, paths, generated-file handling and settings-level ignore lists. `lint-stale` fails when a diagnostic exclusion matches no real diagnostic; `lint-config` checks the premise of the others (a deprecated linter is still deprecated, a linter an incompatible or duplicate rule yields to is still enabled).
- coverage entries name a file, a function and the exact first line of an unreachable block (kinds `unreachable-branch`, `unreachable-platform-branch` with `goos`, `generated-code`). The gate fails if the block is in fact executed or no longer exists.
- mutation entries name a file, an operator, the mutated line's exact text and the accepted statuses (`equivalent-mutant`, `out-of-domain-mutant`). The gate fails if no such mutant survives.

Strictness settings (thresholds, `enable-all` switches, pinned defaults) are not exceptions and stay in `.golangci.yml`; the numeric thresholds there each state their reason. Do not run `golangci-lint run --fix` across all linters: some autofixers reorder struct fields or change semantics. `golangci-lint fmt --config .golangci.yml` is safe.

Native lint, format, configuration verification, linter listing and source controls select their authoritative configuration by an explicit absolute `--config` path. An adjacent `.golangci.yaml`, `.golangci.json` or ambient configuration cannot override that selection.

`.golangci.yml` is the sole import-policy authority: depguard enforces external library ownership and production dependency direction. `qualitygate architecture` derives classification from its production rules, requires every owned production file to belong to exactly one responsibility, and checks all supported target variants without duplicating the edge table. A new package needs deliberate classification even if it imports no internal package. Tests and support policies preserve independent oracles; production code cannot import test helpers. New dependencies need `go.mod`, `THIRD_PARTY_NOTICES.md`, a license copy under `third_party/licenses`, and an entry in the module allow-list. A test compares the notices with the modules linked into each release binary, their versions, and the license files of those module versions.

## Changes

Keep changes focused. A behavioral change normally includes the implementation, focused tests, the updated contract documents (a test fails if the plan schema and decoder drift apart, and if documented options or links go stale), and an `Unreleased` entry in [`CHANGELOG.md`](CHANGELOG.md) when users are affected.

Use synthetic PDFs as fixtures (`internal/pdffixture`), never confidential documents. Example plans live in `examples/plans`; a test decodes every one of them and builds the ones that need no source files, so an example cannot drift from the plan format.

## Dependencies

Prefer the standard library. A runtime dependency needs a concrete product or platform need, a pinned version in `go.mod`, and an update to `THIRD_PARTY_NOTICES.md`. Development tools are pinned in `tools/versions.env` and installed with `go run ./tools/installtools`; they are not module dependencies.
