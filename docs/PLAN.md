# Plan v2

A plan is one UTF-8 JSON object. Version 1 is rejected with an explanation:
write `version: 2` and rename top-level/item `blank` to `page`. The CLI's
`--blank` operand still means a plain generated page.

```json
{
  "version": 2,
  "output": "packet.pdf",
  "dir": "sources",
  "page": { "text": { "value": "Appendix", "size": 24 } },
  "items": [
    "cover.pdf",
    { "page": {}, "count": 2 },
    { "dir": "chapters", "items": ["01.pdf", "02.pdf"] },
    { "page": { "background": "#F4EFE6", "text": { "value": "Pielikums Ā ļ ķ" } } }
  ]
}
```

## Members and paths

| Member | Meaning |
| --- | --- |
| `version` | Required integer token `2` |
| `items` | Required nonempty array of source strings, generated items or groups |
| `output` | Output PDF; CLI `-o` overrides it |
| `dir` | Prefix for relative paths inside top-level items |
| `page` | Default generated-page style |
| `fit_to` | Fit every output page onto a paper name or explicit sheet size |
| `$schema` | Optional editor schema URL; ignored string |

A source is a literal path string. A generated item has `page` and optional
`count` (default 1). A group has both `dir` and nonempty `items`. Unknown members
are errors. Wrong combinations of known item members are `plan_item_shape`;
unknown members take precedence when encountered.

The base directory is the named plan file's supplied directory. For stdin it is
`--base-dir`, or the working directory. Source paths and item font files resolve
against their enclosing items directory, changed by top-level/group `dir`.
Output and fonts declared in top-level `page` resolve against the base directory.
CLI output/report paths resolve against the working directory.

Paths are joined lexically: `a/link/../b.pdf` becomes `a/b.pdf`. Absolute paths
remain absolute. Empty/NUL paths are invalid. Windows drive-relative `C:x` and
rooted-without-drive `\x` are refused; ordinary relative, absolute and UNC paths
use `filepath` conventions. Input symlinks to regular files are supported.

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

`inherit` uses the last page of the nearest preceding source; leading generated
pages use the first page of the first following source. Displayed size means
CropBox intersected with MediaBox, rotation applied, multiplied by UserUnit.
Generated pages never change the neighbor used by later inheritance. With
`fit_to`, inherited pages use the target size. Without a source or `fit_to`, give an
explicit size. Inherited generated sides must be 1–14,400 pt; source-only pages
are not subject to that generated-size limit.

Paper names are portrait: `A3`, `A4`, `A5`, `B4`, `B5`, `Letter`, `Legal`,
`Tabloid`. Use dimensions such as `297x210mm` for landscape. Each generated side
is 1–14,400 pt. Lengths are JSON numbers in points or decimal strings with an optional
`pt`, `mm`, `cm`, `in` suffix; omitted units mean points. Strings contain no spaces or exponents. Offsets stay within
±14,400 pt. `fit_to` accepts the same sizes except `inherit`.

## Text placement

The anchor is `top-left`, `top`, `top-right`, `left`, `center`, `right`,
`bottom-left`, `bottom`, or `bottom-right`. It attaches the matching point of the
logical text block to the page. Offsets move that block. Alignment positions
individual lines inside it, independently of the anchor.

For nonempty text, the logical block is at least its configured width, even for a short word; an overflowing line can make it wider. Its height uses
font ascent/descent plus `leading × size` per additional baseline. Ink bounds
also account for bearings and shaped mark offsets. Moving a full-width block
sideways can overflow even when its text looks short; give an appropriate width.

```json
{
  "version": 2,
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

Used glyph outlines are pruned with stable IDs and composite closure; job-wide
font resources are identical across generated items and deduplicated by the
backend. A no-subsetting permission retains the full font program. To discard
an unusable inherited font when clearing text, also set `font` to `default`.
See [notices](../THIRD_PARTY_NOTICES.md).

## Strictness and limits

The decoder stops at the first encountered fault: unknown/wrong-case member,
duplicate member, null, wrong JSON type, grammar/range error, trailing data,
invalid UTF-8 or unpaired surrogate. One leading UTF-8 BOM is accepted and counted
in offsets. Offsets are zero-based; line/column are one-based bytes. Well-formed
v1 documents get the migration diagnostic regardless of member order.

Limits: 64 MiB original JSON, 64 group levels, 100,000 flattened items, count
1–100,000 and 100,000 total generated pages. `count` needs an integer token:
`3.0` and `3e0` are rejected. Missing/unsupported version is
`plan_version_unsupported`; missing items is `plan_type`; empty arrays and low
counts are `plan_value`; excessive/overflowing counts are `plan_limit`.

`pdfconcat schema plan` prints the editor schema. Standard JSON Schema cannot
distinguish integer lexemes such as `3` and `3.0`; encoding, duplicate members,
converted unit bounds, platform paths and aggregate limits also require the
decoder. Schema validity alone is not a successful check of a packet.
