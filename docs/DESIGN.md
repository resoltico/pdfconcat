# Product and UX design

## Product sentence

PDFConcat assembles existing PDF documents in an explicitly specified order and inserts generated blank pages at explicitly specified positions.

That sentence is the scope test for proposed features.

## Why the sequence is the interface

The user's real problem is positional: "put these documents in this order, and put a blank here." The interface therefore exposes the final sequence directly instead of encoding an instruction on some other object.

Rejected models include:

- `BLK` or another filename suffix: mutates document identity to store assembly state;
- `--blank-after file.pdf`: separates the directive from the position it modifies and becomes awkward with repeated filenames;
- page-number directives: force the user or agent to know source page counts before assembly;
- a persistent `_blank.pdf`: creates asset management, geometry, and discovery problems;
- implicit directory scanning and filename sorting: hides ordering policy in filesystem rules and shell behavior; and
- multiple equivalent blank syntaxes: increases agent ambiguity without increasing capability.

## Two surfaces, one model

The sequence has two surfaces over one model — an ordered list of `PDF(path)` and `Blank(style, count)` items:

- **The command line** is for short, human-written jobs. `pdfconcat -o out.pdf a.pdf --blank b.pdf` reads like the result it produces.
- **The plan file** is for everything else: thousands of items, generated jobs, rebuildable bundles under version control, and per-blank text and styling.

Both are parsed into the same `assembly.Sequence`. There is no behavior reachable from one surface that the other cannot express, except per-blank styling beyond text, which the command line deliberately leaves to the plan: a command line that carries a style per blank is a plan file written badly.

## Scale

The design target is thousands of source files and thousands of blank pages in one run.

- **No per-blank rewrite.** Blanks are not inserted into an already-merged file one at a time. The ordered list of sources and generated blanks is merged once, so cost grows with the size of the output, not with (blanks × output size). A 6,000-item, 15,000-page assembly takes about two seconds.
- **Render once, reuse.** Each *distinct* blank (size, background, text, font, placement) is rendered once into the workspace and referenced for every page that shares it; a style identical across thousands of pages costs one small file.
- **Concurrent preflight.** Source PDFs are validated in parallel, each exactly once even if listed repeatedly.
- **All failures at once.** Validation collects every failing input and reports a count plus the first twenty, instead of one failure per run.
- **Early failure.** Blank styles — colors, fonts, text encodability — are checked before any PDF is opened.
- **Bounded descriptors.** The merge opens one source at a time; no job can exhaust the process's file-descriptor limit.
- **Progress, not noise.** An interactive terminal gets one self-erasing progress line; redirected output stays clean.
- **Arguments are not the transport.** The command line cannot carry thousands of paths on Windows, so plans can be read from standard input (`--plan -`).

Memory is proportional to the assembled output because the embedded PDF engine builds the result in memory; the 15,000-page run above peaks near 240 MB.

## Choosing the plan format

Plans are strict JSON.

| Candidate | Verdict |
| --- | --- |
| **JSON** | Chosen. Standard library only (no dependency to ship, license, or audit); unambiguous types; every language and shell (`jq`, PowerShell `ConvertTo-Json`, Python, Go) generates it correctly; JSON Schema gives editors completion and validation; strict decoding gives precise errors. Weakness: no comments — mitigated by the plan being a *description* (the file names carry meaning) and by `$schema` tooling. |
| TOML | Pleasant for hand-written configuration, but this document is an *ordered, heterogeneous list of thousands of entries*, which TOML expresses through verbose `[[items]]` tables. Needs a third-party parser. |
| YAML | Comments and terse lists, but implicit typing (`no`, `1e3`, `0755`, unquoted dates) is a hazard for file names and text, indentation errors are easy to make in generated output, and it needs a third-party parser. |
| Line-oriented text | Cannot carry per-blank text and style. |
| A DSL | A new language to document, parse, and get wrong. |

The command line stays the way to express *small* jobs without a file; the plan is the way to express *anything*.

### Why items are strings or objects

A PDF is by far the commonest item, so a bare string is a PDF path: a thousand-file plan is a thousand short lines. Blanks and groups are objects because they carry data. A string is never interpreted (no `"--blank"` sentinel inside a path list), so no file name can collide with a directive.

### Why groups have a `dir`

