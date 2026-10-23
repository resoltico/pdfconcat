# Contributing

Use Go at the version declared in `go.mod`. From a clean checkout:

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

Race tests run on Linux/macOS; Windows runs ordinary tests. CI has six jobs:
lint, tests on those three operating systems, cross-build and `Required CI`.
The aggregate name is a required repository context; do not casually rename it.

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

We deliberately do not gate on mutation testing, coverage percentages, fuzzing
campaigns, dependency test suites, native ACL/console/closed-stream fault injection,
checks of a testing platform or deliberately failing workflows. Fuzz seeds run
as normal tests. Short local fuzz runs up to 30 seconds are permitted. Cross-
compilation covers additional CPU targets; it is not native execution proof.

A new check needs a concrete user-visible risk and the cheapest meaningful test
for it. Preserve failure visibility and expected test discovery. Do not create a
check mainly to satisfy another check.

## Permanent corpus

Frozen, fictional producer exports, authored sources and font notices live in
`internal/core/testdata/corpus` in a source checkout. Authoritative ordinary Go
case tables are in `internal/core/corpus*_test.go`; no producer or network access
is needed by tests. This is finite pairwise/higher-order capability coverage,
not exhaustive arbitrary PDF permutations. Binary archives do not include the
source-only corpus; these commands and paths refer to a source checkout.

Use an unused absolute output directory outside the checkout for retention:

```sh
mkdir -p /absolute/fresh/pdfconcat-artifacts
PDFCONCAT_REQUIRE_ORACLE=1 go test -count=1 ./internal/core -run '^TestCorpus' -artifacts -outputdir /absolute/fresh/pdfconcat-artifacts
PDFCONCAT_SCALE=1 PDFCONCAT_REQUIRE_ORACLE=1 go test -count=1 ./internal/core -run '^TestScale$' -artifacts -outputdir /absolute/fresh/pdfconcat-artifacts
```

The large scale and staged-build workloads are opt-in; ordinary tests and CI
exclude them.

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

Regeneration is reviewed authoring, not CI setup. Inspect every source page,
metadata/action/attachment content and prior form revision, retain complete font
notices, and refresh independent facts/hashes only after visual/structural review.

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
Run every `goreleaser release --snapshot --clean` in an isolated clone: `--clean`
deletes its dist directory. Preserve existing artifacts by using a separate clone.
Inspect six archives, both schemas, documentation/licenses, checksums and a native
version invocation. Snapshot success is not release publication.

A `v*` tag runs the reusable CI, extracts the matching nonempty changelog section,
builds six binaries with GoReleaser and publishes archives/checksums with provenance.
Do not create tags, releases or upstream issues without maintainer authorization.
