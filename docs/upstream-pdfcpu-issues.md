# Upstream issue drafts

Drafts only. No upstream issue has been posted. Duplicate searches and the final
latest-release check remain pending WP10. The owner decides what to post.
The current runtime pin is the tested upstream v0.16.2.

## Merge loses or misdirects named destinations

Repeated sources reuse destination names, redirecting first-copy links into the
last copy. Distinct names in different API sources can also replace earlier trees.
Reproduce with the bundle's apiprobe and NamedDests fixtures. Expected: each
source occurrence retains its own targets and all distinct names remain reachable.
PDFConcat works around this by detaching, renaming and rebuilding destinations.

## Repeated legacy Dests use last-wins merging

Merge two LegacyDests sources. Earlier links using the same legacy names redirect
to the later copy. Expected: namespace the later names and rewrite its references.
PDFConcat rebuilds legacy destinations alongside its name tree.

## Validation silently replaces damaged page-tree children

An empty dictionary in a Pages Kids array can validate with no report notice and
become a blank/omitted page. A Page with non-null Kids can also lose its content.
Expected: fail or supply a content-replacing repair notice. Small raw-tree fixtures
are in the product tests. PDFConcat refuses these shapes before validation.

## RectForArray panic on malformed input

The bundle's malformed-form-fields-panic.pdf provokes an upstream validation
panic. Expected: an error. PDFConcat's raw tree guard now rejects that fixture
before validation; exported backend boundaries also recover ordinary panics.

## Core fonts silently drop unencodable generated text

Create Helvetica text containing Latvian letters and Greek. The CLI succeeds while
some characters disappear. Expected: refusal or a warning for unencodable text.
PDFConcat embeds Noto Sans and checks scripts/glyph coverage before building.

## Resize leaves destination coordinates unchanged

Resize pages containing XYZ/FitH/FitV/FitR destinations. Link rectangles change,
but destination coordinates retain the source coordinate system. Expected:
transform those coordinates or document the limitation. PDFConcat documents it;
it does not implement another fitting engine.

## Merge retains only the first AcroForm default resources

Two static forms use different default fonts: ONE with DA /Helv 12 Tf and
DR Font Helv=Helvetica; TWO with DA /Cour 12 Tf and DR Font Cour=Courier.
Merge succeeds, but later fields retain /Cour while output DR contains only Helv.
Poppler can report Unknown font tag Cour. The same resource name with different
font definitions also needs correct handling. Expected: safely merge/namespace
resources and rewrite references, or reject incompatible forms.
PDFConcat conservatively refuses differing resolved DR and form-level DA/Q
combinations. Its compatible-form tests also show later distinct field names
being namespaced even without collisions; documentation should state that rule.

## Canonical private names are decoded twice

A valid private page dictionary key /Cr#236fpBox canonically means Cr#6fpBox,
not CropBox. Without a real CropBox, upstream lookup/resize can treat it as
CropBox and crop visible text away. Expected: dictionary operations use canonical
bytes and serialization escapes exactly once. PDFConcat refuses ambiguous
literal-hash-plus-hex canonical names; it does not patch the dependency.

## Resize retains UserUnit while writing target coordinate dimensions

Resize a UserUnit 2 page onto A4. Leaving UserUnit 2 makes the physical target
twice A4. Expected: normalize units or account for them in output box dimensions.
Removing local UserUnit after resize matches a physically equivalent source with
scaled coordinates and UserUnit 1, including tested crop/rotation/mixed-unit cases.

## Relaxed parser boundary and real precision

Short product probes accept material object suffixes, decoded duplicate names,
malformed numeric tokens and malformed free-head states; ordinary and packed
objects need consistent, observable policy. Separately, serialization uses twelve
fractional digits and cannot preserve arbitrary finite real bits. Legal comment-
separated indirect-reference spellings are inconsistently supported.
Expected: document/diagnose repair and preserve valid-input identity. Exact probe
commands/results remain in the local implementation evidence. These are disclosed
limitations; final issue splitting and duplicate searches belong to WP10.
