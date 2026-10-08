# CLI contract

This document defines PDFConcat's public command-line language: commands, options, what they print, exit statuses, and how to produce large jobs from a shell. Changes to this grammar are product changes. The plan format has its own contract in [`PLAN.md`](PLAN.md); how generated pages look is in [`BLANK_PAGES.md`](BLANK_PAGES.md). `pdfconcat --help` and `pdfconcat help COMMAND` print the same options as structured data, and tests keep the two in agreement with this document.

## Commands

```text
pdfconcat build  --plan FILE|- [-o FILE] [options]
pdfconcat build  --plan-json JSON [-o FILE] [options]
pdfconcat build  -o FILE [options] [--] PDF|--blank ...
pdfconcat check  (the same three forms as build)
pdfconcat report FILE [--part ID | --page N | --view parts|diagnostics] [--offset N] [--limit N] [--details]
pdfconcat schema plan|report|response
pdfconcat version
pdfconcat help [COMMAND]
```

| Command | Does |
| --- | --- |
| `build` | Assembles the PDF and publishes it. |
| `check` | Does everything `build` does short of creating a PDF: validates and snapshots the inputs, resolves every generated page's size and style, and reports a build-ready layout. It needs no output; when an output is named, by `-o` or by the plan's `output`, it also checks the destination, so a plan whose `output` exists fails `check` unless `--overwrite` is given. |
| `report` | Reads a report saved by `--report` and answers one question about it. It never reopens a PDF. |
| `schema` | Prints the JSON Schema of a plan, complete report, or structured response. |
| `version` | Prints the version, commit, and commit date. `pdfconcat --version` is the same. |
| `help` | Prints help. `pdfconcat help build`, `pdfconcat build --help`, and `pdfconcat build -h` are the same. |

The command is the first argument. Options before it are not accepted, except `--help`, `--version`, and `--format` (`pdfconcat --version`, `pdfconcat --format text --help`).

A typical agent workflow: write a plan, `check --plan job.json --report job.report.json`, read the summary, fetch failures or selected parts with `report`, edit the plan, `check` again, then `build`.

## Options

An option applies to specific commands; the parser rejects an option that does not apply to the selected command and names the options that do. `root` is the position before a command.

| Option | Applies to | Meaning |
| --- | --- | --- |
| `--plan FILE\|-` | build, check | Read the plan from `FILE`, or from standard input with `-`. |
| `--plan-json JSON` | build, check | Take the whole plan as one inline JSON argument, decoded exactly like a file. For small jobs. |
| `--blank` | build, check | Direct operands only: insert one generated page at this position. Repeat for several. It takes no value. |
| `--base-dir DIR` | build, check | Base directory for relative paths of a plan from `-` or `--plan-json`. |
| `-o FILE`, `--output FILE` | build, check | Output PDF, relative to the working directory. Overrides the plan's `output`. |
| `--overwrite` | build, check | Permit replacing an existing output or report file. |
| `--report FILE` | build, check | Save the complete result to `FILE`, relative to the working directory. |
| `--jobs N` | build, check | Concurrent source inspections, at least 1. Default: the smaller of 4 and the CPU count. |
| `--details` | build, check, report | For `build` and `check`: print the complete result instead of the summary. For `report`: expand the selected records (`--part`, `--page`, or `--view`) to full resolved values; without a selection it is an error, so a query never prints a whole saved report. |
| `--expect-attempt ID` | report | Reject a saved report from a different attempt; correlation only, not integrity or current PDF validity. |
| `--part ID` | report | Show one contribution by id: a JSON pointer such as `/items/42/items/3`, or `argv:N`. |
| `--page N` | report | Show the contribution covering output page `N` (1-based). |
| `--view parts\|diagnostics` | report | List contributions or diagnostics, paged and in report order. |
| `--offset N` | report | First record of a `--view` (default 0). |
| `--limit N` | report | Records per `--view` page, 1 to 100 (default 20). |
| `--format json\|text` | root, build, check, report, version, help | Standard output as compact JSON (default) or human-readable text. |
| `--version` | root | Same as the `version` command. |
| `-h`, `--help` | all | Show help for the command. |

### How values are written

