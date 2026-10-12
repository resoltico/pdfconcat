# Contributing

## Get started

Prerequisites: Go (the version in the `go` directive of `go.mod`, the one source of the toolchain version), Git, a native C compiler for the Darwin descriptor sampler, Unix capped scale launcher and patched development-tool tests on race-supported platforms, and Python 3 (a documentation test byte-compiles the documented Python recipe and fails without it). For the independent PDF checks, qpdf and Poppler are optional locally: tests that need them skip when they are missing and fail when `PDFCONCAT_REQUIRE_QA_TOOLS=1` is set, as CI does; `tools/versions.env` states the minimum versions.

```text
git clone https://github.com/resoltico/pdfconcat
cd pdfconcat
go test ./...
go build -o ./bin/ ./cmd/pdfconcat
./bin/pdfconcat version
go run ./tools/installtools        # development tools, into the ignored .tools/bin
```

`tools/versions.env` is the one place the development tools are pinned (golangci-lint, GoReleaser, govulncheck, gremlins, actionlint, Gitleaks, and the minimum qpdf/Poppler versions). `go run ./tools/installtools [name...]` installs them into `.tools/bin` (add it to `PATH`, or let `qualitygate` find it there):

- golangci-lint, govulncheck and actionlint are pinned stock Go modules installed with `go install` through the public module proxy and checksum database. The native linter command/package/module/version metadata is checked independently of its display version.
- GoReleaser comes from its official release archive. The archive's SHA-256 must match the release's checksums file, and when the GitHub CLI is installed `gh release verify-asset` checks the release attestation too. Without `gh` only the digest is checked, and the installer says so.
- gremlins is built from its checksum-verified pinned upstream module with the reviewed [executor patch](tools/mutation-patches/README.md). Its upstream dependency files remain byte-for-byte unchanged. The installer runs the execution, coverage, engine, worker-pool, work-directory and report package tests with `-race` on supported targets and ordinary tests on Windows/arm64. The mutation gate preserves native caller report paths and fails on task-owned snapshot cleanup errors. The mutation gate requires the exact patch-derived version, module/main-package identity and native target; the upstream release binary is rejected. The patch preserves test-worker failures as errors, owns worker process cleanup, and records complete discovery/execution evidence. Baseline and workers use private Go caches with a post-command 4 GiB trim trigger and one-command growth overshoot; the global cache is not cleaned. Its source and Apache license remain separate from runtime archives.
- Gitleaks comes from its official release archive. Its pinned checksum-manifest SHA-256 and the selected archive digest must both match; its mutable upstream release has no immutable attestation. `go run ./tools/installtools gitleaks` followed by `go run ./tools/qualitygate secrets` checks intended source and reachable Git history with default scanner rules and full redaction. Inline allow comments and ignore fingerprints are disabled; no secret exceptions are configured. The reusable secret-hygiene check is a mandatory baseline CI dependency of source publication. Scanner detection does not cover every possible credential format.

CI installs the same pinned stock linter. `qualitygate lint-config` checks native command/module/version metadata, the explicit correctness profile, selected SDK memory-writer contracts and real diagnostic controls. The physical source guard rejects display-mapping ownership escapes. Provenance identifies the tool inputs; correctness still depends on the compiled controls and project tests.

Local application builds go in ignored `bin/`; on Windows, run `.\bin\pdfconcat.exe version` instead of `./bin/pdfconcat version`. Go creates the output directory when needed. Installed development tools stay in `.tools/bin/`, GoReleaser outputs stay in `dist/`, and test/coverage executables and CI smoke binaries stay in isolated temporary directories. The `go build ./...` commands below check compilation without producing a local application executable.

## Gates

Baseline CI runs compilation, tests, race checks, lint, coverage, dependency, vulnerability, secret and workflow checks on the configured runners. Baseline CI requires discovered-target fuzz smoke. The reusable `Campaigns` workflow retains fuzzing, mutation testing, mutation negative controls and scale acceptance and is a required source-release dependency for the exact commit. It can also be dispatched separately. A verification-only source-release dispatch runs baseline/campaigns without publication. A green baseline run alone does not establish mutation or scale results. Run the checks relevant to a change locally. `go run ./tools/qualitygate` with no arguments lists the commands it provides.