"The structure of directories" in a plan is the repeated prefix of paths. A group's `dir` removes the repetition without scanning anything: the plan still lists exactly the files that are assembled.

### Why no globbing or directory scanning

Both put an ordering policy (lexical? natural? locale? case-folded?) where nobody can see it, and the answer differs between Windows and Unix. A plan with explicit paths is reviewable (`--dry-run`), diffable, and identical on every platform. The cost — generating the list — is a one-line script, shown in [`PLAN.md`](PLAN.md#generating-plans).

## `--blank`, `@`, and other spellings

`--blank` stays. It is inert in every shell: `bash`, `zsh`, `fish`, `cmd.exe`, and PowerShell all pass it through untouched. Its one cost is that it shares the option namespace, so a file literally named `--blank` needs `--` or `./--blank`; the `--` escape is standard and cheap.

An `@blank` token was considered and rejected. `@` is meaningful to PowerShell (splatting: `@name`, array `@()`), and by long convention (`javac`, `gcc`, MSBuild) `@file` means "read arguments from this response file" — a different thing from a directive. Both are traps for the people and agents most likely to generate command lines.

The rule that makes the grammar unambiguous: a separate option value may never be a recognized option, so `-o --blank` fails loudly instead of naming an output file `--blank`; values that legitimately start with `-` use `--option=value`.

## Blank appearance

The settings and their behavior are specified in [`BLANK_PAGES.md`](BLANK_PAGES.md). The reasoning:

- **Anchor plus offset is the only placement syntax.** Nine anchors with an offset express every position, absolute lower-left coordinates included, so there is one way to say it rather than two.
- **Offsets use the PDF convention** (`+x` right, `+y` up), so one coordinate system serves the plan and the page.
- **Size inherits from the neighbor by default**, so a blank after a landscape or rotated page looks like it belongs there.
- **Every setting is either set or unset**, and layers merge field by field. Setting a field to its default is still setting it, so a blank can override a layer below it with any value.

### Why the standard fonts

Every viewer supplies the twelve standard fonts, nothing is embedded, output stays small, and the published metrics let the engine measure text exactly. The cost is the character repertoire (Windows-1252). A character outside it is a precise, early error, never a silently wrong glyph. Embedding user-supplied fonts is the natural way to widen it and is not provided.

### Why blanks are generated as one-page PDFs

Each distinct blank becomes a tiny single-page PDF merged into the sequence, so assembly has one code path and one backend operation (merge), and the generated pages are checked by the same validator as every output.

## Human UX

The short case is readable without documentation:

```text
pdfconcat -o Annex.pdf cover.pdf contents.pdf --blank evidence.pdf
```

`--dry-run` exists because positional assembly is easy to inspect before creation. It reports resolved absolute paths, output page ranges, source page counts, each blank's resolved size and style, and the expected total. Normal successful runs print one concise line.

## Agent UX

An agent needs a representation that is deterministic, composable, and difficult to misinterpret:

- one strict, schema-described format (`--print-schema`) whose errors name the JSON path;
- no environment dependence, no globbing, no includes;
- stable relative-path resolution against the plan file;
- `--plan -` to avoid temporary files and `--json` to avoid scraping text;
- exit status 2 for usage errors and 1 for operational failures; and
- the same ordering semantics on the command line and in a plan.

An agent can generate a plan, run `--dry-run --json`, inspect the interpreted result, and then run without `--dry-run`.

## Failure philosophy

PDFConcat prefers explicit failure over speculative repair.

It does not:

- enable backend network activity for this assembly workflow;
- create synthetic merge bookmarks as an undocumented side effect;
- normalize every PDF through a second renderer;
- silently switch backend;
- repair malformed inputs with Ghostscript;
- modify originals; or
- publish an output that has not passed final validation and page-count verification.

A backend incompatibility is an actionable error, not permission to transform source material in an undocumented way.

## Unsupported by design

PDFConcat has no compatibility mode for earlier conventions, and none is planned:

- `BLK` in filenames, `_blank.pdf`, and timestamped `_concat_*` outputs;
- implicit alphabetical directory discovery;
- a line-oriented plan file with `--blank` as a line;
- Ghostscript fallback or normalization options; and
- shell-function flags.

The plan format carries `"version": 1` so that a future incompatible change is explicit rather than silent.
