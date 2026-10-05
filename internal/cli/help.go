// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package cli

import "fmt"

// HelpText returns the complete command-line help text.
func HelpText() string {
	return `PDFConcat assembles PDFs in an explicit order and inserts generated blank pages at explicit positions.

Usage:
  pdfconcat -o OUTPUT.pdf [options] ITEM...
  pdfconcat [-o OUTPUT.pdf] [options] --plan PLAN.json
  pdfconcat [-o OUTPUT.pdf] [options] --plan -        (plan from standard input)

An ITEM is a PDF path or --blank. Items appear in the output in the order given.
For more than a few dozen files, use a plan file: command lines are limited
(about 32 KB on Windows) and shells differ in how they expand wildcards.

Sequence:
  --blank              Insert one generated blank page at this position.
  --blank=TEXT         Insert one blank page printing TEXT (overrides --blank-text).
  --                   Treat every later argument as a PDF path.

Options:
  -o, --output FILE    Output PDF path (required unless the plan names one).
      --plan FILE      Read the sequence from a JSON plan file ('-' for stdin).
      --overwrite      Replace an existing output once a verified result is ready.
      --dry-run        Validate and report the assembly without creating output.
      --json           Report the result (or dry-run) as JSON on stdout.
      --print-schema   Print the JSON Schema of plan files and exit.
  -h, --help           Show this help.
      --version        Show build version information.

Generated-blank defaults (apply to every blank; the plan can refine each one):
      --blank-size SIZE         inherit (default), A4, Letter, ..., or 210x297mm
      --blank-background COLOR  Page fill color, #RRGGBB or #RGB (default: none)
      --blank-text TEXT         Text printed on the page (default: none)
      --blank-font NAME         Standard font, e.g. Helvetica, Times-Roman, Courier
      --blank-font-size LENGTH  Default 12pt
      --blank-color COLOR       Text color, default #000000
      --blank-anchor ANCHOR     top-left, top, top-right, left, center (default),
                                right, bottom-left, bottom, bottom-right
      --blank-x LENGTH          Move the text right of its anchor (negative: left)
      --blank-y LENGTH          Move the text up from its anchor (negative: down)
      --blank-width LENGTH      Text wraps at this width (default: page width - 72pt)
      --blank-align ALIGN       left, center (default), right, justify
      --blank-leading NUMBER    Line height as a multiple of font size, default 1.2

LENGTH is a number with optional unit: 12, 12pt, 20mm, 2cm, 0.5in (bare = points).
Use --option=VALUE for a value that begins with '-'.

Examples:
  pdfconcat -o Annex.pdf 01.pdf 02.pdf --blank 03.pdf
  pdfconcat -o Annex.pdf --blank-text "Intentionally left blank" a.pdf --blank b.pdf
  pdfconcat --plan Annex.json --dry-run
  jq -n '{version:1,items:$ARGS.positional}' --args *.pdf | pdfconcat -o all.pdf --plan -

Exit status: 0 success, 1 operational failure, 2 usage error.
`
}

// VersionText formats release metadata for --version.
func VersionText(build BuildInfo) string {
	return fmt.Sprintf("pdfconcat %s\ncommit: %s\ncommit date: %s\n", build.Version, build.Commit, build.CommitDate)
}