```text
go build ./... && go vet ./...
go run ./tools/qualitygate test                  # baseline tests with discovery checks; add -race
go run ./tools/qualitygate format                # formatting, including owned fixtures; never rewrites files
go run ./tools/qualitygate lint                  # configured application version + every owned compiled package, including fixtures
go run ./tools/qualitygate secrets               # intended source and all reachable Git history, redacted
go run ./tools/qualitygate architecture          # classified production files across six target variants
go run ./tools/qualitygate lint-config           # explicit stock profile, physical source guards and compiled negative controls
go run ./tools/qualitygate coverage -evidence /path/to/new-attempt # merged raw unit + executable coverage; no exclusions or percentage gate
go run ./tools/qualitygate fuzz -time 30s        # compiler-recognized targets checked against package-attributed Go list events
go run ./tools/qualitygate mutation              # gremlins in a clean snapshot (slow)
go run ./tools/qualitygate controls              # deliberate defects the tests must detect
govulncheck ./...
go mod verify && go mod tidy -diff
go run ./tools/qualitygate release-version -snapshot # canonical application version from internal/app/version.txt
goreleaser check && goreleaser release --snapshot --clean && go run ./tools/qualitygate archives dist
command -v shellcheck >/dev/null
actionlint -shellcheck 'shellcheck --check-sourced'
```

Workflow checks run from the checkout root and require ShellCheck on `PATH`; `--check-sourced` includes the owned CI shell scripts in the stock actionlint check.

Each gate runs independently. For a quick local build, run `go run ./tools/qualitygate lint` followed by `go build -o ./bin/ ./cmd/pdfconcat`; this does not start fuzzing or mutation testing. Add `lint-config` when checking the correctness profile and compiled negative controls. A local build with selected checks does not establish full verification or change the required CI and release checks.

Hosted mutation campaigns partition the complete sorted native runtime-file inventory across three disjoint jobs per host. Each job retains full integration judging, has a measured five-and-a-half-hour work budget plus setup/upload margin within the six-hour runner limit, and uploads the full source-hash inventory and selected partition. Every partition must pass; a partial matrix cannot certify the corpus. Local `mutation` defaults to the whole inventory (`-partition=0/1`); use all `INDEX/COUNT` values when distributing a campaign.

The test gate streams complete JSON events and separate process stderr to external temporary files. Failed runs retain both paths; successful runs remove temporary evidence. Use `test -events /outside/checkout/events.jsonl` to retain every run explicitly (the path must be new); stderr is saved at the same path with `.stderr` appended. Parsing failures still drain the child output, preserving diagnostic bytes without holding the complete log in memory.

What each proves, and how it can fail without a code defect:

