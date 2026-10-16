# Plan v3

A plan is one UTF-8 JSON object with `version: 3`. Use source objects to select
input pages, generated items for extra pages, and containers for sections or
shared path scopes. For whole files and plain blanks, the CLI also accepts PDF
operands and `--blank`. See the [installation guidance](../README.md#use-a-published-release)
for documentation matching a source build or published binary.

```json
{
  "version": 3,
  "output": "packet.pdf",
  "dir": "inputs",
  "page": { "size": "A4", "text": { "size": 24 } },
  "page_numbers": {},
  "items": [
    { "source": "cover.pdf" },
    { "contents": {} },
    {
      "title": "Evidence",
      "dir": "evidence",
      "start_on": "odd",
      "divider": {},
      "items": [
        { "source": "report.pdf", "pages": [[2, 4], [7, 7]] },
        { "title": "Figures", "items": [{ "source": "plots.pdf" }] }
      ]
    },
    { "page": { "text": { "value": "Appendix" } }, "count": 2 }
  ]
}
```

## Members and paths

| Member | Meaning |
| --- | --- |
| `version` | Required integer token `3` |
| `items` | Required nonempty array of the four item shapes below |
| `output` | Output PDF; CLI `-o` overrides it |
| `dir` | Scope for relative paths in top-level items |
| `page` | Default generated-page style |
| `fit_to` | Fit every output page onto a paper name or explicit sheet size |
| `page_numbers` | Optional physical footer numbering configuration |
| `$schema` | Optional editor schema URL; ignored string |

Each item is exactly one of:

- A source object with required `source` and optional `pages` selection.
- A generated item with `page` and optional `count` (default 1).
- A contents item with `contents`.
- A container with nonempty `items` and at least one declared `dir`, `title` or
  `start_on`; `divider` is allowed only with `title`.

Unknown members are errors. Wrong combinations of known members are
`plan_item_shape`; encountered unknown members retain first-fault precedence.
Each source needs its own object; a bare path string is not an item.

The base directory is the named plan file's supplied directory. For stdin it is
`--base-dir`, or the working directory. Source paths and item font files resolve
against their enclosing items directory, changed by top-level/container `dir`.
Output and fonts declared in top-level `page` resolve against the base directory.
A container's `dir` applies to its children; a font declared in that container's
`divider` resolves in the enclosing item scope, before the container's `dir`.
CLI output/report paths resolve against the working directory.

Paths are joined lexically: `a/link/../b.pdf` becomes `a/b.pdf`. Absolute paths
remain absolute. Empty/NUL paths are invalid. Windows drive-relative `C:x` and
rooted-without-drive `\x` are refused; ordinary relative, absolute and UNC paths
use `filepath` conventions. Input symlinks to regular files are supported.

## Source selections

`pages` is a nonempty array of positive inclusive integer pairs, such as
`[[2,4],[7,7]]`. Pairs must ascend without duplicates, overlaps or reversal.
Adjacent pairs normalize to one range. A bound beyond the original source page
count is `source_selection_out_of_range`. Omission and an explicit full-range
selection have the same whole-source behavior. To repeat or reorder material,
use separate source occurrences; there is no page-expression language.

The entire source is still validated and hashed. Selection cannot hide a
signature, encryption, damage or another whole-source refusal. The original
source version still contributes to the output version. Source PageLabels are
removed. Selection composes visible pages; it is not redaction and does not
promise that every omitted-page byte or private object is erased.

A genuinely partial selection of a source with nonempty form fields or any
widgets is `source_selection_form_unsupported`. Full-range forms keep the
existing form rules. Fitting and effective-PDF-2.0 appearance compatibility use
the retained selected view, so omitted annotations and targets do not create
those compatibility refusals. See [PDF policy and limitations](DESIGN.md).

Local links and destinations to omitted pages are removed within their own
occurrence; they never jump into a different occurrence of the same source.
Retained source bookmarks preserve their native navigation beneath the
innermost authored section. Removed navigation produces
`source_navigation_removed`; an excessive composed outline hierarchy is
`source_outline_depth_unsupported`. URI and remote targets remain outside local
page selection. This is neither a sanitizer nor a universal PDF-fidelity claim.

## Sections, title dividers and odd starts

A container with `title` is an authored section. Its JSON pointer is its identity;
repeated titles remain separate sections. Containers without a title only group
or align material and create no navigation entry. Sections nest in authored
order; source bookmarks appear beneath their nearest titled section. Ordinary
generated pages create no section of their own.

Titles are nonempty single-line Unicode strings, not whitespace-only, with no
control or format characters, and at most 10,000 characters. Their original text
is preserved without trimming or normalization. The supported-script, glyph and
font rules below apply when a title is painted in a divider or contents. A
bookmark-only title does not require a font it never uses.

`divider: {}` adds exactly one generated title page before the children. Its
settings layer over root `page` defaults, but its text is always the section
title. Declaring `divider.text.value` is forbidden, including an empty value.
A section without `divider` adds no title page.

`start_on` is `any` by default or `odd`. An odd boundary inserts at most one blank
immediately before the container when its next physical page would otherwise be
even. Nested starts naturally share the same odd page. The automatic blank uses
the preceding physical sheet's displayed size, or the global fit target. It has
no background, text or footer. If that size cannot form a supported generated
canvas, fit the packet or omit odd alignment. This describes physical PDF page
positions; it does not configure a printer or impose sheets.

All pages count from 1: covers, contents, dividers, authored blanks and automatic
padding. A section targets its first non-padding page, including its divider.
Its inclusive range excludes its own entry padding but includes child padding.
For example, after cover page 1, an unaligned parent whose first odd-start child
inserts padding page 2 and content page 3 has range `[2,3]`, target 3; the child
has range `[3,3]`, target 3. A section's range start and navigation target can
differ. Reports, bookmarks, contents and footers use these same physical pages.

## Linked contents

At most one `contents` item is allowed, at any position, including inside a
section. It requires at least one authored section. Its optional `title` defaults
to `Contents`; optional `size` defaults to `fit_to`, otherwise A4. An explicit
size is authored before fitting. Without a contents item, no contents pages are
created, and ordinary plans need no sections.

Contents lists all authored sections once in preorder, including its containing
section, with wrapped titles, final physical target numbers and clickable
whole-page links. It does not list itself or imported source bookmarks. It uses
bundled Noto Sans, black text, an 18pt repeated heading, 12pt entries, 36pt
margins, 12pt indentation per section depth, 6pt row gaps and leading 1.2.
A whole row stays on one page. The number column has fixed measured digit cells;
final numbers do not reflow titles or change the measured contents page count.
Overwide indentation, unsupported text or a row too tall for a page is refused;
titles are not split across pages, truncated or shrunk.

Enabled footers reserve their conservative physical envelope plus 6pt clearance
before contents pagination, including an explicit contents canvas fitted to a
different sheet size. A high footer margin can therefore reduce usable contents
space or cause a refusal.

## Physical footer numbers

Presence of `page_numbers` enables physical Arabic numbers; omission disables
painting. An empty object enables the defaults. The only fields are `size`
(default 9pt, positive length) and `margin` (default 8mm, nonnegative length).
Numbers use black bundled Noto Sans at the bottom center, with the bottom ink
edge at `margin`, after fitting. Every non-padding page is numbered, including
covers, authored blanks, dividers and contents. Automatic odd-start padding
counts in the sequence and remains visually blank.

There are no resets, separate logical folios, Roman numerals or exclusions.
A footer may cover existing source content; there is no overlap detector or
content shifting. Unusable placement is refused before publication. Footer
placement requires both logical and painted bounds to fit, so a zero bottom
margin can fail even when the painted digit fits. Refusals identify the affected
edges and suggest margin, font-size or sheet-size changes appropriate to them.
Footer insertion preserves original source streams and geometry on unfitted pages;
fitted pages preserve their immediate post-fit state outside the overlay.

## Generated pages and layering

Each setting comes from item `page`, then top-level `page`, then built-ins. Text
members merge individually. `font` is atomic: a file object replaces the whole
choice; `default` explicitly selects the embedded font. `background: none` clears
a fill; `value: ""` clears text. Empty text still validates its selected font.

| Setting | Built-in | Values |
| --- | --- | --- |
| `size` | `inherit` | Paper name or `WIDTHxHEIGHT[unit]` |
| `background` | `none` | `#RGB`, `#RRGGBB`, `none` |
| `text.value` | Empty | Up to 10,000 characters |
| `text.font` | `default` | Built-in Noto Sans or object with `file` |
| `text.size` | 12 pt | Length, 1–14,400 pt |
| `text.color` | `#000000` | RGB hex color |
| `text.anchor` | `center` | Nine-point anchor |
| `text.x`, `text.y` | 0 | Offsets, +x right and +y up |
| `text.width` | Page width minus 72 pt | Length, 1–14,400 pt |
| `text.align` | `center` | `left`, `center`, `right`, `justify` |
| `text.leading` | 1.2 | Baseline distance / font size, 1–10 |
| `text.overflow` | `error` | `error`, `allow` |

`inherit` uses the last page of the nearest preceding selected source. Leading
generated pages use the first selected page of the first following source.
Displayed size means CropBox intersected with MediaBox, rotation applied,
multiplied by UserUnit. Generated pages, dividers and contents never change the
source neighbor used by later inheritance. With `fit_to`, inherited pages use
the target size. Without a source or `fit_to`, give an explicit size. Inherited
generated sides must be 1–14,400 pt; source-only pages are not subject to that
generated-size limit.

Paper names are portrait: `A3`, `A4`, `A5`, `B4`, `B5`, `Letter`, `Legal`,
`Tabloid`. Use dimensions such as `297x210mm` for landscape. Each generated side
is 1–14,400 pt. Lengths are JSON numbers in points or decimal strings with an
optional `pt`, `mm`, `cm`, `in` suffix; omitted units mean points. Strings contain
no spaces or exponents. Offsets stay within ±14,400 pt. `fit_to` accepts the same
sizes except `inherit`.

## Text placement

The anchor is `top-left`, `top`, `top-right`, `left`, `center`, `right`,
`bottom-left`, `bottom`, or `bottom-right`. It attaches the matching point of the
logical text block to the page. Offsets move that block. Alignment positions
individual lines inside it, independently of the anchor.

For nonempty text, the logical block is at least its configured width, even for
a short word; an overflowing line can make it wider. Its height uses font
ascent/descent plus `leading × size` per additional baseline. Ink bounds also
account for bearings and shaped mark offsets. Moving a full-width block sideways
can overflow even when its text looks short; give an appropriate width.

```json
{
  "version": 3,
  "output": "column.pdf",
  "items": [{ "page": {
    "size": "A4",
    "text": {
      "value": "Right-hand column that wraps",
      "anchor": "top-right", "x": "-20mm", "y": "-20mm",
      "width": "60mm", "align": "left"
    }
  }}]
}
```

Wrapping is greedy at U+0020. Leading paragraph spaces and internal space runs
are retained; spaces consumed by a wrap disappear at that break. Trailing spaces
have no measured line width and are not justified. NBSP and other accepted spaces
are glyphs, not break opportunities. Justification stretches interior ordinary
spaces on wrapped lines, except the last line of a paragraph.

CRLF, CR and LF are line breaks. One final line break adds nothing; adjacent
breaks create an empty line. Letters/marks are not normalized. TAB, other controls,
U+2028/U+2029 and format characters (joiners, bidi controls, soft hyphen) are
refused. Supported scripts are left-to-right Latin, Greek, Cyrillic and suitable
Common/Inherited characters. RTL/complex scripts are refused even if a font has
their glyphs. Missing glyph sequences are errors.

Default overflow refuses a word wider than the block or logical/ink bounds past
a page edge. `allow` places measured overflow and warns; it does not shrink,
truncate or normalize text. Empty text has no text geometry; whitespace can have
logical bounds without ink. A nonempty value on a page whose default margins
leave width below 1 pt is refused even with `allow`; specify a valid explicit
width or a wider page. Explicit authored sizes are laid out before fitting.

## Fonts

`default` is embedded Noto Sans Regular, with mark shaping and ToUnicode/ActualText
extraction. A file choice must be static TrueType with glyf outlines. Collections,
variable/CFF fonts, damaged fonts, restricted embedding and bitmap-only embedding
permissions are refused. No system-font lookup is performed.

Text drawn with the same font shares its embedded program. Unused glyph
outlines are removed when the font's permissions allow subsetting; otherwise the
full program is retained. To discard an unusable inherited font when clearing
text, also set `font` to `default`.

A font's PostScript name must meet the PDF name-safety policy: a literal hash
followed by two hexadecimal digits is refused by `check` and `build` as
`font_invalid`. Use a font with a different PostScript name. Ordinary hashes and
names resembling subset names remain supported. See the [font metadata
requirements](DESIGN.md#text-and-pruning) and [notices](../THIRD_PARTY_NOTICES.md).

## Strictness and limits

The decoder stops at the first encountered fault: unknown/wrong-case member,
duplicate member, null, wrong JSON type, grammar/range error, trailing data,
invalid UTF-8 or unpaired surrogate. One leading UTF-8 BOM is accepted and counted
in offsets. Offsets are zero-based; line/column are one-based bytes. Only plan
version 3 is supported; version checks follow the same first-fault order.

Limits: 64 MiB original JSON, 64 container levels, 100,000 authored item nodes
(including containers), and 100,000 explicit selection pairs across the plan.
`count` is 1–100,000. The actual generated-page total is at most 100,000,
including dividers, contents and automatic padding. Source selections and
repeated generated runs stay compact; selected source pages have no arbitrary
new page-count cap. Actual totals must fit the backend's representable page
index. These limits are not a constant-memory guarantee.

`count`, page indexes and `version` need integer tokens: `3.0` and `3e0` are
rejected. Missing/unsupported version is
`plan_version_unsupported`; missing items is `plan_type`; empty arrays and low
counts are `plan_value`; excessive/overflowing counts are `plan_limit`.

`pdfconcat schema plan` prints the editor schema. Standard JSON Schema cannot
distinguish integer lexemes such as `3` and `3.0`; encoding, duplicate members,
converted unit bounds, platform paths and aggregate limits also require the
decoder. Schema validity alone is not a successful check of a packet.
