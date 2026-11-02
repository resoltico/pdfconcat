# Security

## Reporting a security issue

Use GitHub's [private vulnerability report form](https://github.com/resoltico/pdfconcat/security/advisories/new)
if private reporting is available for this repository. If the form is unavailable,
open a [contact request](https://github.com/resoltico/pdfconcat/issues/new?template=security_contact.yml)
asking the maintainer to arrange a private channel. That request is public: include
only the request for contact, with no vulnerability details, private paths or PDFs.

Once a private channel is established, include the PDFConcat version, platform,
command and a minimal synthetic reproduction where possible. Keep confidential
PDFs out of public issues and pull requests.

## Installation and release trust

The [installers](docs/INSTALL.md) download a release archive and its
`checksums.txt` from this project's GitHub releases over HTTPS, check the
archive's SHA-256 checksum, and run the downloaded program once as the current
user before installing it. They need no administrator access and write only the
program and, unless disabled, one marked PATH line or user PATH entry. Because
the checksums come from the same release as the archive, a match detects a
damaged download, not a compromised release or repository. Release archives,
`checksums.txt` and both installers carry GitHub build-provenance attestations
from the release workflow; `gh attestation verify` checks them.

macOS executables have an [ad hoc signature](https://developer.apple.com/documentation/security/seccodesignatureflags/adhoc),
which does not identify the publisher. PDFConcat has no Apple Developer ID
signature or notarization, and Windows executables are not code-signed. The
installers do not disable Gatekeeper, SmartScreen or other protections. Manual
browser downloads carry the operating system's internet mark; the installation
guide describes clearing it only after the checksum has been checked. Managed
devices and features such as Windows Smart App Control may still refuse
execution.

## Input and PDF behaviour

PDFConcat is for trusted local files. It is not a sandbox or sanitizer. Malformed
or highly compressed PDFs can exhaust memory/CPU or trigger fatal runtime limits;
ordinary panic recovery cannot cover those failures. Use OS resource limits for
untrusted/large inputs. PDFConcat does not execute or verify network destinations
or active scripts embedded in source PDFs, but retained page/annotation actions
may execute in a PDF viewer. Removing document-level scripts/attachments is not
sanitization.

Encrypted sources, signature values/certification/timestamps, dynamic forms,
optional content and known content-dropping page-tree shapes are refused. Empty
unsigned signature fields remain supported. Signatures are not authenticated.
Upstream relaxed parsing may accept other repairs/malformed syntax; see the
[feature policy and limitations](docs/PDF-POLICY.md).

Page selection is visible-page composition, not redaction. It does not promise
removal of every private object or byte associated with omitted pages. Every
original source remains subject to whole-file safety checks and hashing. Footer
numbers are overlays and may cover existing content.

## Filesystem and saved outputs

Named input plans, fonts and PDFs must be regular files; input symlinks to regular
files are supported. Filesystem checks assume trusted local namespaces and do not
claim protection against concurrent adversarial path substitution.

Output/report targets cannot alias existing inputs or each other, cannot be
symlinks or other nonregular files, and require `--overwrite` for existing regular
targets. Missing inputs are protected only by cleaned path spelling; alternative
spellings of missing paths are not reserved. Reports are rechecked after PDF
publication. With undecoded plans, a report can only create a new path even if
overwrite was requested. A trailing error does not erase the fact that an output
has already been saved; do not blindly retry a result with `output.published: true`.

Files use ordinary umask permissions; replacements retain their old mode. No
private ACL or recovery-copy guarantee is made. Temporary files are siblings of
the target. Without overwrite, native no-replace rename has a hard-link fallback;
with overwrite, publication uses a replacing rename. Some network/share
filesystems do not support these semantics or directory durability. There is no
universal network-filesystem guarantee. A backend/output/report failure never
intentionally rolls back an already-published PDF. See
[saved-output and report behaviour](docs/CLI.md#reports-and-committed-state)
for the result fields that record these outcomes.
