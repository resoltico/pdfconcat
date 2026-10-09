# PDFConcat

PDFConcat is a command-line tool for one job: **assemble existing PDF documents in an explicit order and insert generated blank pages at explicit positions.** A generated page can carry a background color and a block of text (Latin including Latvian, Greek, and Cyrillic) for separators and section titles. It is one self-contained executable with no external PDF tools, built for macOS, Linux, and Windows on amd64 and arm64.

```console
pdfconcat build -o Annex.pdf 01.pdf 02.pdf --blank 03.pdf 04.pdf
```

produces `01.pdf`, `02.pdf`, a generated blank page, `03.pdf`, `04.pdf`. The place where `--blank` appears is the place where the blank appears.

Agents are the primary audience. Every command prints compact JSON on standard output by default (`--format text` renders it for people), default build/check/report summaries and command failures are bounded to 2 KiB, and `--report FILE` saves the complete result for queries without reopening any PDF.

## Install

Build the v0.2.0 release source with the Go toolchain required by [`go.mod`](go.mod). This is a source-only release; executable packaging is configured separately and does not provide uploaded binaries:

```text
git clone --branch v0.2.0 https://github.com/resoltico/pdfconcat
cd pdfconcat
go build -o ./bin/ ./cmd/pdfconcat
./bin/pdfconcat version
```

Contributor verification builds the patched development linter and mutation tool from verified upstream source; setup requires Go, Git and a C compiler on platforms with Go race-detector support. See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the installer and Windows/arm64 ordinary-test requirement. The PDFConcat runtime remains one self-contained offline executable.

The build creates `bin/` when needed. On Windows, use `.\bin\pdfconcat.exe version` to run it. The examples below use the installed command name `pdfconcat`; for a source build, substitute `./bin/pdfconcat` on macOS/Linux or `.\bin\pdfconcat.exe` on Windows. No PATH change is needed.

GitHub release notes are extracted from the matching version section in `CHANGELOG.md`. The source-release workflow publishes source and changelog notes. See [`docs/RELEASING.md`](docs/RELEASING.md). `pdfconcat version` prints the version, commit, and commit date of the running executable. The application version is configured once in [`internal/app/version.txt`](internal/app/version.txt); ordinary builds embed it, snapshots derive from it, and release tags must match it.

## Quickstart

Write a plan, the one place that says what to assemble and how generated pages look:

```json
{
  "version": 1,
  "output": "Annex.pdf",
  "blank": { "text": { "value": "This page intentionally left blank", "anchor": "bottom", "y": "20mm" } },
  "items": [
    "cover.pdf",
    { "blank": {} },
    { "dir": "sections", "items": ["01.pdf", "02.pdf"] },
    { "blank": { "text": { "value": "Appendix", "size": 24 } }, "count": 2 },
    "appendix.pdf"
  ]
}
```

Then check, inspect what the check found, and build:

```console
pdfconcat check --plan Annex.json --report Annex.report.json
pdfconcat report Annex.report.json --view diagnostics
pdfconcat report Annex.report.json --view parts --limit 20
pdfconcat report Annex.report.json --part /items/3
pdfconcat report Annex.report.json --page 5
pdfconcat build --plan Annex.json
```

- `check` validates and snapshots every input, resolves every generated page's size and text layout, and reports the resolved layout and known policy consequences. It creates no PDF. `--report FILE` keeps the complete result; the summary stays within 2 KiB and gives an exact next command when it fits; longer paths are explicitly marked as previews.
- `report --view diagnostics` lists errors and warnings, in pages of results, with the declaration location (JSON pointer, byte offset, line, column) of each diagnostic. `--part` shows one contribution with its resolved appearance, `--page N` the contribution that covers output page N. None of these opens a PDF.
- `build` repeats the preparation, assembles, verifies the result, and publishes it.

Success does not mean there are no consequences to review. `error_count` and `warning_count` count captured diagnostic records; their sum is `diagnostic_count`. Allowed text overflow warns only when measured layout finds clipping or another overflow. Material catalog-feature removal and retained executable actions also warn. Follow the attempt-bound diagnostics route, inspect selected parts/pages, and compare counts, order, provenance and source digests with the requested packet. A check predicts consequences; a published build records committed effects. A report query never validates current PDF bytes.

A passing `check` applies the same source-known assembly policy as `build`, including the backend page cap and legacy-destination restrictions. It is advisory about later file changes, backend I/O, and output verification: `build` captures and checks everything again and verifies the assembled PDF. A plan with an `output` that already exists fails `check` and `build` alike until you pass `--overwrite`.