- `test` runs every package (except `./test/scale`, which has its own job) as `go test -json` and fails if a package with test files passes no test, if a required test did not run, or if a package does not pass. Skipped tests are listed, not hidden.
- `lint-config` verifies the explicit correctness profile, stock native tool identity, real security/error controls and physical source ownership.
- `coverage` measures statement coverage of the packages linked into the executable. Tests that run the executable must build it with `internal/exectest` (`exectest.Build`, `exectest.Command`): under the gate that builds with `go build -cover` and collects `GOCOVERDIR` data, which is merged with the unit profile (`go tool covdata textfmt`). A run that produced no executable coverage fails. There is no global percentage gate or excluded denominator. Raw coverage and statement counts remain visible; `-evidence DIRECTORY` requires a new caller-owned attempt directory and retains its start/outcome manifest, raw JSON events/stderr, unit/executable/native data and validated merged/function output on success and failure. CI uploads that directory for 14 days; catastrophic runner loss can still prevent upload. An incomplete attempt never represents authoritative total coverage. Critical invariant matrices, mutation controls and independent PDF oracles remain required. Platform-specific files count only in their own OS's run.
- `fuzz` requires a macOS or Linux runner: other platforms fail before launching a fuzz subprocess because owned descendant cleanup is not established there. Each selected target must actually run and pass; zero targets, missing/skipped targets and incomplete event streams fail. Discovery compares package-attributed `go test -json -list` events with the compiler-generated fuzz target tables from `go list -compiled -test -json`; misleading or suppressed `TestMain` output and unavailable compiler evidence fail. `-time` must be a positive duration or iteration count (for example, `30s` or `1x`). Cancellation and normal completion terminate inheriting Go workers before reaping their process-group leader; Linux requires readable native `/proc` process-state metadata; unavailable or unknown state fails cleanup. Deliberately detached processes or workers that leave their process group/session are unsupported. JSON events and stderr are retained on failure. CLI fuzz inputs encode OS argument vectors losslessly with a NUL separator, including empty arguments and control characters. Run longer locally with `-time 5m`; a failing input is written under the package's `testdata/fuzz` and becomes a regression test when committed.
- `mutation` copies tracked and unignored files into a temporary snapshot, so it never mutates the working tree, and runs gremlins with every operator. Every discovered host mutant is executed, including those the coverage dry run marks uncovered. A final uncovered judgment fails; surviving or timed-out mutants fail unless a precise registry entry accepts them; a tool failure is a failure, never a pass. The denominators (killed, lived, not covered, timed out, not viable, other-platform files not judged, accepted) are printed. Mutants in files the host does not compile are judged by that platform's own run. Integration judging is enabled by default, including executable subprocess tests: all runtime packages belong to the command import graph, so changing runtime code invalidates its Go test cache. `-integration=false` is a targeted debugging mode and does not satisfy the full campaign. Integration campaigns validate the selected checker once and propagate its directory through child `PATH`, so clean snapshots can run required real-linter tests without copying ignored tools. Registry contents, module identity, pins, patch bytes and checker configuration come from the frozen snapshot; installed executables are located in the original tree or `PATH`. Registry path/operator/anchor/column freshness is checked once on that clean snapshot before tool setup or discovery; registry validator rejection tests remain in the integration suite, while repository-instance freshness is owned by the mutation preflight. Ordinary `go test ./...` does not independently audit those real-tree anchors. Snapshots reject listed symlinks/nonregular inputs, omitted owned Go sources, and omitted or changed native build/test embedding inputs discovered by Go. Tracked working-tree deletions remain absent, and ignored build artifacts remain excluded. Arbitrary dynamically read non-Go test fixtures must be tracked or unignored; compiler metadata cannot discover such implicit ignored inputs. `-timeout-coefficient` must be positive (default 25); zero does not select an upstream fallback. The baseline requires the verified checker on PATH. With `-integration=false` and one `-only` package, discovery and execution both test that validated package as their baseline; scanner and report paths remain relative to the repository root. Default, integration and multiple-package baselines run the whole module. `-workers` counts actual execution workers (1 through 4, default 2), including in integration mode; the controller translates this to the pinned tool's pool input. `-max-duration` is an optional total command budget (default 0, unlimited), charged from command entry through setup, discovery and execution. Positive values must exceed the reserved 60 seconds for worker drain and cleanup; each tool invocation receives the remaining work time. `-max-duration=2h` applies a two-hour command budget without imposing that limit on other campaigns. Cancellation forwards Interrupt on POSIX and permits a bounded drain before forced termination. Windows uses the tool's internal deadline for budget expiry; direct Interrupt forwarding is unsupported, and native parent-cancellation rollback/cleanup remains a Windows verification prerequisite. Discovery is checked against the pinned tool’s dry run, including exact files, positions, and operators; incomplete reports cannot pass.
- `controls` applies each deliberate defect in `tools/mutation-controls.yml` (a text-anchored one-line change) to a snapshot and requires the package's tests to fail. When a refactor moves an anchor, the control reports the missing or ambiguous anchor; update the anchor, keep the property.
- Run scale acceptance with `PDFCONCAT_SCALE=1 PDFCONCAT_REQUIRE_QA_TOOLS=1 go run ./tools/qualitygate test -run '^TestScaleAcceptance$' -require TestScaleAcceptance -timeout 30m ./test/scale`. The reusable Campaigns job enables both variables and preserves results. Set `PDFCONCAT_SCALE_RESULTS` to save JSON rows. Missing RSS/descriptor readings or failed intermediate samples fail enabled acceptance. Darwin compiles a private libproc descriptor collector before the measured interval; native `ESRCH` is terminal only with matching owned-process exit evidence, and other native/protocol/process failures remain errors. Its buffer grows from 16 to at most 65,536 entries; exhausting that budget fails explicitly. Descriptor counts are sampled lower bounds; results include successful/attempted samples and the observed span divided by wall time. Unix RSS uses launch-through-exit kernel accounting; Windows RSS is a sampled peak lower bound. Fixtures and independent verification are timed separately.

A workflow file is configuration, not execution evidence. Retain the actual native workflow results and skipped/unavailable prerequisites for the candidate under review.

Format with `golangci-lint fmt --config .golangci.yml` (gofumpt). Build-tagged files are analyzed only for the host OS; CI lints on each OS.

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

Keep schema, decoder, builder, summary and query contracts synchronized. Format 2 diagnostics carry error/warning severity; counts describe aggregated records. A query can succeed while selecting historical errors. Test error-only, warning-only and mixed saved runs, predicted/committed effects, consumer projections, bounded previews and complete diagnostic traversal. Use measured placement findings and reachable PDF object facts rather than duplicate geometry math or raw string searches.

Recovery tests must retry a remedy that repairs the named cause. A distinct safe report remains writable when the PDF target is unusable, while report/input/output aliases and unknown-inventory no-clobber safeguards still reject. After PDF commit, retain actual publication and owned report recovery; cancellation and failed stdout cannot imply rollback.

Keep work bills, fixtures generated for audits, binaries, credentials and raw measurement/campaign evidence outside commits. Reusable deterministic fixtures and failure-detecting controls belong in tests. Campaign commands below describe available checks; run fuzzing/mutation campaigns only when explicitly authorized, and never report an unrun or partial campaign as passed.

