# Generated blank pages

A generated blank is a page with a size, an optional background fill, and an optional block of text. This document is the single reference for how blanks look; [`CLI.md`](CLI.md) and [`PLAN.md`](PLAN.md) describe where to write the settings.

## Settings

Every setting is optional. Anything left unset falls through to the next layer (see [Layering](#layering)).

| Setting | CLI option | Plan key (under `blank`) | Default | Meaning |
| --- | --- | --- | --- | --- |
| Page size | `--blank-size` | `size` | `inherit` | `inherit`, a paper name, or `WIDTHxHEIGHT[unit]`. |
| Background | `--blank-background` | `background` | none | Page fill, `#RRGGBB` or `#RGB`. None leaves the page unpainted. |
| Text | `--blank-text`, `--blank=TEXT` | `text.value` | none | The text. Empty means no text. A newline starts a new line. |
| Font | `--blank-font` | `text.font` | `Helvetica` | A [standard font](#fonts-and-characters). |
| Font size | `--blank-font-size` | `text.size` | `12pt` | A [length](#lengths). |
| Text color | `--blank-color` | `text.color` | `#000000` | `#RRGGBB` or `#RGB`. |
| Anchor | `--blank-anchor` | `text.anchor` | `center` | The [page point](#text-placement) the text block attaches to. |
| Offset | `--blank-x`, `--blank-y` | `text.x`, `text.y` | `0` | Moves the block from its anchor: `+x` right, `+y` up. |
| Block width | `--blank-width` | `text.width` | page width − 72pt | Width of the text block; lines wrap here. |
| Alignment | `--blank-align` | `text.align` | `center` | `left`, `center`, `right`, `justify`. |
| Leading | `--blank-leading` | `text.leading` | `1.2` | Line height as a multiple of the font size, 1 to 10. |

`--blank=TEXT` sets the text of one blank only; the other command-line options apply to every blank in the run. In a plan, a blank's own `blank` object holds any of the keys above.

## Page size

- `inherit` takes the size of the nearest preceding source page *as displayed* (MediaBox with `/Rotate` applied), so a blank after a landscape page is landscape. A leading blank takes the size of the first source page. Consecutive blanks share one size.
- A paper name — `A3`, `A4`, `A5`, `B4`, `B5`, `Letter`, `Legal`, `Tabloid` — is portrait. Write landscape explicitly.
- `WIDTHxHEIGHT[unit]` gives an exact size with one shared unit, for example `297x210mm` or `8.5x11in`.

## Lengths

A length is a number with an optional unit: `pt` (default; 1/72 inch), `mm`, `cm`, or `in`. In a plan file a length is a JSON number (points) or a string with a unit.

## Text placement

The text is a block `width` wide holding the wrapped lines. The anchor picks the page point the block attaches to, one of `top-left`, `top`, `top-right`, `left`, `center`, `right`, `bottom-left`, `bottom`, `bottom-right`:

- horizontally, a left anchor puts the block's left edge at the point, a right anchor its right edge, and a centered anchor centers the block on it;
- vertically, a top anchor hangs the block below the point, a bottom anchor stands it above, and a middle anchor centers it.

The offset then shifts the block. Anchor plus offset expresses every position, absolute lower-left coordinates included (`bottom-left` with an offset). `align` justifies each line within the block.

Wrapping breaks at spaces; runs of spaces collapse; a word wider than the block overflows rather than being split. `justify` never stretches the last line of a paragraph.

Examples:

| Intent | Settings |
| --- | --- |
| Block 25 mm in from the top-left corner | anchor `top-left`, x `25mm`, y `-25mm` |
| Footer 15 mm above the bottom edge | anchor `bottom`, y `15mm` |

## Fonts and characters

The twelve standard PDF fonts, which every viewer supplies without embedding: `Helvetica`, `Helvetica-Bold`, `Helvetica-Oblique`, `Helvetica-BoldOblique`, `Times-Roman`, `Times-Bold`, `Times-Italic`, `Times-BoldItalic`, `Courier`, `Courier-Bold`, `Courier-Oblique`, `Courier-BoldOblique`. Names are case-insensitive.

Text must be in the Windows-1252 repertoire: Western European Latin letters, common punctuation, and `€`. Any other character — for example Latvian `ā`, `ē`, `ģ`, or any non-Latin script — fails the run with an error naming the character, before any PDF is read. Embedding fonts for wider repertoires is not supported.

## Layering

For each blank, each setting comes from the first layer that sets it:

1. the blank's own settings (a plan's per-blank `blank` object, or `--blank=TEXT`);
2. `--blank-*` command-line options;
3. the plan's top-level `blank` defaults;
4. the built-in defaults above.

Settings merge one by one: a blank that sets only its text keeps the font, color, and background from lower layers. Writing a setting's default value still counts as setting it. Passing `--blank-*` options when the sequence has no blank is an error.

## Identical blanks

Blanks that resolve to the same size, background, and text are rendered once and reused, however many pages share them.
