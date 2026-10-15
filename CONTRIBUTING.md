# Contributing

Use Go at the version declared in `go.mod`. From a clean checkout:

```sh
go build -o bin/pdfconcat ./cmd/pdfconcat
go vet ./...
go test ./...
```

Independent PDF tests use qpdf and Poppler (`pdftotext`, `pdftoppm`, `pdfinfo`,
`pdffonts`, `pdftohtml`). Install `qpdf poppler` with Homebrew, or
`qpdf poppler-utils` with apt. Missing tools skip their tests locally; set
`PDFCONCAT_REQUIRE_ORACLE=1` to require them. Linux CI does that.

```sh
PDFCONCAT_REQUIRE_ORACLE=1 go test -race ./...
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
`@version` execution because its prerelease YAML API conflicts with the linter's
combined dependency graph; the version is still derived from the one tool module.
See [DESIGN](docs/DESIGN.md). Actionlint uses shellcheck when it is available.

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
- File publication natively on Linux, macOS and Windows; pipeline report safety
  and cancellation use small product tests.

We deliberately do not gate on mutation testing, coverage percentages, fuzzing
campaigns, dependency test suites, native ACL/console/closed-stream fault injection,
checks of a testing platform or deliberately failing workflows. Fuzz seeds run
as normal tests. Short local fuzz runs up to 30 seconds are permitted. Cross-
compilation covers additional CPU targets; it is not native execution proof.

A new check needs a concrete user-visible risk and the cheapest meaningful test
for it. Preserve failure visibility and expected test discovery. Do not create a
check mainly to satisfy another check.

## Comparison and scale

The comparison uses the pdfcpu CLI matching the runtime module pin, Python 3.9+
and the oracle tools. It creates a fresh child directory, checks expected
success/refusal exits and preserves stdout/stderr for failures. It exports only
small fixtures from `internal/pdffixture`, without a copied fixture module or
unused 100000-page generation.

```sh
scripts/run-comparison.sh /absolute/path/to/pdfconcat /tmp/pdfconcat-comparisons
```

Scale is opt-in and must not run in CI. The rebuild's one authorized local run
is recorded in DESIGN; it is not repeated by ordinary checks. To run a future
owner-approved measurement:

```sh
go test -c -o /tmp/pdfconcat-scale.test ./internal/pipeline
PDFCONCAT_SCALE=1 PDFCONCAT_REQUIRE_ORACLE=1 /usr/bin/time -l /tmp/pdfconcat-scale.test -test.run '^TestScale$' -test.v
```

The external timer syntax shown is macOS. The test prints fixture and complete
build durations and requires independent page counts. `BenchmarkBuild` is a small
optional benchmark; no timing threshold gates builds.

## Release

The owner controls tags/releases. Before tagging, run the delivered-state checks
from a fresh clone, inspect `Required CI` for the PR's current commit, reconcile actual
linked licenses/NOTICE files, move the intended changelog outcome to a dated
version section, and review installation availability wording.

Validate GoReleaser at the exact version in `.github/workflows/release.yml`.
Run every `goreleaser release --snapshot --clean` in an isolated clone: `--clean`
deletes its dist directory. Never use it against an owner's shared dist/artifacts.
Inspect six archives, both schemas, documentation/licenses, checksums and a native
version invocation. Snapshot success is not release publication.

A `v*` tag runs the reusable CI, extracts the matching nonempty changelog section,
builds six binaries with GoReleaser and publishes archives/checksums with provenance.
Do not create tags, releases or upstream issues without the owner's authorization.
The next intended owner tag for this rebuild is `v0.3.0`.
