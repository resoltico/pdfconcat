# Plan-file contract

A plan file describes a complete assembly: the output, how generated blank pages look, and the ordered sequence of PDFs and blanks. It is the one place appearance is written: there are no command-line style options, and the command-line shortcut for simple jobs compiles to the same job. Plans are the interface for large and generated jobs, for rebuildable bundles kept in version control, and for anything that needs per-blank text or styling.

```text
pdfconcat build --plan job.json
pdfconcat check --plan job.json --report job.report.json
pdfconcat build --plan - --base-dir /project -o out.pdf < generated-plan.json
```

## Format

A plan is exactly one [JSON](https://www.rfc-editor.org/rfc/rfc8259) object in valid UTF-8. A leading byte order mark is accepted; only whitespace may follow the object. This format is plan format version `1`; any other `version` is rejected.

The decoder is strict. It rejects, with a located error (source name, byte offset, line and column, JSON pointer such as `/items/41/blank/text/color`, a stable `plan_*`/`json_*` code, and the expected value):

- unknown member names, and names with the wrong case (`Version`);
- duplicate member names at any depth, including names that become identical after JSON escapes are decoded;
- `null` anywhere (omit a member to leave it unset);
- comments, trailing commas, single quotes, `NaN` and `Infinity`, leading zeros;
- invalid UTF-8, and unpaired surrogate escapes such as `"\ud800"`;
- wrong types, non-finite or out-of-range numbers, fractional counts, an empty `items` array at any level, and ambiguous item shapes.

Parsing stops at the first structural fault, so a rejected plan reports the first problem found, not every problem. The machine-readable schema ships in the binary (`pdfconcat schema plan`); the source file is `internal/plan/plan.schema.json` in the repository. The schema states the structural rules; it cannot express duplicate members, Unicode and UTF-8 rules, NUL characters in paths, the per-side range of a `WIDTHxHEIGHT` size, unit-converted length bounds, or the aggregate limits below. Those are enforced by the decoder only. For editor completion, save the schema next to the plan with `pdfconcat schema plan > plan.schema.json` and add `"$schema": "./plan.schema.json"`; PDFConcat itself ignores the member and never fetches a schema from a network. Release archives also contain the schema as `schemas/plan.schema.json`.

```json
{
  "$schema": "./plan.schema.json",
  "version": 1,
  "output": "Annex.pdf",
  "dir": "source",
  "blank": {
    "background": "#F4EFE6",
    "text": {
      "value": "This page intentionally left blank",
      "font": { "file": "fonts/Example.ttf" },
      "size": 14,
      "color": "#555555",
      "anchor": "bottom",
      "y": "20mm"
    }
  },
  "items": [
    "cover.pdf",
    { "blank": {} },
    { "dir": "part-1", "items": ["01.pdf", "02.pdf"] },
    {
      "blank": {
        "size": "A4",
        "background": "none",
        "text": { "value": "Part 2\nEvidence bundle", "font": "default", "size": "18pt", "anchor": "top-left", "x": "25mm", "y": "-30mm", "align": "left" }
      },
      "count": 2
    },
    { "dir": "part-2", "items": ["01.pdf", "02.pdf"] },
    { "blank": { "text": { "value": "" } } }
  ]
}
```

Complete plans that the test suite builds with the real executable are in the repository's `examples/plans` directory: `basic.json`, `latvian-text.json`, `grouped-directories.json`, `font-file.json`, and `generated-only.json`.

### Top level

| Member | Required | Meaning |
| --- | --- | --- |
| `$schema` | no | Any string; ignored by PDFConcat. For editors. |
| `version` | yes | Must be `1` (`1.0` and `1e0` are the same integer). |
| `output` | no | Destination PDF, non-empty, relative to the plan's base directory. `-o` on the command line overrides it. |
| `dir` | no | Directory that relative item paths resolve against, relative to the plan's base directory. Default: the base directory itself. |
| `blank` | no | Defaults for every generated blank. |
| `items` | yes | The ordered sequence; at least one entry. |

The plan's **base directory** is the directory of a named plan file; for a plan read from standard input or passed inline it is `--base-dir`, or the working directory when that is not given.

### Items

`items` is an array whose entries are one of:

| Entry | Meaning |
| --- | --- |
| `"path.pdf"` | A source PDF. A bare string is always a PDF path; nothing in a string is interpreted. A string such as `"--blank"` or `"@blank"` is just a file name. |
| `{ "blank": { … }, "count": N }` | `N` consecutive generated blank pages (default 1, at most 1,000,000) sharing the style `blank`, which layers over the plan defaults. `{ "blank": {} }` is a plain blank. |
| `{ "dir": "path", "items": [ … ] }` | A group. Relative paths in the nested items resolve against `dir`, which is itself relative to the enclosing directory. Groups exist only to avoid repeating a directory prefix; they have no other effect on the sequence. |

`count` is an integer: `3`, `3.0` and `3e0` are accepted, `2.5` is not, and nothing is rounded.

Paths are literal filesystem paths, not URLs or templates; there is no wildcard, glob, or directory scan. The plan lists exactly the files that are assembled, in exactly that order. `/` is the recommended separator; absolute paths are used as given and are platform-specific. A group is not a sandbox: `..` and absolute paths inside a group are allowed.

### Blank settings

A `blank` object (the plan defaults, or one item's own) accepts `size`, `background`, and a `text` object. Every member is optional, and an omitted member inherits. Appearance layers only as per-item, then plan defaults, then built-in defaults.

| Member | Values |
| --- | --- |
| `size` | `"inherit"` (default), a paper name (`A3`, `A4`, `A5`, `B4`, `B5`, `Letter`, `Legal`, `Tabloid`; portrait, exact case), or `"WIDTHxHEIGHT[unit]"`. Each side is 1 to 14400 points. |
| `background` | `"#RRGGBB"`, `"#RGB"`, or `"none"` (no painting; also clears an inherited fill). |
| `text.value` | A string of at most 10,000 characters. `""` means no text, and is preserved as an explicit value. |
| `text.font` | `"default"` or `{ "file": "path/to/Font.ttf" }`. One atomic choice. |
| `text.size` | A length; 1 to 14400 points. |
| `text.color` | `"#RRGGBB"` or `"#RGB"`. |
| `text.anchor` | `top-left`, `top`, `top-right`, `left`, `center`, `right`, `bottom-left`, `bottom`, `bottom-right`. |
| `text.x`, `text.y` | Lengths, within ±14400 points. |
| `text.width` | A length; 1 to 14400 points. |
| `text.align` | `left`, `center`, `right`, `justify`. |
| `text.leading` | A number from 1 to 10. |
| `text.overflow` | `"error"` (default) or `"allow"`. |

A length is a JSON number of points, or a string made of a decimal number and an optional unit `pt`, `mm`, `cm`, or `in`, with no spaces: `12`, `"12pt"`, `"25.4mm"`, `".5in"`. Exponents are accepted in JSON numbers (`1e2`) but not in strings. Every length is finite and within ±14400 points.

Meanings, defaults, placement and layering are in [`BLANK_PAGES.md`](BLANK_PAGES.md).

### Where a font file is resolved

A font file is resolved against the directory of the scope that **declares** it, before any layering, so a font keeps its own base wherever it ends up being used:

- a font in the plan's top-level `blank` defaults resolves against the plan's base directory. The top-level `dir` does not apply to it;
- a font in a blank item resolves against the directory its items resolve against: the top-level `dir` for root-level items, or the group's directory for items in a group;
- an absolute path is used as given.

So a default font `fonts/A.ttf` stays relative to the plan even for a blank that sits inside a chapter group, while `{ "file": "fonts/B.ttf" }` written inside that group is relative to the group.

## Provenance

Every item, and every style value, remembers where it was written: the source name (file path, `<stdin>`, or `<inline>`), the absolute byte offset (a leading byte order mark counts as three bytes), line and column (columns count bytes), and the JSON pointer of the item, such as `/items/42/items/3`. Later stages report failures with these locations. Jobs compiled from command-line operands are located as `argv:N`, where N is the zero-based position of the operand, excluding the executable name.

## Limits

| Limit | Value |
| --- | --- |
| Plan size | 64 MiB, a byte order mark included |
| Nested groups | 64 |
| PDF and blank entries after groups are flattened | 100,000 |
| Structural nodes: items, `blank`, `text` and font objects | 250,000 |
| One blank item's `count` | 1,000,000 |
| Generated pages in total | 1,000,000 |

The limits are charged while the plan is read, so an oversized plan is rejected early with an error that names the limit and the location where it was crossed. They are declared resource bounds, not a promise that every job within them runs in bounded memory.

After source inspection, both `check` and `build` also enforce the backend's total-page resource cap, declared by [`pdfengine.MaxOutputPages`](../internal/pdfengine/request.go), and its [source occurrence rules](ARCHITECTURE.md#pdf-backend-behavior). The domain's signed 32-bit page arithmetic is a separate representational bound, not the backend's capacity promise. A passing check validates the source-known policy without assembling millions of pages; build still performs output verification and can fail on later I/O or file changes.

## Generating plans

Plans are plain JSON, so any tool can write one. The list of files is the only thing that needs generating; the ordering rule is whatever the generating command sorts by, and writing it down in the plan lets it be reviewed and rerun. A plan on standard input has no directory, so pass `--base-dir` (or use absolute paths) to make relative paths independent of the working directory.

[`CLI.md`](CLI.md#large-jobs-and-shells) has the tested recipes for Python, bash and zsh, PowerShell, and cmd.exe, and explains why newline-delimited text is not a safe transport for file names.

Review before building: `pdfconcat check --plan plan.json --report plan.report.json`.
