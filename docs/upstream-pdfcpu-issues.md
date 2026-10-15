# Upstream issue drafts

These drafts describe behavior reproduced with [pdfcpu v0.16.2](https://github.com/pdfcpu/pdfcpu/releases/tag/v0.16.2). Recheck the target upstream version and related reports before posting. Obtain maintainer approval and attach only minimal synthetic fixtures. PDFConcat's supported refusals and adapters are described in [DESIGN](DESIGN.md).

## Reproduction inputs

From a source checkout, export the comparison fixtures into an existing empty directory:

```sh
PDFCONCAT_COMPARISON_DIR=/absolute/path/to/empty-directory go test ./internal/pipeline -run '^TestComparisonFixtures$' -count=1
```

The exporter in [comparison_test.go](https://github.com/resoltico/pdfconcat/blob/main/internal/pipeline/comparison_test.go) writes `plain-a.pdf`, `links.pdf`, `named.pdf`, `legacy-dests.pdf`, `form.pdf`, `outlined.pdf`, `signed.pdf`, `fitmarks.pdf` and `damaged.pdf`. Use fresh output paths for each CLI command. Additional shapes are defined by [PDF engine tests](https://github.com/resoltico/pdfconcat/blob/main/internal/pdf/engine_test.go), [fork-boundary tests](https://github.com/resoltico/pdfconcat/blob/main/internal/pdf/fork_regression_test.go) and [pdffixture](https://github.com/resoltico/pdfconcat/tree/main/internal/pdffixture). Run upstream API reproductions with unmodified pdfcpu, rather than through PDFConcat's refusals/adapters.

## Merge loses or misdirects named destinations

Related report: [pdfcpu #591](https://github.com/pdfcpu/pdfcpu/issues/591).

```sh
pdfcpu merge -c disable -b=false out.pdf plain-a.pdf named.pdf named.pdf
```

Input: two copies with `chap1`/`chap2` destinations and links between their two pages. Expected first-copy link targets after the plain page: pages 3/2. Observed targets: 5/4, in the later copy.

API variant: read and validate a plain context, a NamedDests context and another context whose destination names are distinct `chapA`/`chapB`; call `MergeXRefTables` in that order and write. The earlier tree can disappear even without a name collision. Expected: all distinct names survive, and colliding names are namespaced with references rewritten within their occurrence. PDFConcat detaches and rebuilds destinations.

## Repeated legacy Dests use last-wins merging

Related report: [pdfcpu #676](https://github.com/pdfcpu/pdfcpu/issues/676).

```sh
pdfcpu merge -c disable -b=false out.pdf plain-a.pdf legacy-dests.pdf legacy-dests.pdf
```

Input: repeated legacy Dests names used by links in each source. Expected first-copy targets: 3/2. Observed targets: 5/4. Later definitions redirect earlier links. Namespace later names and rewrite their local references instead of overwriting earlier definitions. PDFConcat rebuilds legacy destinations alongside its name tree.

## Validation silently replaces damaged page-tree children

API reproduction: `ReadContext`, then `ValidateContextWithReport` on exported `damaged.pdf`. A child of the Pages Kids array is `<< >>`; its sibling has ordinary content. Validation returns no error, PageCount 2 and no notice, although the corrupt child is repaired into blank content. Request: refuse the shape or provide an observable content-replacing repair notice.

Also test a Page with non-null Kids and its own visible content. Validation accepts it, but merging can discard that Page's content. The engine policy tests construct both shapes. PDFConcat refuses them before validation.

## RectForArray panic on malformed input

Related report: [pdfcpu #1189](https://github.com/pdfcpu/pdfcpu/issues/1189).

Pass [malformed-form-fields-panic.pdf](https://github.com/resoltico/pdfconcat/blob/main/internal/pdf/testdata/malformed-form-fields-panic.pdf) through upstream `ReadContext` and `ValidateContextWithReport`. Validation can panic in `XRefTable.RectForArray` with `index out of range [3] with length 3`. Expected: return an error for the malformed rectangle. PDFConcat's raw page-tree guard refuses this fixture before validation; its exported PDF boundaries also recover ordinary panics.

## Core fonts silently drop unencodable generated text

Use the `core.json` produced by [run-comparison.sh](https://github.com/resoltico/pdfconcat/blob/main/scripts/run-comparison.sh):

```sh
pdfcpu create -c disable core.json core.pdf
pdftotext core.pdf -
```

The Helvetica text is `Pielikums Ā ļ ķ — Ελληνικά Кириллица`. Creation exits 0, but extraction retains `Pielikums` and the dash while the tested letters/words disappear. Expected: explicit unencodable-character refusal or warning. PDFConcat embeds Noto Sans and checks scripts and glyph coverage.

## Resize leaves destination coordinates unchanged

```sh
pdfcpu resize -c disable "form:A4P, rotate:off" in.pdf out.pdf
```

Input: link annotations targeting XYZ/FitH/FitV/FitR destinations. The annotation Rect is scaled, but destination coordinates remain in the source coordinate system. Expected: transform destination coordinates or document the scroll-position limitation. PDFConcat documents this limitation and retains the correct page target.

## Merge retains only the first AcroForm default resources

API reproduction: read/relaxed-validate two forms, call `MergeXRefTables`, then write. Form ONE uses `/DA /Helv 12 Tf` and `/DR /Font /Helv` = Helvetica; form TWO uses `/DA /Cour 12 Tf` and `/DR /Font /Cour` = Courier. Both request NeedAppearances. Poppler reports `Unknown font tag 'Cour'` when rendering the second page: its DA retains Cour, but the output DR contains only Helv.

The same resource name bound to different fonts also needs safe handling. Expected: merge/namespace resources and rewrite references, or refuse incompatible forms. PDFConcat requires equivalent resolved DR and form-level DA/Q representations. Its actual field-name mappings account for backend namespacing even without collisions. See `TestNarrowFormDefaultsDigest` in the engine tests for the fixture builders.

## Canonical private names are decoded twice

```sh
pdfcpu resize -c disable "form:A4P, rotate:off" private-crop-key.pdf out.pdf
```

Input: MediaBox 306×396, visible text at (72,200), private key `/Cr#236fpBox [20 20 80 80]`, and no real CropBox. The key decodes once to `Cr#6fpBox`, which is distinct from CropBox. Upstream lookup decodes it again, treats it as CropBox and crops away the visible text; extraction is empty. Expected: operate on canonical bytes and escape exactly once when serializing. PDFConcat refuses ambiguous literal-hash-plus-hex names; ordinary literal hashes remain supported.

## Resize retains UserUnit while writing target coordinate dimensions

Related report: [pdfcpu #905](https://github.com/pdfcpu/pdfcpu/issues/905).

Resize a UserUnit 2 page with `form:A4P, rotate:off`. Inspect UserUnit and MediaBox using qpdf JSON: raw A4 coordinates remain multiplied by UserUnit 2, so the physical sheet is twice A4. Expected: normalize units or account for them in output box dimensions. The engine's fitting tests construct unit/crop/rotation cases; PDFConcat removes local UserUnit after resize.

## Relaxed parser boundary and real precision

Related report: [pdfcpu #1506](https://github.com/pdfcpu/pdfcpu/issues/1506).

Keep valid-input fidelity and intentional relaxed repair policy as separate topics. Ordinary and packed objects can accept material suffixes such as `1.0 junk`, duplicate decoded keys including null omissions, malformed numeric tokens such as `1e20`/`0+5`, and malformed free-head states. Request an explicit, observable repair/refusal policy rather than assuming every tolerated malformed document is a defect.

Valid-input concerns: real serialization rounds to at most twelve fractional digits, and some legal comment-separated reference spellings are rejected. An example source reference is shown below; the line break is significant:

```text
/Contents 4 0%second
R
```

Expected: preserve valid numeric/reference meaning or expose the unsupported boundary. `TestUpstreamNumericSerializationLimitIsExplicit` and the object-syntax cases in the engine tests define current product controls. PDFConcat documents the precision/comment-reference limits and does not provide a second PDF parser.