- A value follows its option (`-o out.pdf`) or is attached (`--output=out.pdf`). `-o` has no attached form.
- A separate value may not start with `-`: `-o --blank a.pdf` is an error, not an output file named `--blank`, and a misspelled option is never swallowed as a file name. The one exception is `--plan -`, where `-` means standard input. A value that does start with `-` is attached: `--output=-draft.pdf`, `--limit=-1` (which is then rejected as negative).
- Paths and ids must not be empty. `--plan-json=` may be: the plan decoder rejects empty input with its own located diagnostic.
- Each option may appear once, under either spelling. Options without a value (`--overwrite`, `--details`, `--help`, `--version`, `--blank`) reject an attached one: `--overwrite=yes` is an error.
- Numbers are plain decimal digits that fit 64 bits. `--page` is at least 1, `--offset` and `--limit` are not negative, and `--jobs` is at least 1. The range of a page size (`--limit` 1 to 100) is judged by the report query, not the parser.
- Only the documented long and short option forms are accepted; unknown options, abbreviations and extra aliases are errors. There is no `--dry-run`, `--json`, `--print-schema`, or `--blank-*`: use `check`, the default JSON output, `schema plan`, and plan files.

### Help and errors

The line is validated left to right, option by option, and the first fault ends parsing. `--help` is itself only recorded, so **an invalid argument anywhere on the line is reported even when `--help` is present**, before or after it: `pdfconcat build --bogus --help` and `pdfconcat build --help --bogus` both fail with `usage_unknown_option`. Help is shown when every argument that was read is valid. Requirements that need the whole line, a plan source for `build` and `check`, a `FILE` for `report`, are not enforced for a help request: `pdfconcat build --help` is help. After `--`, `--help` is a file name. `--help` and `--version` cannot be combined. `--format text` anywhere before the fault makes the error text, and a `--format` after the fault or an invalid one does not.

## Plan sources

`build` and `check` take instructions from exactly one of three sources. They are mutually exclusive, and the error names both sides of the conflict.

| Source | Use |
| --- | --- |
| `--plan FILE` | A [plan file](PLAN.md). Relative paths in it resolve against the plan file's directory. The supported transport for large jobs. |
| `--plan -` | The plan on standard input. Relative paths resolve against `--base-dir`, or the working directory. |
| `--plan-json JSON` | The plan as one argument, for small jobs. Same decoder, same limits, same errors as a file; every per-page appearance, count, group, and font is available. Relative paths resolve against `--base-dir`, or the working directory. |
| Direct operands | PDF paths and `--blank` in output order, for simple jobs. Relative paths resolve against the working directory. |

### Direct operands

```console
pdfconcat build -o out.pdf a.pdf --blank b.pdf        # blank between documents
pdfconcat build -o out.pdf --blank a.pdf b.pdf        # leading blank
pdfconcat build -o out.pdf a.pdf b.pdf --blank        # trailing blank
pdfconcat build -o out.pdf a.pdf --blank --blank b.pdf
```

The position of `--blank` is the position of a generated page, which inherits its size from a neighboring source page. `--blank` takes no value: there is no `--blank=TEXT` and no styling option. Text, background, size, fonts, and counts belong in a plan (`pdfconcat schema plan`), including an inline `--plan-json` plan. A path may appear more than once; each occurrence is assembled at its position. PDFConcat never expands wildcards, so `a*.pdf` is passed to the program as the shell expanded it, or literally.

Paths and arguments must be valid UTF-8. Raw byte filenames outside Unicode are unsupported; invalid argv is rejected before any file operation. Path-resolving commands also require a UTF-8 working directory (`working_directory_invalid_utf8`, exit 1); change to a Unicode working directory before retrying.

Operands must not be empty. A lone `-` is not an operand: standard input is only `--plan -`.

### Literal names: `--` and `./--blank`

`--` ends option parsing: every later argument is a literal operand, including `--blank`, `--help`, `-`, and a second `--`.

```console
pdfconcat build -o out.pdf -- --blank          # a PDF file named --blank
pdfconcat build -o out.pdf ./--blank           # the same file, without --
pdfconcat build -o out.pdf --blank ./--blank   # a generated page, then that file
```

`./--blank` (or `.\--blank` on Windows) is the portable spelling because it works anywhere in the line. After `--` an operand is only a path, so no further `--blank` pages can be requested on that line; mix the two with `./` instead. The same holds for `report -- --saved.json`.

