# PDFConcat

PDFConcat is a cross-platform command-line tool for one job: **assemble existing PDF documents in an explicit order and insert generated blank pages at explicit positions.** It runs on macOS, Linux, and Windows (amd64 and arm64) as a single binary with no external PDF tools.

```text
pdfconcat -o Annex.pdf 01.pdf 02.pdf --blank 03.pdf 04.pdf
```

produces `01.pdf`, `02.pdf`, a generated blank page, `03.pdf`, `04.pdf`. The place where `--blank` appears is the place where the blank appears.

## Install

Download an archive from the [releases](https://github.com/resoltico/pdfconcat/releases) page, or build from source with Go 1.27.1:

```text
go build ./cmd/pdfconcat
```

## Use it

**Blank pages that say something.** Give every blank a message and a look:

```text
pdfconcat -o Annex.pdf \
  --blank-text "This page intentionally left blank" \
  --blank-font Times-Italic --blank-color '#555555' \
  --blank-anchor bottom --blank-y 20mm \
  01.pdf 02.pdf --blank 03.pdf
```

**Plan files.** Long, generated, or rebuildable jobs use a JSON plan:

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

```text
pdfconcat --plan Annex.json              # build
pdfconcat --plan Annex.json --dry-run    # inspect first
pdfconcat --plan - -o out.pdf < generated.json
```

Plans list exactly the files assembled — no globbing, no directory scanning — so they can be reviewed, diffed, and rerun. Thousands of PDFs and thousands of blanks are ordinary.

`pdfconcat --help` summarizes every option.

## Documentation

| Document | Contents |
| --- | --- |
| [`docs/CLI.md`](docs/CLI.md) | Command-line grammar, options, output policy, exit status |
| [`docs/PLAN.md`](docs/PLAN.md) | Plan-file format, with recipes that generate plans from a directory |
| [`docs/BLANK_PAGES.md`](docs/BLANK_PAGES.md) | Blank page size, background, text, placement, and layering |
| [`docs/DESIGN.md`](docs/DESIGN.md) | Why it works this way, and what was rejected |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | Packages, data flow, and invariants |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | Development setup, checks, lint policy |
| [`docs/RELEASING.md`](docs/RELEASING.md), [`SECURITY.md`](SECURITY.md), [`CHANGELOG.md`](CHANGELOG.md) | Releases, security boundaries, change history |

## Safety

PDFConcat never modifies source PDFs or plan files. It assembles into a temporary workspace beside the destination, validates the result, checks the exact page count, and only then publishes it. An existing output is refused unless `--overwrite` is given; an output that is also an input, or a symbolic link, is always refused. Without `--overwrite`, publication uses the operating system's native no-clobber rename, so a file created concurrently is not replaced. `--dry-run` checks everything except creating the output.

## Limits

- The output is a newly assembled PDF. Page order and blank placement are the contract; bookmarks, forms, metadata, tagged-PDF structure, and signatures follow pdfcpu's merge behavior. Do not assume PDF/UA accessibility structure or digital signatures survive.
- Verification means the embedded pdfcpu validator accepts the result and the page count matches. It is not a second implementation's conformance check.
- An input the embedded engine rejects fails with an error; nothing is repaired or rewritten with another tool. Encrypted PDFs that need a password fail; there is no password prompt.
- Blank-page text uses the standard PDF fonts and the Windows-1252 character set ([details](docs/BLANK_PAGES.md#fonts-and-characters)).
- PDFConcat is not a PDF repair tool, optimizer, OCR tool, converter, or general pdfcpu front end.

## License

Copyright (c) 2026 Ervins. PDFConcat is licensed under the [Mozilla Public License 2.0](LICENSE). Third-party components keep their own licenses; see [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md). The corresponding source of a release is the matching version tag at `https://github.com/resoltico/pdfconcat`.