## Exceptions

`.quality-exceptions.yml` records precise mutation judgments, with a stable id, exact physical source anchor/column, operator, resolved accepted statuses, rationale and retained observable property. Unjudged `NOT COVERED` outcomes cannot be excepted. The gate validates references before discovery and requires surviving mutants to match their judgments.

The stock correctness profile is explicit: govet, staticcheck, errcheck, ineffassign, unused, gosec, depguard and nolintlint. Use one formatter, gofumpt. Narrow exact-file diagnostic suppressions live in `.golangci.yml`; their reasons and the disposition of replaced enforcement are in [Verification](docs/VERIFICATION.md). Broad folder exclusions and source-regex suppressions are unsupported. Active inline suppressions and compiler display directives are rejected in owned source by a physical AST comment scan; directive strings in negative-control fixtures are supported. Build, embed and generate directives remain supported.

The owned-source check retains physical file-size and exported-type limits and complete target selection. Universal function complexity/style metrics are no longer mandatory. Review coherent responsibilities directly. Compiled import negative controls run over the supported target scopes; symlink ownership escapes, loader errors and files excluded from every target remain failures. Required native execution is distinct from cross-compilation.

Checks use `--fix=false`. Do not run arbitrary linter autofixers across the tree. Scope formatting to owned directories, for example `golangci-lint fmt --config .golangci.yml cmd internal tools test`; never format the maintained third-party trees.

Native lint, format, configuration verification, linter listing and source controls select their authoritative configuration by an explicit absolute `--config` path. An adjacent `.golangci.yaml`, `.golangci.json` or ambient configuration cannot override that selection.

`.golangci.yml` is the sole import-policy authority: depguard enforces external library ownership and production dependency direction. `qualitygate architecture` derives classification from its production rules, requires every owned production file to belong to exactly one responsibility, and checks all supported target variants without duplicating the edge table. A new package needs deliberate classification even if it imports no internal package. Tests and support policies preserve independent oracles; production code cannot import test helpers. New dependencies need `go.mod`, `THIRD_PARTY_NOTICES.md`, a license copy under `third_party/licenses`, and an entry in the module allow-list. A test compares the notices with the modules linked into each release binary, their versions, and the license files of those module versions.

## Changes

Keep changes focused. A behavioral change normally includes the implementation, focused tests, the updated contract documents (a test fails if the plan schema and decoder drift apart, and if documented options or links go stale), and an `Unreleased` entry in [`CHANGELOG.md`](CHANGELOG.md) when users are affected.

Use synthetic PDFs as fixtures (`internal/pdffixture`), never confidential documents. Example plans live in `examples/plans`; a test decodes every one of them and builds the ones that need no source files, so an example cannot drift from the plan format.

## Dependencies

Prefer the standard library. A runtime dependency needs a concrete product or platform need, a pinned version in `go.mod`, and an update to `THIRD_PARTY_NOTICES.md`. Development tools are pinned in `tools/versions.env` and installed with `go run ./tools/installtools`; they are not module dependencies.

For capped Unix scale checks, the harness compiles the private `test/scale/testdata/descriptor_launcher.c` before the measured interval. Darwin uses the installed SDK's same-PID SETEXEC and default close-on-exec inheritance with only stdio inherited; Linux uses close_range and checked /proc survivors, zero capabilities and no-new-privileges. Each receipt records actual compiler path/digest/version and SDK, helper source/executable digests, product identity, PID/token, limits and hard-raise refusal. Keep executable/input directories frozen and exclude privileged external limit changes. `CGO_ENABLED=0` project build/vet checks remain required: the C source is embedded testdata compiled only for native capped harness runs. A verified ceiling does not turn failed samples valid; partial/unavailable observations and original failed receipts remain separate from resource acceptance.

Darwin native progress fault verification requires the Xcode Command Line Tools C compiler and
`go run ./tools/qualitygate test -native-progress -events /path/to/new-events.json`. This required
Mac CI lane selects `progress_native` with CGO enabled, runs the real signal stimulus, interrupted
write retry and sandbox policy cases with race detection and atomic coverage, and retains parent
JSON events, child coverage and compiler identity beside the event file. Ordinary developer tests
omit this tag. The fixture package is test support and cannot enter the product dependency graph;
all six standalone release build/vet variants continue to use CGO disabled.

Coverage uses a CGO-disabled baseline. On Darwin it additionally runs this native fault lane and
merges its Go counts only after checking identical selected production source bytes and identical
coverage file, block, statement and atomic-mode inventories. These counts qualify the altered
signal fixture; they do not measure C statement coverage or certify ordinary signal delivery.
