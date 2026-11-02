# Frozen fictional producer corpus

These six PDFs complement precise synthetic [pdffixture](../../../pdffixture)
inputs. They are controlled producer exports, not customer documents or a PDF
conformance claim. Ordinary Go tests read frozen bytes and stage isolated copies
without external-network access. A loopback canary tests retained URI behavior.
Producer tools never run in tests or CI.

## Use the frozen corpus

The [corpus tests](../../corpus_test.go) and adjacent `corpus*_test.go` files define
the cases. See [provenance](provenance.json) for frozen asset identities and
producer facts (including authoring tool versions), and [notices](NOTICE.txt) for copyright and license details.
Public provenance uses corpus- or repository-relative paths. Font-specific
notices also apply to font subsets embedded in the PDFs.

For test commands and retained investigation output, see the
[contributor guide](../../../../CONTRIBUTING.md#permanent-corpus).

## Regenerate reviewed assets

Authoring sources and recipes are in [sources](sources/). Choose unused absolute
output paths outside the checkout. Review text, structure, metadata and rendered
pages before replacing frozen bytes or refreshing their recorded facts. Producer
and platform changes can affect output; these recipes do not promise identical
bytes after an upgrade.

Run the following examples from this corpus directory. Replace the absolute
example paths with your fresh output paths. The Python scripts locate the bundled
Noto Sans from the source checkout; no system installation of that font is needed.

### Business report and form base

[generate_business_sources.py](sources/generate_business_sources.py) requires
ReportLab 4.4.9 and creates `business-report.pdf` and `approval-form-base.pdf`:

```sh
python3 sources/generate_business_sources.py --output-dir /absolute/fresh/business
```

[make_classic_incremental.py](sources/make_classic_incremental.py) accepts
`BASE OUTPUT` and adds a supporting-page revision to the form base. It requires
qpdf on `PATH` to inspect the base; the recorded authoring version is 12.4.2:

```sh
python3 sources/make_classic_incremental.py /absolute/fresh/business/approval-form-base.pdf /absolute/fresh/approval-form.pdf
```

The original base remains an exact prefix of the incremental PDF. Review both
revisions and refresh the recorded base-prefix facts when replacing this asset.

### Scanner-like artwork

[create_scan_sources.py](sources/create_scan_sources.py) requires ReportLab 4.4.9
and Pillow 12.3.0. It creates the source images and `scan-invoice.pdf`:

```sh
python3 sources/create_scan_sources.py --output-dir /absolute/fresh/scans
```

### Other producer exports

The [office export recipe](sources/export_office_report.txt) records the font
preparation, LibreOffice version and export arguments. The
[object-stream encoding recipe](sources/qpdf_report_encoding.txt) records the
qpdf arguments and independent equivalence checks.

Use the Typst version declared in `provenance.json` to compile:

```sh
typst compile --ignore-system-fonts --creation-timestamp 0 sources/technical-note.typ /absolute/fresh/technical-note.pdf
```

### Record a reviewed change

When changing a frozen asset, update its hash, byte count and independently
reviewed facts in `provenance.json`, retaining the required font notices. When
only an authoring script or recipe changes, update its `AuthoredSourcesSHA256`
entry; the reviewed frozen PDFs need not be regenerated for a path correction.
