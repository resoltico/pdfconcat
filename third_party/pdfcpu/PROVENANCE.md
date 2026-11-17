# PDFConcat's pdfcpu source patch

This is a modified local source distribution of `github.com/pdfcpu/pdfcpu`
`v0.16.4`, acquired from the Go module proxy using the reviewed module locks:

- Module sum: `h1:XW7q98xu3vNAimTSX93fIWnTDA2F0zz8mu88wiXoMjU=`
- go.mod sum: `h1:zMjQF5nYZ6A0yaG2d4GAj1QrzM5U8BZYDFHpuRwPuxQ=`
- Upstream: https://github.com/pdfcpu/pdfcpu/tree/v0.16.4
- Upstream license: [Apache-2.0](LICENSE.txt); upstream notices and source are retained.
- PDFConcat modifications: AGPL-3.0-only under [the project terms](../../PROJECT-NOTICE.md).

The root module replaces pdfcpu with this directory. It is **modified source**,
not the untouched released module. Build metadata exposes the local replacement;
scanners must retain that identity. `PDFCONCAT-PATCH.json` records the exact changed
source hashes and upstream hashes. This source tree is the canonical patch owner;
there is no second patch file or cache patching step.

The patch adds a typed, per-read `ZeroOffsetPolicy`, disabled by default. The
opt-in policy reserves effective zero-offset classic-xref entries as free
`tombstones`, retaining generation, revision and xref offset. The original typed
trailer and every effective live object's parsed syntax are checked before
`resolveObject` can consult indirect stream lengths, filters, or normalize values.
Dictionary/array references are checked by object number, including other
generations and unreachable active objects. Strings and stream payload are values,
not parsed reference syntax. Existing parser recursion, byte and cancellation
bounds remain in force. Ordinary strict output reads never enable the policy.

Exceptional hybrid or xref-stream revisions are refused. Those readers may parse
objects while constructing the combined xref table; this patch does not claim an
original-reference proof for that path. Ordinary valid hybrid/xref-stream sources
retain upstream processing. This is not a general Quartz compatibility claim.

Run the focused dependency test from this module:

```
go test ./pkg/pdfcpu -run TestZeroOffsetEntryPolicy -count=1
```

The first-party reader and application tests exercise the linked replacement,
including original-reference guards and independently checked serialized output.
The source archive check verifies that vendoring retains these exact modified
files, and an offline vendor build uses the same code without a patch step.

The patch also returns a typed `MissingAnnotationAppearanceError` from the
existing PDF 2.0 appearance checker. It changes no exception or graph rule;
PDFConcat uses the category and subtype facts rather than matching the English
error sentence. Other dictionary, shape and dereference failures remain errors.

The numeric extension emits finite model reals as non-exponent float64 round-trip
decimals, retaining a decimal marker for nonzero integral-valued reals and a
canonical zero. Content/operator emission uses this same owner. Nonfinite output
scalars refuse. Real syntax is dispatched before native integer conversion, so
large explicit reals do not hit an integer range error. Oversized integer tokens
and out-of-range real values refuse visibly; they never become zero or a rounded
real substitute. Ordinary integers and valid indirect-reference syntax retain
their native meaning. This does not recover precision lost during float64 parsing.

Additional dependency-owned qualification (also explicitly discovered by CI):

```
go test ./pkg/pdfcpu/types ./pkg/pdfcpu/model -run 'TestFloatPDFRoundTrip|TestRealLexicalRoundTripAndIntegerRangeRefusal' -count=1
```

The root serializer independently refuses nonfinite scalars before emitting PDF
bytes. Root tests reopen finite scalar witnesses through the actual parser and
verify numeric equality, including subnormals and the largest finite values.

The runtime-only `Configuration.RejectEncryptedInput` option defaults to false.
When enabled, a recognized encryption reference returns the existing typed
`ErrEncrypted` before encryption-dictionary dereference or key setup, with context
cancellation preserved. PDFConcat enables it for source admission/re-admission
and strict output reopening. Header/xref/trailer parsing still occurs first;
existing encryption-reference recognition and earlier malformed-input failures
are unchanged. No cryptographic algorithms or configuration-file schema change.

Dependency-owned controls explicitly exercise default encryption support, refusal
before dictionary I/O/key work, cancellation, cloning and the supported xref forms:

```
go test ./pkg/pdfcpu -run TestEncryptedInputReadPolicy -count=1
```