### `--base-dir`, `-o`, and plan `output`

| Path | Resolves against |
| --- | --- |
| Item paths, `dir`, `output`, and root font files of a named plan file | The plan file's directory (a font declared inside a group resolves against that group; see [`PLAN.md`](PLAN.md#where-a-font-file-is-resolved)). |
| The same for `--plan -` and `--plan-json` | `--base-dir DIR`, or the working directory. |
| Direct operands | The working directory. |
| `-o`, `--output` and `--report` | Always the working directory. |

`--base-dir` only chooses where a plan from standard input or an inline plan starts; it does not change the working directory. It is rejected with a named plan file (whose own directory is the base) and with direct operands. `-o` overrides the plan's `output`, and `-o` is never resolved against `--base-dir`: with `--plan - --base-dir /project -o out.pdf` the file is `out.pdf` in the working directory, while the same plan's `"output": "out.pdf"` would be `/project/out.pdf`.

## Output

Standard output is compact JSON by default, for help, version, results, and failures alike; scripts never need `--format text`. `--format text` renders summaries as concise lines; explicit `--details` payloads use indented JSON so every value remains available. JSON is the stable interface to parse. Nothing else is printed to standard output: no banners, no progress. Progress appears only on an interactive terminal on standard error.

- `build` and `check` print a small summary: status, whether the PDF was published, source, generated, and total page counts, the number of diagnostics, and at most five of them. The summary says it is a summary. Default summaries and structured command failures fit within 2 KiB including the newline. With `--report FILE` they give the exact next command when it fits; otherwise `next_omitted: true` directs callers to their original report argument. `truncated_fields` marks path/location previews, which must not be used as complete filenames. The diagnostic preview may show fewer than five records to stay within the byte budget.
- `--details` prints the complete result on standard output instead, in the chosen format.
- `--report FILE` saves the complete result: every contribution with its source location, output page range, and resolved appearance, and every diagnostic with its stage, stable code, location, and message. Complete reports include producing tool/version/commit/toolchain/platform identity; a verified output records its SHA-256 `output_digest`, computed while writing. These identify the captured run and bytes; queries do not establish current file validity. JSON object member order is unspecified. The saved file is a regular JSON document that standard tools can read. See `pdfconcat schema report`.
- `schema plan`, `schema report`, and `schema response` print the raw JSON Schema, with no wrapper. `schema` has no `--format`.
- Command errors omit job lifecycle fields; job failures retain known lifecycle and publication facts. Both share diagnostic stage/code/location/recovery vocabulary. Messages are for reading; `code` is the stable identifier to match. Separate `cause` retains report I/O details when the message leads with a safe repair.

A late publication failure retains a report describing the PDF commit and the failed original report target. `recovery_state: "current"` means the retained report metadata was refreshed atomically. If refresh I/O fails, `publication_pending` marks complete layout data whose publication metadata still describes the planned success; use the outer committed state for the actual outcome. `unavailable` means its owned identity was lost: preserve the unknown file and do not copy it as a report. Manual recovery does not change the captured run’s publication history. On a copied report query, a long historical recovery path is referenced through `complete_report.publication.recovery_report`, because its directory cannot be derived from the current report operand.

When a late report failure has an overlong recovery path, the bounded result gives the lossless `recovery_basename` and `recovery_directory_from: "original_report_argument"`: combine that basename with the directory of the original `--report` path. Complete details preserve full paths. Never use a truncated preview as a recovery command. Recovery instructions use POSIX `cp -n` or, on Windows, PowerShell `[System.IO.File]::Copy` with literal quoted paths and overwrite disabled; choose a distinct unused report target when the original target is unsafe. Named plan/report/font/source inputs must be regular files; FIFOs and devices are rejected without waiting for a writer. Stdin plan pipes remain supported.

### Querying a saved report

```console
pdfconcat report job.report.json                                # summary
pdfconcat report job.report.json --view diagnostics             # first 20 diagnostics
pdfconcat report job.report.json --view diagnostics --offset 20 --limit 20
pdfconcat report job.report.json --view parts --limit 100       # contributions in order
pdfconcat report job.report.json --part /items/42               # one contribution
pdfconcat report job.report.json --part argv:3 --details        # its full resolved values
pdfconcat report job.report.json --page 5001                    # the contribution covering output page 5001
```

