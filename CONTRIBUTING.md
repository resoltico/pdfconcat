# Contributing

For commands and packet plans, start with the [README](README.md). This guide
covers development from a source checkout; binary release archives do not contain
the test sources or corpus.

- [Build and ordinary tests](#build-and-ordinary-tests)
- [Install a development command](#install-a-development-command)
- [Stock tools](#stock-tools)
- [Test scope](#what-we-test-and-what-we-do-not)
- [macOS setup verification](#macos-setup-verification)
- [Corpus investigations and regeneration](#permanent-corpus)
- [Optional scale measurements](#scale)
- [Release procedure](#release)

## Build and ordinary tests

Use Go at the version declared in [go.mod](go.mod). Shell snippets use bash/zsh;
adapt environment assignments and paths when using another shell. From a clean
checkout:

```sh
go build -o bin/pdfconcat ./cmd/pdfconcat
go vet ./...
go test -count=1 ./...
(cd tools && go mod tidy -diff)
```

Independent PDF tests use qpdf and Poppler (`pdftotext`, `pdftoppm`, `pdfinfo`,
`pdffonts`, `pdftohtml`). The minimum qpdf version is declared in
[`internal/oracle/qpdf.version`](https://github.com/resoltico/pdfconcat/blob/main/internal/oracle/qpdf.version);
it includes corrected linearization checking required by the frozen corpus.
Install `qpdf poppler` with Homebrew. On Linux, install `poppler-utils` with apt
and obtain a supported qpdf from the [official releases](https://github.com/qpdf/qpdf/releases)
if the distribution package is older. CI downloads that declared version's official
Linux binary with a pinned checksum in `.github/workflows/ci.yml`; updating the pin
requires updating its checksum too. These are test tools, not runtime dependencies.
Missing or unsupported tools skip their tests locally; set
`PDFCONCAT_REQUIRE_ORACLE=1` to require them. Linux CI does that.

```sh
PDFCONCAT_REQUIRE_ORACLE=1 go test -race -count=1 ./...
```

Race tests run on Linux/macOS; Windows runs ordinary tests. CI gates lint,
tests on those three operating systems, cross-builds, native macOS setup and
release packaging through `Required CI`. That aggregate name is a required repository
context; do not casually rename it.

## Install a development command

For development convenience, Go can install the latest published tag:

```sh
go install github.com/resoltico/pdfconcat/cmd/pdfconcat@latest
```

Use the Go version declared in [go.mod](go.mod). The executable goes into the
directory printed by `go env GOBIN`, or `bin` beneath the first directory printed
by `go env GOPATH` when GOBIN is empty. Use that executable's absolute path or
add its directory to your development shell's PATH.

Keep development executables separate from `/usr/local/bin/pdfconcat` and
`/usr/local/libexec/pdfconcat`, which belong to the
[macOS setup tool](docs/MACOS.md). A Go installation is not recorded or removed
by that tool. Check command resolution when testing alongside an installed
release; `go install` does not establish which copy a bare `pdfconcat` selects.

## Stock tools

Tool versions live in `tools/go.mod`. Build one native linter, then run that
executable with each target GOOS. Setting GOOS on `go tool` would cross-build the
tool itself and fail to execute it.

```sh
go build -modfile=tools/go.mod -o /tmp/pdfconcat-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint
for goos in linux darwin windows; do GOOS="$goos" /tmp/pdfconcat-lint run ./...; done
go tool -modfile=tools/go.mod govulncheck ./...
actionlint_version=$(go list -modfile=tools/go.mod -m -f '{{.Version}}' github.com/rhysd/actionlint)
go run "github.com/rhysd/actionlint/cmd/actionlint@$actionlint_version"
go mod tidy -diff
(cd tools && go mod tidy -diff)
```

Use a writable scratch executable path on Windows. Actionlint uses isolated
`@version` execution with its version derived from the tool module: its YAML API
is incompatible with the combined linter-tool dependency graph.
Actionlint uses shellcheck when it is available.

The compiler confines adapters and domain packages beneath
`internal/core/internal/`. Depguard confines third-party adapters and keeps file,
process and network I/O out of production CLI code. See
[architecture](docs/ARCHITECTURE.md) for the package graph.

Lint is standard plus a curated list. No inline lint suppressions are allowed.
Every exception belongs in `.golangci.yml` with its reason. Do not run an all-
linter autofix. A narrow `--fix --enable-only=LINTER` needs manual diff review;
formatters (`golangci-lint fmt`) only run the configured gofmt/goimports pair.

## What we test and what we do not

We test behavior at the boundary where users rely on it:

- Plan decoding, layout, text/font rules and result rendering with ordinary tests.
- PDF behavior with small synthetic fixtures and independent qpdf/Poppler checks.
  Poppler byte-name lookup complements decoded qpdf JSON; pixels complement
  structural font/pruning checks.
- File publication natively on Linux, macOS and Windows; core report safety
  and cancellation use small product tests.
- macOS installation, update and removal against native filesystems and real
  ad hoc signatures; see [setup verification](#macos-setup-verification).

We deliberately do not gate on mutation testing, coverage percentages, fuzzing
campaigns, dependency test suites, native ACL/console/closed-stream fault injection,
checks of a testing platform or deliberately failing workflows. Fuzz seeds run
as normal tests. Short local fuzz runs up to 30 seconds are permitted. Cross-
compilation covers additional CPU targets; it is not native execution proof.

A new check needs a concrete user-visible risk and the cheapest meaningful test
for it. Preserve failure visibility and expected test discovery. Do not create a
check mainly to satisfy another check.

## macOS setup verification

The user contract is in [the macOS setup guide](docs/MACOS.md). Setup is release
and installation machinery, separate from the PDF assembly CLI. It uses ad hoc
signing only: there are no Developer ID credentials or notarization steps.

Run lifecycle checks only on an expendable Mac or a clean virtual machine. They
exercise the real managed locations under `/usr/local` and require administrator
permission. The harness refuses to start when those locations already contain
an installation; do not remove an existing installation merely to satisfy a test
precondition. The ordinary Go test command does not run these checks.

From the checkout, with the Go version declared in `go.mod`, run as your ordinary
administrator account. Authorize `sudo` first; the harness checks administrator
access without prompting at startup:

```sh
sudo -v
/bin/bash install/macos_test.sh
```

The native checks build real PDFConcat fixtures and install them through setup's
privileged entry point. They exercise ad hoc signing, repeat installation,
updates and removal, and check refusal of foreign or modified files. Isolated
worker cases check caller identity, lock validation and parent-directory policy
without changing shared parent attributes. The checks also verify preservation
of parent-directory permissions and shell configuration. The optional network
case exercises the published GitHub release path:

```sh
/bin/bash install/macos_test.sh --network
```

The network case checks post-install guidance from isolated zsh sessions with
autoloaded functions, aliases and alternate PATH executables, including a
function that rejects `version`. It compares those bare commands with the
managed executable's full path and checks that removal preserves the other
definitions. These sessions use test-owned shell files; they do not read or
change personal shell profiles or functions.

To install and remove an exact native archive from a GoReleaser output directory,
use `/bin/bash install/macos_test.sh --package-dir /absolute/path/to/dist`.
This mode requires a stable version label accepted by setup; ordinary snapshot
versions are not accepted. It checks representative documentation, examples and
schemas, installed executable bytes and outside-file preservation, then removes
the installation through its installed helper. Repeat `sudo -v` if authorization
expires before another harness invocation. The same disposable-Mac restriction
applies to every mode.

The native CI matrix in [the workflow](.github/workflows/ci.yml) covers macOS 15
and macOS 26 on Apple Silicon and Intel. The macOS 15 jobs also run the network
case. Keep those runner choices and required dependencies aligned with the
supported minimum and architectures.
Shell syntax checks on Linux are useful feedback, not evidence for native
`codesign`, `sudo`, filesystem or Gatekeeper behavior. Cross-compilation alone
does not exercise installation on the target architecture.

### Checks on a developer Mac

Before publishing a setup change, use a fresh macOS account or restored VM and
follow the user guide exactly. Record the OS version, CPU architecture, release
version and actual result. Check:

- The complete copy-and-paste bootstrap, menu, confirmation and password prompt
  in an ordinary user's Terminal; cancellation must leave the prior installation
  usable.
- A Mac without a previous PDFConcat installation, including genuinely absent
  `/usr/local/bin`, and a Mac with an existing installation from this setup.
- Apple Silicon and Intel execution on macOS 15 and a current macOS version;
  record the exact OS point version instead of treating a runner label as proof
  for every earlier point release. On Apple Silicon, also check that Terminal
  running under Rosetta still installs the native `arm64` release.
- An account without administrator access and, where available, managed-device
  restrictions; the documented explanation must match the failure.
- A custom PATH, alias or function named `pdfconcat`; verify the managed copy
  by full path and distinguish it from what the user's shell selects, including
  after removal. Opening a new Terminal may reload the same definitions.
- Download interruption, verification failure and unavailable network access;
  check that an existing working installation remains usable.
- Offline removal through the installed helper, including the missing-binary
  case, changed managed files and unrelated files added to the managed tree.
- User-created PDFs, plans and reports before and after removal; the setup tool
  must preserve them.
- Any macOS privacy or security prompt caused by the documented workflow. If
  investigating Safari downloads or quarantine, record that as a separate path;
  do not claim it was tested by the Terminal download case.

Use the observed messages to update the user guide. Do not suppress Gatekeeper,
disable system protections, remove quarantine attributes, or count a successful
ad hoc signature check as Apple notarization. Automated CI does not establish
the behavior of Finder dialogs, administrator password entry, enterprise
policies or every user's shell configuration.

## Permanent corpus

Frozen, fictional producer exports, authored sources and font notices live in
`internal/core/testdata/corpus` in a source checkout. Authoritative ordinary Go
case tables are in `internal/core/corpus*_test.go`; no producer or network access
is needed by tests. This is finite pairwise/higher-order capability coverage,
not exhaustive arbitrary PDF permutations. Binary archives do not include the
source-only corpus; these commands and paths refer to a source checkout. See the
[corpus guide](https://github.com/resoltico/pdfconcat/blob/main/internal/core/testdata/corpus/README.md)
for asset provenance and producer recipes.

### Retain investigation output

Use an unused absolute output directory outside the checkout for retention:

```sh
mkdir -p /absolute/fresh/pdfconcat-artifacts
PDFCONCAT_REQUIRE_ORACLE=1 go test -count=1 ./internal/core -run '^TestCorpus' -artifacts -outputdir /absolute/fresh/pdfconcat-artifacts
PDFCONCAT_SCALE=1 PDFCONCAT_REQUIRE_ORACLE=1 go test -count=1 ./internal/core -run '^TestScale$' -artifacts -outputdir /absolute/fresh/pdfconcat-artifacts
```

The large scale and staged-build workloads are opt-in; ordinary tests and CI
exclude them.

### Read retained output and signal checks

`t.ArtifactDir` provides actual unique case paths; child directories are siblings,
not children of the suite directory. Derived matrices record those paths. Each
case stages copies of selected inputs and consumed fonts; canonical corpus bytes
are never writable product inputs/collision targets. Product/schema/publication
checks run without optional PDF tools; only nested oracle checks skip locally.
Linux CI requires the tools. Ordinary subprocess tests compile fresh normal children;
a race parent does not instrument such children. The Linux/macOS stdin test
explicitly builds both normal/race children and proves pipe consumption before SIGINT. The
opt-in staging test observes an owned stage before signaling and verifies cleanup.
Scale profiles have no timing threshold. Existing unit/context tests remain
supporting checks, not substitutes for real signal boundaries.

### Regenerate source assets

Follow the corpus guide's recipes in fresh paths outside the checkout. Regeneration
is reviewed authoring, not CI setup. Inspect every source page,
metadata/action/attachment content and prior form revision, retain complete font
notices, and refresh independent facts/hashes only after visual/structural review.
If an authoring script changes without changing the frozen assets, refresh its
`AuthoredSourcesSHA256` entry without replacing the reviewed PDF bytes.

## Scale

`TestScale` is the opt-in runner for source-and-generated measurements, corpus
scale profiles, and staged-build SIGINT checks. The signal subtest runs on
Linux/macOS and skips on Windows. Run it when investigating a concrete scale or
staging question:

```sh
go test -c -o /tmp/pdfconcat-scale.test ./internal/core
PDFCONCAT_SCALE=1 PDFCONCAT_REQUIRE_ORACLE=1 /usr/bin/time -l /tmp/pdfconcat-scale.test -test.run '^TestScale$' -test.v
```

The external timer syntax shown is macOS. The test prints fixture and complete
build durations and requires independent page counts. `BenchmarkBuild` is a small
optional benchmark; no timing threshold gates builds.

## Release

Release publication requires maintainer authorization. Review the intended commit, pending changelog outcomes, linked licenses/NOTICE files and installation instructions. The tag's version must have a matching nonempty changelog section. The release workflow runs the reusable CI before publication; keep `Required CI` intact.

Validate GoReleaser at the exact version in `.github/workflows/release.yml`.
Complete release packaging requires macOS: `install/sign-macos.sh` applies and
verifies an ad hoc signature on each Darwin executable before it is archived.
This needs no Developer ID credential or notarization service. The Darwin
linker settings declare macOS 15.0.0 as the minimum; ordinary cross-compilation
checks alone do not verify that metadata or the final signatures.

Run every `goreleaser release --snapshot --clean` in an isolated clone: `--clean`
deletes its dist directory. Preserve existing artifacts by using a separate clone.
Inspect the release archives, both schemas, documentation/licenses, checksums and
native version invocations. Verify the macOS signing and setup contract using the
checks above, including actual installed bytes and removal. Snapshot success is
not release publication.

The CI packaging job builds the current checkout through `.goreleaser.yml`
without publishing. It selects a reachable stable tag accepted by setup and
passes it through `GORELEASER_CURRENT_TAG` as the test archive's version label.
These current-source archives are test artifacts, not published release
artifacts. Only that unpublished build skips GoReleaser's ref validation;
the actual release job retains validation.

CI checks archive checksums and both Darwin executables' ad hoc signatures and
minimum OS. It runs the native archive's executable for a version check and
blank-PDF build, then passes that exact archive to the harness's `--package-dir`
mode. That case prepares and installs the archive through the production worker,
checks installed assets and executable bytes, and removes it through the
installed helper. Missing-document and missing-executable controls check failure
detection. The separate lifecycle matrix and interactive checks above cover
additional states and environments.

A `v*` tag runs the reusable CI, extracts the matching nonempty changelog section,
builds the configured targets with GoReleaser and publishes archives/checksums
with provenance. macOS executables use ad hoc signing only. Checksums and
attestations must describe the final published artifacts after signing; they do
not substitute for a native lifecycle test or establish notarization.
Do not create tags, releases or upstream issues without maintainer authorization.
