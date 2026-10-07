# Generated blank pages

A generated blank is a page with a size, an optional background fill, and an optional block of text. This document is the single reference for how blanks look. Appearance is written only in the JSON [plan](PLAN.md); the plan's `blank` object holds the defaults and each blank item has its own `blank` object. Simple jobs given on the command line use the default appearance.

## Settings

Every setting is optional. Anything left unset inherits from the next layer (see [Layering](#layering)).

| Setting | Plan key (under `blank`) | Default | Meaning |
| --- | --- | --- | --- |
| Page size | `size` | `inherit` | `inherit`, a paper name, or `WIDTHxHEIGHT[unit]`. |
| Background | `background` | `none` | Page fill, `#RRGGBB` or `#RGB`; `none` leaves the page unpainted. |
| Text | `text.value` | empty | The text. Empty means no text. `\n` starts a new line. |
| Font | `text.font` | `default` | The built-in font, or a [font file](#fonts-and-characters). |
| Font size | `text.size` | `12pt` | A [length](#lengths), 1 to 14400 points. |
| Text color | `text.color` | `#000000` | `#RRGGBB` or `#RGB`. |
| Anchor | `text.anchor` | `center` | The [page point](#text-placement) the text block attaches to. |
| Offset | `text.x`, `text.y` | `0` | Moves the block from its anchor: `+x` right, `+y` up. |
| Block width | `text.width` | page width − 72pt (at least 1pt) | Width of the text block; lines wrap here. The block is always this wide, however short the text. |
| Alignment | `text.align` | `center` | `left`, `center`, `right`, `justify`. |
| Leading | `text.leading` | `1.2` | Distance from one baseline to the next, as a multiple of the font size (not of the font's own line gap), 1 to 10. |
| Overflow | `text.overflow` | `error` | `error` or `allow`; see [Overflow](#wrapping-and-overflow). |

Names are exact lower case (`top-left`, not `Top-Left`); paper names are exact (`A4`). An omitted setting inherits; `null` is an error. To clear an inherited background write `"background": "none"`; to clear inherited text write `"value": ""`.

## Page size

- `inherit` takes the size of the neighboring source page as displayed: the visible page rectangle (the effective CropBox, limited by the MediaBox), with `/Rotate` and `UserUnit` applied. A leading blank takes the size of the first following source page; any other blank takes the size of the nearest preceding source page. An explicitly sized blank does not change what later blanks inherit: inheritance always comes from source pages.
- A job made only of blanks has no source page to inherit from, so every blank needs an explicit size. An inherited size with no source neighbor is an error that says so.
- A source page larger than 14400 points is kept unchanged in the output; only a generated blank that would inherit such a size is rejected, with an instruction to give an explicit size.
- A paper name (`A3`, `A4`, `A5`, `B4`, `B5`, `Letter`, `Legal`, `Tabloid`) is portrait. Write landscape explicitly, for example `297x210mm`.
- `WIDTHxHEIGHT[unit]` gives an exact size with one shared unit, for example `297x210mm` or `8.5x11in`. Written sizes must have both sides between 1 and 14400 points (200 inches). Generated pages have a clean origin and `UserUnit` 1.

## Lengths

A length is a JSON number of points, or a string with a decimal number and an optional unit: `pt` (default; 1/72 inch), `mm`, `cm`, or `in`, without spaces. Every length is finite and within ±14400 points; font size and block width are at least 1.

## Text placement

Coordinates are in points (1/72 inch) unless a length gives a unit. Placement has one model: a nine-point **anchor** on the page plus **x/y offsets**. `+x` moves right and `+y` moves up, as in PDF. The anchor is one of `top-left`, `top`, `top-right`, `left`, `center`, `right`, `bottom-left`, `bottom`, `bottom-right`.

The text is a **block** `width` wide holding the wrapped lines. The block is always the full `width` (the default is the page width minus 72 pt, a 36 pt margin on each side), even when the text is a single short word, and it is taller by one line advance for each line. For one line the block is the font's ascent plus its descent high (about 1.36 times the font size for the default font); for several lines, add `leading × size` per extra line. Block bounds use the font's advance widths, ascent, and descent, not the ink of the glyphs. The anchor chooses where the *block* attaches to the page:

- horizontally, a left anchor puts the block's left edge at the anchor point, a right anchor its right edge, and a centered anchor centers the block on it;
- vertically, a top anchor hangs the block below the anchor point, a bottom anchor stands it above, and a middle anchor centers it.

The offsets then shift the whole block. Absolute coordinates are the same model: `bottom-left` plus `x` and `y` measured from the lower-left corner of the page to the lower-left corner of the block.

`align` is a different thing: it places each *line inside* the block. A block can be anchored at the right edge of the page while its lines are left-aligned. The default `align` is `center`, so a short text in a block anchored at the left edge still appears in the middle of the block; write `"align": "left"` to put it at the anchor.

Because the block is as wide as `width`, shifting it by more than the margin pushes it past the page edge, and the default overflow policy then rejects it. Give a smaller `width` whenever you move a block sideways.

Examples (A4 page, 595 x 842 pt):

| Intent | Settings |
| --- | --- |
| Block 25 mm in from the top-left corner, lines flush left | anchor `top-left`, x `25mm`, y `-25mm`, width `100mm`, align `left` |
| Footer line centered, its block 15 mm above the bottom edge | anchor `bottom`, y `15mm` |
| Text starting 100 pt right of and 50 pt above the lower-left corner | anchor `bottom-left`, x `100`, y `50`, width `200`, align `left` |
| Right-hand column, 20 mm from the top and right edges, lines starting at the same edge | anchor `top-right`, x `-20mm`, y `-20mm`, width `60mm`, align `left` |

The last row as a complete plan (a job of only generated pages needs an explicit size):

```json
{
  "version": 1,
  "output": "column.pdf",
  "items": [
    {
      "blank": {
        "size": "A4",
        "text": {
          "value": "Right-hand column that wraps",
          "anchor": "top-right",
          "x": "-20mm",
          "y": "-20mm",
          "width": "60mm",
          "align": "left"
        }
      }
    }
  ]
}
```

## Wrapping, whitespace, and overflow

Text is split into paragraphs at line breaks, and each paragraph is wrapped to `width` with the real advance widths of the chosen font. The rules are exactly these:

- A line break in the value is `\n` in JSON. CRLF and a lone CR are line breaks too. One final line break adds nothing; a line break directly after another gives an empty line (`"a\n\nb"` is three lines).
- Wrapping is greedy and breaks only at the ordinary space, U+0020. A word is a run of other characters.
- Spaces at a wrap point are consumed by the break. Leading spaces of a paragraph and runs of spaces inside a line are kept and take width. Trailing spaces never count toward a line's width and are never justified.
- U+00A0 and every other Unicode space character are ordinary glyphs: they are never a break opportunity, never collapsed, and never stretched. A TAB is rejected (use spaces), as are other control characters, U+2028 and U+2029 (use a line break), and format characters such as joiners, bidirectional controls, and the soft hyphen. Text is never normalized: what you write is what is placed.
- `align` places each line in the block: `left`, `center`, `right`, or `justify`. `justify` widens the interior U+0020 spaces of every wrapped line so it fills `width`; the last line of each paragraph, and any line without an interior space, stays left-aligned.
- The value holds at most 10,000 characters.

By default (`"overflow": "error"`) the layout is rejected, when a word is wider than the block, or when the block or positioned glyph ink extends beyond any page edge. Actual font bearings, combining-mark offsets, baseline positions and justification all contribute to ink bounds; a negative left bearing is rejected even when the advance-based block fits. The message gives the measured widths and spans. Text is never silently shrunk, clipped, or truncated. `"overflow": "allow"` places the text exactly as specified even when it extends past the page, for deliberate off-page art, and the report records the policy and findings. The report preserves computed block `bounds` and separate `ink_bounds` even when overflow rejects a page. Unavailable geometry is null; null ink bounds also describe text with no visible ink. Empty text gives a background-only page. Whitespace-only text has null ink bounds while retaining logical block overflow checks.

## Fonts and characters

The built-in font (`"font": "default"`) is Noto Sans Regular, embedded in the program and licensed under the SIL Open Font License 1.1 (see [`THIRD_PARTY_NOTICES.md`](../THIRD_PARTY_NOTICES.md)). It covers Latin, including Latvian and the other European alphabets, Greek, and Cyrillic, and the text is shaped with the font's own mark positioning, so combining marks sit correctly. Text is not normalized: a precomposed `ā` and `a` followed by U+0304 each print as written, and extraction from the PDF returns the characters you wrote. Nothing depends on fonts installed on the system, and the font is written once, whole, into any PDF that uses it.

```json
{
  "version": 1,
  "output": "latvian.pdf",
  "items": [
    {
      "blank": {
        "size": "A5",
        "text": {
          "value": "Pielikums\nĀ ļ ķ š ž, a\u0304 l\u0327; Ελληνικά; Кириллица",
          "size": 16,
          "leading": 1.5
        }
      }
    }
  ]
}
```

Accepted text is left-to-right Latin, Greek, and Cyrillic, together with left-to-right or direction-neutral characters that belong to no script (digits, punctuation, symbols, spaces) and combining marks, plus line breaks. Right-to-left characters are rejected even when their Unicode script is Common, as with the Arabic question mark. The written sequence must shape into glyphs supplied by the font; font composition may supply a combined glyph even when an individual combining mark has no nominal glyph. A sequence the font cannot render is an error naming the character and its position, never a replacement box. Any other script (for example Arabic, Hebrew, Devanagari, Thai, or Han) is rejected with an error naming the first offending character, even if a supplied font has glyphs for it, because this tool does not do right-to-left or complex-script layout. Script and glyph-availability constraints are checked before source PDFs are captured or inspected, without assuming a page size. Diagnostics locate the declaration that supplied the effective field, including top-level defaults. Identical explicit overrides remain distinct repair locations even when their appearance is rendered once. Full diagnostics carry ordered `consumers` references to every affected report part; bounded summaries omit these lists. Shaping validates the actual selected glyph outlines during layout.

To use another font, give a TrueType file: `"font": { "file": "fonts/Example.ttf" }`. It must be a static font with TrueType (`glyf`) outlines whose embedding permissions allow embedding; variable fonts, OpenType/CFF fonts, font collections, and malformed files are rejected with an error that names the file. The path resolves against the directory of the plan scope that declares it (see [PLAN.md](PLAN.md#where-a-font-file-is-resolved)); the file is copied once per job, and a font is identified by the content of its bytes, not by its path. A font file cannot also be an output, source, or plan file. The chosen font is written whole into each PDF that uses it. Names of standard PDF fonts such as `Helvetica` are not fonts here: `font` is `"default"` or a file object.

## Layering

For each blank, each setting comes from the first layer that sets it:

1. the blank item's own `blank` object;
2. the plan's top-level `blank` defaults;
3. the built-in defaults above.

Settings merge one by one: a blank that sets only its text keeps the font, color, and background from lower layers. Writing a setting's default value still counts as setting it. The one exception is the font, which is a single atomic choice: `"font": { "file": ... }` replaces the inherited font entirely, and `"font": "default"` selects the built-in font even when the defaults name a file.

## Identical blanks

Blanks that resolve to the same size, background, text, and font choice (including the resolved font file path and captured content identity) are rendered once and reused, however many pages share them. Different font paths remain distinct report choices even when their bytes match; their font objects in the generated resource PDF still share one embedding by content identity. A build shapes and immediately writes one distinct page at a time into the shared resource PDF, then retains only bounds and findings. Fonts and extraction mappings remain shared across the job; a check retains geometry without rendering.
