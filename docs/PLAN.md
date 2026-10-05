# Plan-file contract

A plan file describes a complete assembly: the output, how generated blanks look, and the ordered sequence of PDFs and blanks. It is the interface for large and generated jobs, for rebuildable bundles kept in version control, and for anything that needs per-blank text or styling.

```text
pdfconcat --plan book.json --overwrite
pdfconcat --plan - -o out.pdf < generated-plan.json
```

## Format

A plan is one strict [JSON](https://www.rfc-editor.org/rfc/rfc8259) document in UTF-8 (a leading BOM is accepted). Unknown fields, trailing data, and duplicate-shape mistakes are errors with the line and column, or the JSON path (`items[41].blank.text.color`), of the problem. The machine-readable schema ships in the binary (`pdfconcat --print-schema`) and in [`internal/plan/plan.schema.json`](../internal/plan/plan.schema.json); add `"$schema"` to a plan for editor completion and validation.

```json
{
  "$schema": "https://raw.githubusercontent.com/resoltico/pdfconcat/main/internal/plan/plan.schema.json",
  "version": 1,
  "output": "Annex.pdf",
  "dir": "source",
  "blank": {
    "background": "#F4EFE6",
    "text": {
      "value": "This page intentionally left blank",
      "font": "Times-Italic",
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
        "text": { "value": "Part 2\nEvidence bundle", "font": "Helvetica-Bold", "size": "18pt", "anchor": "top-left", "x": "25mm", "y": "-30mm", "align": "left" }
      },
      "count": 2
    },
    { "dir": "part-2", "items": ["01.pdf", "02.pdf"] },
    { "blank": { "text": { "value": "" } } }
  ]
}
```

### Top level

| Field | Required | Meaning |
| --- | --- | --- |
| `$schema` | no | Ignored by PDFConcat; for editors. |
| `version` | yes | Must be `1`. A different value is an error naming the version this build reads. |
| `output` | no | Destination PDF, relative to the plan file's directory (to the working directory for `--plan -`). `-o` on the command line overrides it. |
| `dir` | no | Directory that relative PDF paths resolve against, relative to the plan's directory. Default: the plan's directory. |
| `blank` | no | Run-wide defaults for every generated blank. |
| `items` | yes | The ordered sequence. |

### Items

`items` is an array whose entries are one of:

| Entry | Meaning |
| --- | --- |
| `"path.pdf"` | A source PDF. A bare string is always a PDF path; nothing in a string is interpreted. |
| `{ "blank": { … }, "count": N }` | `N` consecutive generated blanks (default 1) sharing the style `blank`, which layers over the run-wide defaults. `{ "blank": {} }` is a plain blank. |
| `{ "dir": "path", "items": [ … ] }` | A group. Relative paths in the nested items resolve against `dir`, which is itself relative to the enclosing directory. Groups nest to any depth and exist only to avoid repeating a directory prefix; they have no other effect on the sequence. |

Paths use `/` or the platform separator; absolute paths are used as given. There is no wildcard, glob, or directory scan — the plan lists exactly the files that are assembled, in exactly that order. Generate the list with a script when the files come from a directory (see *Generating plans*).

### Blank settings

A `blank` object (top-level defaults, or one item's own) accepts `size`, `background`, and a `text` object with `value`, `font`, `size`, `color`, `anchor`, `x`, `y`, `width`, `align`, and `leading`. All are optional. Their meanings, defaults, units, and layering are in [`BLANK_PAGES.md`](BLANK_PAGES.md); the table there lists each setting's plan key next to its command-line option.

## Paths and portability

Relative paths resolve against the plan file's directory (or `dir`, or a group's `dir`), never against the working directory, so a plan behaves identically wherever it is run from. A plan read from standard input has no directory; it resolves against the working directory. Write `/` in paths: Windows accepts it, and a plan with `/` works unchanged on all three platforms.

## Limits

A plan may be up to 64 MiB. Thousands of PDFs and thousands of blanks are ordinary: each distinct blank is rendered once and reused for every page that shares it, and the output is produced in one pass over all sources.

## Generating plans

Plans are plain JSON, so any tool can write one. The list of files is the only thing that needs generating; the ordering rule is whatever the generating command sorts by, written down in the plan where it can be reviewed and rerun.

POSIX shell with `jq` (lexical order):

```sh
find chapters -name '*.pdf' | sort \
  | jq -R . | jq -s '{version: 1, items: .}' \
  | pdfconcat --plan - -o book.pdf
```

PowerShell (natural file-name order is the shell's, so state the sort explicitly):

```powershell
$items = Get-ChildItem chapters -Filter *.pdf | Sort-Object Name | ForEach-Object { $_.FullName }
@{ version = 1; items = $items } | ConvertTo-Json -Depth 5 | pdfconcat --plan - -o book.pdf
```

Python:

```python
import json, pathlib, subprocess
files = sorted(pathlib.Path("chapters").glob("*.pdf"))
plan = {"version": 1, "items": [str(f.resolve()) for f in files]}
subprocess.run(["pdfconcat", "--plan", "-", "-o", "book.pdf"], input=json.dumps(plan).encode(), check=True)
```

Review before committing: `pdfconcat --plan plan.json --dry-run` (add `--json` to parse the result).
