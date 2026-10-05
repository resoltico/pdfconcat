# Contributing

## Get started

Prerequisites: Go 1.27.1 (the version in `go.mod`) and Git. No other tools are needed; development tools are pinned in `tools/go.mod` and run through the Go toolchain.

```text
git clone https://github.com/resoltico/pdfconcat
cd pdfconcat
go test ./...
go build ./cmd/pdfconcat
```

## Checks

CI runs these on macOS, Linux, and Windows; run them before sending a change.

```text
go vet ./...
go test -race ./...
go tool -modfile=tools/go.mod golangci-lint run ./...
go tool -modfile=tools/go.mod govulncheck ./...
go mod tidy && go -C tools mod tidy && git diff --exit-code go.mod go.sum tools/go.mod tools/go.sum
go tool -modfile=tools/go.mod goreleaser check
```

Format with `go tool -modfile=tools/go.mod golangci-lint fmt`. Build-tagged files are analyzed only for the host OS; to lint another one, build the tool once (`go tool -n -modfile=tools/go.mod golangci-lint` prints its path) and run it with `GOOS=windows` or `GOOS=linux`.

## Scope and contracts

PDFConcat does one thing: assemble existing PDFs in an explicit order and insert generated blank pages at explicit positions. [`docs/DESIGN.md`](docs/DESIGN.md) records what was rejected and why; read it before proposing a feature.

These documents are the product contracts: [`docs/CLI.md`](docs/CLI.md), [`docs/PLAN.md`](docs/PLAN.md), [`docs/BLANK_PAGES.md`](docs/BLANK_PAGES.md), and [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), together with the plan JSON Schema at `internal/plan/plan.schema.json`. A behavior change updates the relevant contract and its tests together.

Invariants worth knowing before you change code:

- Assembly is single-pass. Blanks are never inserted one at a time into an already-merged file, because jobs run to thousands of sources and blanks.
- Sources and plan files are never modified. An output is published only after final validation and an exact page-count check, using the platform's native no-clobber rename.
- pdfcpu runs stateless and offline, with merge bookmarks disabled, and is imported only by `internal/pdfengine`. Nothing silently falls back to Ghostscript, qpdf, or another engine.
- Behavior is equivalent on macOS, Windows, and Linux; OS-specific code is isolated in build-tagged files with the same package contract.
- No implicit directory scanning or globbing, and one syntax per concept.

## Lint policy

Every golangci-lint linter is enabled, at the version pinned in `tools/go.mod`. There are no inline `//nolint` comments; a test fails if one appears. A genuine exception goes in the registry in `.golangci.yml` with a comment saying why it is justified, and a test fails for a registry entry without one. Fix the code first.

Do not run `golangci-lint --fix` with all linters: some autofixers reorder struct fields or emit code that does not compile. `--fix --enable-only=wsl_v5` is safe for whitespace; review the diff.

## Changes

Keep changes focused. A behavioral change normally includes the implementation, focused tests, the updated contract documents (a test fails if the plan schema and decoder drift apart, and if documented options or links go stale), and an `Unreleased` entry in [`CHANGELOG.md`](CHANGELOG.md) when users are affected.

Use synthetic PDFs as fixtures (`internal/pdffixture`), never confidential documents.

## Dependencies

Prefer the standard library. A runtime dependency needs a concrete product or platform need, a pinned version in `go.mod`, and an update to `THIRD_PARTY_NOTICES.md`. Development tools belong in the separate `tools` module as `tool` directives.