Other ways to give the same job: a plan on standard input, `pdfconcat build --plan - --base-dir /project -o out.pdf < generated.json`; the whole plan inline, `pdfconcat build --plan-json '{"version":1,"items":["a.pdf",{"blank":{}},"b.pdf"]}' -o out.pdf`; or the direct operands shown above. All four are compiled to the same job. A file literally named `--blank` is written `./--blank`. Arguments, filenames, and the working directory must use valid Unicode text; raw byte filenames outside UTF-8 are rejected.

The first command to run is `pdfconcat --help`: it prints one screen and ends with the next command to try. `pdfconcat schema plan`, `pdfconcat schema report`, and `pdfconcat schema response` print the JSON Schemas. Plans remain version 1; JSON responses and saved reports use format 2.

## Exit status

| Status | Meaning |
| ---: | --- |
| `0` | Success; a build/check can carry warnings. A successful report query preserves the saved run's own outcome. |
| `2` | Invalid instructions or unsupported source policy: command/plan/layout/query errors, signature/form/rendering refusals, or destination/file-role conflicts. |
| `1` | Operational failure: an unreadable file, the PDF backend, verification, or publication. |
| `130` | Interrupted. Cancellation before the native PDF commit preserves the destination; a completed commit remains published. |

If the PDF was published and a later step fails (writing the report, writing standard output), the status is `1` and the result says `"published": true`. [`docs/CLI.md`](docs/CLI.md) has the exact classification, the complete grammar, quoting for bash, PowerShell, and cmd.exe, and recipes that generate a plan from a directory.

## Documentation

| Document | Contents |
| --- | --- |
| [`docs/CLI.md`](docs/CLI.md) | Commands, options, output, report queries, exit status, shell recipes |
| [`docs/PLAN.md`](docs/PLAN.md) | Plan-file format, limits, and provenance |
| [`docs/BLANK_PAGES.md`](docs/BLANK_PAGES.md) | Generated page size, background, fonts, text, placement, and overflow |
| [`docs/DESIGN.md`](docs/DESIGN.md) | Why it works this way, design choices and measurement method |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | Packages, pipeline, PDF backend behavior, and invariants |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | Development setup and checks |
| [`docs/RELEASING.md`](docs/RELEASING.md), [`SECURITY.md`](SECURITY.md), [`CHANGELOG.md`](CHANGELOG.md) | Releases, security boundaries, changes |

Configured executable archives contain this README, `docs/CLI.md`, `docs/PLAN.md`, `docs/BLANK_PAGES.md`, the JSON Schemas, and the licenses; the other documents are in the source repository.

## Safety

PDFConcat never modifies source PDFs, fonts, or plan files. It copies each distinct source once into a private workspace and assembles from the copies, builds the result in a temporary file beside the destination, validates it, checks the exact page count, and only then publishes it. An existing output is refused unless `--overwrite` is given; an output that is also an input, or a symbolic link, is always refused. Without `--overwrite`, publication uses the operating system's native no-clobber rename, so a file created concurrently is not replaced. The workspace needs scratch space for a copy of every distinct source. Atomic visibility is not crash durability, and copying the sources is not a filesystem-wide atomic snapshot; see [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) and [`SECURITY.md`](SECURITY.md) for exactly what is and is not claimed.

## Limits

- The output is a newly assembled PDF. Supported page order, ordinary links, static-form appearance and ordinary editing, and generated appearance are the contract. Signature-bearing inputs are refused: use unsigned sources and sign the final PDF externally. Catalog bookmarks/tagged structure and non-destination name trees are removed; page-local attachments and actions can remain. This is not a sanitizer. The table in [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md#pdf-backend-behavior) lists each feature.
- Verification means the embedded pdfcpu validator accepts the result and the page count matches. It is not a second implementation's conformance check.
- An input the embedded engine rejects fails with an error; nothing is repaired or rewritten with another tool. Encrypted PDFs are rejected, including those that open with an empty password.
- Plans have declared bounds (64 MiB, 100,000 contributions, 1,000,000 generated pages; see [`docs/PLAN.md`](docs/PLAN.md#limits)). Memory grows with the size of the documents, because the result is built in memory. The measurement method for large jobs is in [`docs/DESIGN.md`](docs/DESIGN.md#measured-results); results require an identified executable and native host, and are not guarantees.
- Generated-page text uses an embedded Unicode font (Noto Sans) or a TrueType file you supply. Only left-to-right Latin, Greek, and Cyrillic text is accepted; other scripts are rejected rather than set incorrectly ([details](docs/BLANK_PAGES.md#fonts-and-characters)).
- PDFConcat is not a PDF repair tool, optimizer, OCR tool, converter, or general pdfcpu front end.

## License

Copyright (c) 2026 Ervins Strauhmanis. The PDFConcat application is licensed under the [Mozilla Public License 2.0](LICENSE). Third-party components keep their own licenses; see [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md). The corresponding source of a release is the matching version tag of the repository at `https://github.com/resoltico/pdfconcat`.
