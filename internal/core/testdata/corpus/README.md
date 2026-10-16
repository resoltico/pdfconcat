# Frozen fictional producer corpus

These six PDFs complement precise synthetic [pdffixture](../../../pdffixture)
inputs. They are controlled producer exports, not customer documents or a PDF
conformance claim. Ordinary offline Go tests read frozen bytes and stage only isolated copies;
producer tools never run in tests or CI. The [corpus tests](../../corpus_test.go)
and adjacent `corpus*_test.go` files define the cases. See [provenance](provenance.json)
for frozen asset identities and producer facts, and [notices](NOTICE.txt) for
copyright and license details.

Authoring sources and recipes are in [sources](sources/). Run authoring into fresh
scratch paths, review text/structure/metadata/renders and update independently
reviewed facts before replacing frozen bytes. For the form,
[generate_business_sources.py](sources/generate_business_sources.py) creates a base
in scratch. [make_classic_incremental.py](sources/make_classic_incremental.py)
accepts `BASE OUTPUT` and appends one supporting-page revision. The original base
is retained as an exact prefix of the incremental PDF; both revisions require
review. From this corpus directory, the Typst command is:

```sh
typst compile --ignore-system-fonts --creation-timestamp 0 sources/technical-note.typ FRESH_OUTPUT.pdf
```

The ReportLab generator uses `--output-dir FRESH_DIRECTORY`; scanner-like artwork
uses the same option on [create_scan_sources.py](sources/create_scan_sources.py).
They use ReportLab 4.4.9 and Pillow 12.3.0. The [office export](sources/export_office_report.txt)
and [object-stream encoding](sources/qpdf_report_encoding.txt) recipes record
precise arguments.
Public provenance uses corpus- or repository-relative paths.
Font-specific notices also apply to font subsets embedded in these PDFs.