`--part`, `--page`, and `--view` are mutually exclusive. `--offset` and `--limit` page a `--view` and are errors without one. Paged responses state the total, how many records were returned, and the next offset, which is null after the last record. Page numbers are 1-based; a page outside a complete layout, an unknown part id, and an incomplete layout are structured errors, never empty successes.

### What `--overwrite` covers

`--overwrite` permits replacing an existing file at the explicitly named output artifacts: the PDF (`-o` or the plan's `output`) and the `--report` file. Without it, either existing file is an error and publication uses the operating system's no-clobber primitive. It never permits replacing a source PDF, the plan file, a font file, or any file the job reads: an output that is also an input is refused with or without `--overwrite`. Symbolic-link and non-regular targets are always refused. New PDF/report basenames in the same filesystem directory are conservatively compared after Unicode normalization and case folding, even on case-sensitive filesystems. Choose clearly distinct names; actual filesystem identities are rechecked at publication, including before report replacement after PDF commit. `check` creates no PDF; an output it is given (`-o` or the plan's `output`) only checks what `build` would be allowed to do. A failure report written before the instructions were fully understood is only created at a new path, whatever `--overwrite` says.

## Exit status

| Status | Meaning |
| ---: | --- |
| `0` | Success, including help, version, schema, a passing `check`, and report queries. |
| `2` | Invalid instructions: a malformed command line, an invalid plan, a failed layout (including text that overflows or cannot be rendered), a font file that is not a usable font, a destination or file-role conflict (an existing output without `--overwrite`, an output that is also an input, a symbolic-link or non-regular target), a source that is not a regular file, an invalid report query, and a saved report that is malformed or of an unsupported version. |
| `1` | Operational failure: a file that cannot be read or found (source, font, plan, or saved report), a source the PDF engine cannot read (malformed, encrypted), a source that changed while it was being captured, scratch space, verification, or publication. |
| `130` | Interrupted. Cancellation before the native PDF commit preserves the destination; an atomic publication that already completed is not undone. |

If the PDF was published and a later step, such as writing the report or standard output, fails, the exit status is `1` and the result says `"published": true`.

## Usage errors

A rejected command line is a diagnostic with stage `usage`, a stable code, an `argv_index` (the zero-based position of the argument in the arguments after the executable's name, so `build -o --blank` faults at `argv:1`), and a message that names the option and the expected value. Parsing stops at the first fault. Faults that concern the line as a whole, a missing plan source or file, have no location.

| Code | Raised when |
| --- | --- |
| `usage_invalid_utf8` | An argument contains invalid UTF-8; paths and arguments must use Unicode text. |
| `usage_missing_command` | No command was given. |
| `usage_unknown_command` | The first argument, or the operand of `help`, is not a command. |
| `usage_command_not_first` | An operand appears before the command. |
| `usage_unknown_option` | The option does not exist. |
| `usage_inapplicable_option` | The option exists but not for this command. |
| `usage_duplicate_option` | An option is given twice, under either spelling. |
| `usage_missing_value` | A value option is last on the line. |
| `usage_dash_value` | A separate value starts with `-`; attach it with `=`. |
| `usage_unexpected_value` | An option that takes no value was given one, including `--blank=TEXT`. |
| `usage_empty_value` | A path, id, format, or operand is empty. |
| `usage_invalid_value` | `--format` is neither `json` nor `text`. |
| `usage_invalid_jobs` | `--jobs` is not a whole number of at least 1. |
| `usage_conflicting_actions` | `--help` and `--version` were both given. |
| `usage_plan_source_conflict` | More than one of `--plan`, `--plan-json`, and direct operands. |
| `usage_missing_plan_source` | `build` or `check` has no instructions. |
| `usage_base_dir_conflict` | `--base-dir` with a named plan file or with direct operands. |
| `usage_missing_operand` | `report` has no `FILE`, or `schema` no name. |
| `usage_unexpected_operand` | An extra operand for `report`, `schema`, `version`, or `help`. |
| `usage_stdin_operand` | `-` as an operand. |
| `usage_unknown_schema` | `schema` was given a name other than `plan`, `report`, or `response`. |
| `report_selection_conflict` | More than one of `--part`, `--page`, `--view`. |
| `report_paging_needs_view` | `--offset` or `--limit` without `--view`. |
| `report_details_need_selection` | `report --details` without `--part`, `--page`, or `--view`. |
| `report_unknown_view` | `--view` is neither `parts` nor `diagnostics`. |
| `report_invalid_number` | `--page`, `--offset`, or `--limit` is not a whole number that fits 64 bits, or `--page` is below 1. |
| `report_invalid_paging` | `--offset` or `--limit` is negative. |

## Large jobs and shells

No command line can portably carry thousands of paths. Windows `CreateProcess` accepts at most 32,767 characters for the whole command line, and `cmd.exe` at most 8,191. Linux limits a single argument to 128 KiB (which bounds `--plan-json`) and all arguments together to about 2 MiB; macOS to about 1 MiB. Use a plan file, or standard input, for anything long or generated, and `--plan-json` or direct operands only for short jobs. Prefer a file for complex text containing quotes or newlines.

A plan lists exactly the files that are assembled, in exactly that order, so generating one means choosing and writing down an ordering. Generate it with a tool that sorts explicitly and writes UTF-8. Nothing here is portable between file systems beyond what they store: paths are literal, and a name that is not valid Unicode cannot be written in a JSON plan.

### Python: the portable recipe

One deterministic recipe for every operating system. The ordering is stated in the code (code-point order of the `/`-separated path, independent of locale), the file is written as UTF-8 regardless of the console or locale, a name that cannot be encoded fails loudly instead of being replaced, zero files is an explicit error, and one file is still a JSON array.

```python
import json
import pathlib
import subprocess

files = sorted(path.as_posix() for path in pathlib.Path("chapters").glob("*.pdf"))
if not files:
    raise SystemExit("no PDF files in chapters/")
plan = {"version": 1, "items": files}
pathlib.Path("job.json").write_text(json.dumps(plan, ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
raise SystemExit(subprocess.run(["pdfconcat", "build", "--plan", "job.json", "-o", "book.pdf"]).returncode)
```

The plan's paths are relative to `job.json`, which is next to the working directory here. Replace `glob` with `rglob` to descend into subdirectories, or sort by another key; whatever the key, write it in the script. The script returns PDFConcat's own exit status.

### bash and zsh

Globs expand in sorted order, but the order follows the locale, so fix it with `LC_ALL=C` (byte order, which is code-point order for UTF-8). `jq --args` carries any name, including quotes and newlines, as an array element. `nullglob` makes a pattern with no match expand to nothing in bash, so an empty list reaches PDFConcat and is rejected as an empty `items`; without it bash passes the pattern text itself, and zsh stops with `no matches found`.

```sh
( export LC_ALL=C; shopt -s nullglob
  jq -n '{version: 1, items: $ARGS.positional}' --args chapters/*.pdf > job.json )
pdfconcat build --plan job.json -o book.pdf
```

In zsh omit `shopt`. The list is limited by the argument limits above; for more files use the Python recipe.

Inline JSON is quoted with single quotes: `pdfconcat build --plan-json '{"version":1,"items":["a.pdf","b.pdf"]}' -o out.pdf`.

### PowerShell

State the order explicitly with an ordinal sort (UTF-16 code-unit order, which equals code-point order for names in the Basic Multilingual Plane), and force an array with `@(...)` and a `[string[]]` cast so zero or one file still becomes a JSON array. Write a file with an explicit encoding: piping text to a native program uses `$OutputEncoding`, which in Windows PowerShell 5.1 is ASCII and would replace non-ASCII characters in names. A leading byte order mark, which Windows PowerShell 5.1's `utf8` adds, is accepted. `.pdf` is matched without regard to case, as Windows does.

```powershell
$items = [string[]]@(Get-ChildItem chapters -File | Where-Object { $_.Extension -ieq '.pdf' } | ForEach-Object { 'chapters/' + $_.Name })
[Array]::Sort($items, [StringComparer]::Ordinal)
@{ version = 1; items = $items } | ConvertTo-Json -Depth 3 | Set-Content job.json -Encoding utf8
pdfconcat build --plan job.json -o book.pdf
```

`--blank` is an ordinary argument in PowerShell. Quote values that contain spaces, commas, semicolons, `$`, or parentheses. A word starting with `@` is PowerShell syntax (splatting, array expressions), which is why PDFConcat does not use `@blank`. Inline JSON is written with single quotes in PowerShell 7.3 and later; Windows PowerShell 5.1 and earlier versions drop embedded double quotes from native arguments, so use a file.

### cmd.exe

`cmd.exe` has no way to build a plan from a directory listing worth recommending; generate the plan with PowerShell or Python and then run it. Quote any path with spaces in double quotes. The program receives standard Windows command-line parsing, so inside an argument a double quote is written `\"`:

```bat
pdfconcat build -o "Annual Report.pdf" "chapters\01 Intro.pdf" --blank "chapters\02 Body.pdf"
pdfconcat build --plan-json "{\"version\":1,\"items\":[\"a.pdf\",\"b.pdf\"]}" -o out.pdf
pdfconcat build -o out.pdf .\--blank
```

`cmd.exe` expands `%NAME%` even inside double quotes, so a name containing `%`, and a text with `^`, `&`, or `!` under delayed expansion, cannot be written reliably on the command line: use a plan file.

### Why not `find | sort | jq -R`?

Newline-delimited text is a lossy transport for file names: a name containing a newline becomes two items, a locale-dependent `sort` makes the order differ between machines, `jq -R` silently replaces bytes that are not valid UTF-8 with U+FFFD so the plan names a different file, and without `pipefail` the pipeline reports the exit status of its last command even when `find` failed or found nothing. A plan is the one place that fixes the order and the exact file names, so generate it from a list that is never split on a delimiter: shell globs with `--args`, or the Python recipe.

## Response and report format 2

All structured responses and complete saved reports carry `format_version: 2`. Input plans remain version 1. Command errors (`kind: error`) have no build lifecycle fields. Help gives labeled argv templates whose input/output filenames must be replaced, and a concrete executable continuation when it fits. Omitted help continuations expose `executable_from` and exact `next_args`, so callers need not parse a shell example.

Every build/check execution has one opaque `attempt_id`, including failed decoding and preflight. The same identity is retained in its saved or recovery report and broken-stdout committed-state receipt. This identifies an attempt; it is neither authentication nor proof that a current PDF or source still matches captured evidence.

A successful report query has `kind: report_query`, `command: report`, `status: ok`, and exit 0. `saved_run` gives the captured attempt's identity, command and status; `result` carries its summary or selected records. Publication targets inside `result` are historical. Summary queries label them with `publication_context: historical_target`. Queries never reread PDFs or rerun preflight. A successful read of failed-job diagnostics is still a successful query.

Concrete `next` arrays use the currently invoking executable and absolute path of the report actually selected, including a copied/renamed/symlinked report. Execute arrays unchanged under the same environment. Job follow-ups include `--expect-attempt ID`; a replaced report is rejected as `report_attempt_mismatch`. No suggestion executes automatically.

If an exact continuation cannot fit, `next` is null, `next_omitted` is true, and `next_reference` states the action/view/expected attempt. Reconstruct argv from the current invoking executable and the caller-owned `original_argv.--report` value (jobs) or `original_argv.report_operand` (queries). Resolve a relative original argument in that invocation's working directory, not a later query directory. Preview paths must never be used as executable paths. Full diagnostic values remain available through `--details` or the saved current report; `location_from` references the exact recovery location in `complete_report.diagnostics/N/recovery/location` when a preview cannot carry it.

Requested-report publication facts separately say `report_write: written|not_written` and `report_target_observation: unknown`. If the working directory cannot resolve a target, `report_from: original_argv.--report` identifies the caller-owned request while the target path remains absent. No file is opened or written in that case. They make no global unchanged-file promise during concurrency. An existing target can contain historical evidence. Until complete resource/alias inventory, even `--overwrite` permits a failure report only at an unused target. Repair the input and choose a new report target; do not treat an older target as evidence for the new attempt.

Diagnostic recovery actions are a finite vocabulary: `open_help`, `edit_input`, `choose_new_report`, `inspect_report`, `recover_report`. They are guidance with declaration or invocation references, not shell commands. Late report-publication failure retains the existing no-rebuild recovery procedure and owned recovery metadata.

Only complete format-2 reports are read. Old report-version-1 evidence is preserved and rejected non-destructively. Inspect its raw JSON with ordinary tools. A fresh `check` of the current job to a NEW report target creates current evidence; it cannot reconstruct a historical run. Never rebuild/overwrite merely to upgrade a report.
