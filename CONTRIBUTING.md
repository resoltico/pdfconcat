# Contributing

For commands and packet plans, start with the [README](README.md). This guide
covers development from a source checkout; binary release archives do not contain
the test sources or corpus.

The README defines the [product scope and boundaries](README.md#scope-and-boundaries).
Assembly aids and simple styled text pages, including generated-only plans, are
supported. General document layout, source-content editing and format conversion
are outside that scope. Scope eligibility does not imply an implemented feature;
the README comparison identifies missing merge workflows.

- [Build and ordinary tests](#build-and-ordinary-tests)
- [Install with Go](#install-with-go)
- [Stock tools](#stock-tools)
- [Test scope](#what-we-test-and-what-we-do-not)
- [Installer verification](#installer-verification)
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
[`internal/oracle/qpdf.version`](internal/oracle/qpdf.version);
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
tests on those three operating systems, cross-builds, release packaging and
native installation through `Required CI`. That aggregate name is a required repository
context; do not casually rename it.

## Install with Go

To install the current checkout into Go's executable directory:

```sh
go install ./cmd/pdfconcat
```

To install the latest published source release instead:

```sh
go install github.com/resoltico/pdfconcat/cmd/pdfconcat@latest
```

Use the Go version declared in [go.mod](go.mod). The executable goes into the
directory printed by `go env GOBIN`, or `bin` beneath the first directory printed
by `go env GOPATH` when GOBIN is empty. Use that executable's absolute path or
add its directory to your development shell's PATH.

Keep development executables separate from the installed release in
`~/.local/bin` or `%LOCALAPPDATA%\Programs\PDFConcat`. Check which copy a bare
`pdfconcat` selects with `command -v pdfconcat` or `Get-Command pdfconcat`.

## Stock tools

Tool versions live in `tools/go.mod`. Build one native linter, then run that
executable with each target GOOS. Setting GOOS on `go tool` would cross-build the
tool itself and fail to execute it.

```sh
go build -modfile=tools/go.mod -o /tmp/pdfconcat-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint
(for goos in linux darwin windows; do GOOS="$goos" /tmp/pdfconcat-lint run ./... || exit; done)
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
- Installation, update and removal from the exact release archives on Linux,
  macOS and Windows; see [installer verification](#installer-verification).

We deliberately do not gate on mutation testing, coverage percentages, fuzzing
campaigns, dependency test suites, native ACL/console/closed-stream fault injection,
checks of a testing platform or deliberately failing workflows. Fuzz seeds run
as normal tests. Short local fuzz runs up to 30 seconds are permitted. Cross-
compilation covers additional CPU targets; it is not native execution proof.

A new check needs a concrete user-visible risk and the cheapest meaningful test
for it. Preserve failure visibility and expected test discovery. Do not create a
check mainly to satisfy another check.

## Installer verification

The user contract is in [the installation guide](docs/INSTALL.md).
`install/install.sh` serves macOS and Linux and must stay POSIX `sh`: Ubuntu runs
it with dash. `install/install.ps1` serves Windows and must run under both
Windows PowerShell 5.1 and PowerShell 7; keep it ASCII, and never use `exit`
outside the final handler, because `irm | iex` runs it in the caller's session.
Both are published as standalone release assets beside the archives they install.
Latest is resolved once to a stable tag before downloading archive/checksum data;
version and archive identity must agree. Keep network and mirror paths subject
to the same checksum, regular-file and ownership checks. Explicit release pins
are stable versions; local snapshot fixtures may report canonical SemVer suffixes.

PDFConcat owns the common engine bodies shared with EML Attachment Remover.
Keep only literal product differences between `BEGIN PRODUCT SETTINGS` and
`END PRODUCT SETTINGS`; change common behavior in both repositories together.
The copies are self-contained and have no runtime cross-repository dependency.
EML pins a reviewed PDFConcat commit for its development/CI parity check. Commit
the canonical PDF engine change first, preserve EML's product settings when
copying the scripts, then update EML's `install/engine-source.txt` to that full
PDF commit. From the EML checkout run:

```sh
python3 install/check-sync.py /absolute/path/to/PDFConcat
```

The offline check reads the pinned Git object, not uncommitted files. Without a
checkout argument the checker fetches the pinned files over HTTPS for development
or CI; this is not an installation-time dependency. PDF CI checks its canonical
behavior without depending on EML's default branch.

The test harnesses install, update and uninstall real archives from a
GoReleaser dist directory. They use a temporary `HOME` or install folder; the
Unix harness never reads or edits your own startup files:

```sh
cp install/install.sh install/install.ps1 dist/
bash install/install_test.sh "$PWD/dist"
```

```powershell
Copy-Item install/install.sh, install/install.ps1 dist/
./install/install_test.ps1 -Dist "$PWD\dist"
```

The harnesses execute the staged `dist/install.sh` / `dist/install.ps1`, not a
source fallback, and check them against the primary manifest before the cases.
Stage the unchanged source files after a local GoReleaser build; their hashes
already belong to its checksum manifest.

The PowerShell harness runs each case under `powershell` and `pwsh` when both
exist. Its user PATH case edits and then restores `HKCU\Environment`, so it runs
only when `CI` is set. Restore raw PATH text, value kind and original absence,
including on failures. Test owned/unowned targets, tampered receipts/binaries,
locked operations, exact PATH ownership, unsafe archives, checksum/version
failures and activation rollback. Never run these tests against a developer's
actual startup files or user registry. Lint both installers with the commands in the Lint job:
`shellcheck` (exceptions in `.shellcheckrc`) and PSScriptAnalyzer (exceptions in
`install/PSScriptAnalyzerSettings.psd1`).

CI builds the release files once on macOS, where Darwin executables are ad hoc
signed, then runs the harnesses on Linux x86-64 and arm64, macOS 15 and 26 on
Apple silicon, macOS 15 on Intel, and Windows x64 and Arm64. Automated runs do
not cover browser downloads, Gatekeeper or SmartScreen prompts, managed-device
policies, or proxies. Before releasing an installer change, also run the
documented command by hand in a fresh account on at least one system per
operating system, and check that the output reads well in a light and a dark
terminal theme.

## Permanent corpus

Frozen, fictional producer exports, authored sources and font notices live in
`internal/core/testdata/corpus` in a source checkout. Authoritative ordinary Go
case tables are in `internal/core/corpus*_test.go`; no producer or external-network
access is needed by tests. A bounded loopback HTTP canary verifies that the CLI
retains URI annotations without fetching them. This is finite pairwise/higher-order capability coverage,
not exhaustive arbitrary PDF permutations. Binary archives do not include the
source-only corpus; these commands and paths refer to a source checkout. See the
[corpus guide](internal/core/testdata/corpus/README.md)
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
optional benchmark, and `BenchmarkPacket` builds a representative packet from
the frozen corpus; no timing threshold gates builds.

## Release

Release publication requires maintainer authorization. Review the intended
changes, pending changelog outcomes, linked licenses/NOTICE files and installation
instructions. Prepare the matching dated, nonempty changelog section and merge
the release changes into `main`. Tag that intended commit as `vX.Y.Z` and push
the tag. The release workflow runs the reusable CI before publication; keep
`Required CI` intact. Do not publish a second release by hand while it is running.

For local packaging investigations, use the exact GoReleaser version in
`.github/workflows/ci.yml`. The release workflow already performs its CI
gates, exact tag-version packaging checks and final publication; a routine release does
not require duplicating those gates locally.

Complete release packaging requires macOS: `.github/scripts/sign-macos.sh`
applies and verifies an ad hoc signature on each Darwin executable before it is
archived. This needs no Developer ID credential or notarization service. The
Darwin linker settings declare macOS 15.0.0 as the minimum; ordinary
cross-compilation checks alone do not verify that metadata or the signatures.

Run a local `goreleaser release --snapshot --clean` only in an isolated clone:
`--clean` deletes its dist directory. When investigating packaging, inspect the
archives, checksums, native version invocations and `pdfconcat licenses`, then run the
[installer harnesses](#installer-verification) against that dist directory.
Copy the unchanged `install/install.sh` and `install/install.ps1` into `dist/`
before running the harnesses. Snapshot success is not release publication.
GoReleaser publication is disabled in configuration; it is a producer only.

Archives contain only the program, `LICENSE`, the third-party notices and their
license texts; the notices live in `internal/legal/` so that the executable
embeds them too. `cmd/pdfconcat/notices_test.go` checks that every linked
module, every license file inside a linked package and every asset a dependency
embeds is covered. Two snapshot builds of one commit must produce identical
`checksums.txt`: archive members use fixed owners and commit timestamps.

Release builds pass `-trimpath` and `-ldflags=-w`. They keep the Go symbol
table, without DWARF data, so that `govulncheck -mode=binary` and similar
scanners see which packages are linked; stripping it with `-s` makes them
report every advisory for a linked module. Release and ordinary builds use the
compiler's default optimization settings.

Profile-guided optimization is not enabled for release builds.

GoReleaser's before hooks generate Windows resources with the pinned
`go-winres` (file properties and an `asInvoker` manifest, as git-ignored
`cmd/pdfconcat/rsrc_windows_*.syso`) and build the pinned `cyclonedx-gomod`
into `.tools/bin`. That tool writes one CycloneDX SBOM per executable, from the
module list embedded in it, without serial numbers or timestamps. Detecting the
main module's license downloads the tagged release through the Go proxy, so
snapshot SBOMs omit that one license with a warning.

Archive names carry no version (`pdfconcat_linux_amd64.tar.gz`). The CI producer
uses `release --snapshot --clean` for non-tag builds and stable/prerelease tag
metadata with `release --clean --skip=publish` for `v*` tags. Both modes stage
six archives, six SBOMs, both standalone installers and `checksums.txt` flat in
one artifact. CI verifies hashes, archive contents, SBOM platforms, ad hoc Darwin
signatures and minimum OS, scans Darwin binaries with `govulncheck -mode=binary`,
and runs the seven native installer jobs against those exact staged files.

A `v*` tag's publisher waits for that reusable CI, downloads the qualified artifact
and verifies the complete filename set and hashes again. It extracts exactly one
nonempty, dated matching changelog section, attests the qualified bytes, then
publishes them with `gh release create --verify-tag --notes-file`; there is no
second build or signing pass. Prerelease tags remain prereleases. The workflow
does not create tags or overwrite an existing release. macOS signing stays ad hoc;
checksums and attestations do not establish notarization. Expired/missing qualified
artifacts fail publishing instead of causing an untested rebuild.
Do not create tags, releases or upstream issues without maintainer authorization.
