# Install, update and uninstall the CLI

This guide describes the installer in this checkout. The published v0.9.0
installer predates ownership receipts. The latest-download commands below serve
the currently published installer; the new ownership rules take effect with the
release containing this change. Use documentation at the matching release tag.

The installer downloads PDFConcat for the current user, verifies the archive's
SHA-256 checksum and test-runs the program before replacing an owned installation.
No administrator account, background updater, scheduled task or file association
is needed. The CLI's inputs and outputs are outside the installer's scope.

## Install

On macOS or Linux:

```sh
curl -fsSL https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh | sh
```

If curl is unavailable, use
`wget -qO- https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh | sh`.
The installer itself requires standard shell tools, tar and a SHA-256 tool
(`sha256sum`, `shasum` or `openssl`).

On Windows, use PowerShell:

```powershell
powershell -ExecutionPolicy ByPass -c "irm https://github.com/resoltico/pdfconcat/releases/latest/download/install.ps1 | iex"
```

Windows PowerShell 5.1 and PowerShell 7 are supported. `ByPass` applies only to the
launched PowerShell session; it does not change persistent policy or override
Group Policy. Managed systems can still refuse the program.

Supported systems are macOS 15 or later on Apple Silicon and Intel, Linux on
x86-64 and ARM64, and Windows 10/11 on x64 and ARM64. Linux builds use
`CGO_ENABLED=0` and do not need a system C library; execution can still be limited
by the kernel or device policy. Native CPU selection also handles translated shells.

The default location is `$XDG_BIN_HOME/pdfconcat` when `XDG_BIN_HOME` is set,
otherwise `~/.local/bin/pdfconcat`. On Windows it is
`%LOCALAPPDATA%\Programs\PDFConcat\pdfconcat.exe`.
Choose another absolute folder with `--install-dir DIR` or `-InstallDir DIR`.

Open a new terminal after PATH changes, then verify:

```sh
pdfconcat --version
pdfconcat --help
```

For a quick functional check, run `pdfconcat build -o blank.pdf --blank --fit-to A4`.

The documented Windows command runs in a child process, so its PATH changes do
not reach an already-open parent terminal. Running the script directly in the
current PowerShell session may also update that session's PATH. On macOS/Linux,
follow the quoted command printed by the installer to update the current shell,
or open a new terminal. `--no-modify-path` / `-NoModifyPath` leaves persistent
and current-session PATH unchanged.

## Update and choose a release

Run the same install command again. The installer resolves latest once to a stable
`vX.Y.Z` tag, then downloads both archive and `checksums.txt` from that release.
A failed download, checksum or candidate test happens before replacing the active
program. An update requires the existing binary, legal files and ownership state
to match their receipt.

To select a stable release explicitly:

```sh
curl -fsSL https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh | sh -s -- --version X.Y.Z
```

```powershell
$env:PDFCONCAT_VERSION = 'X.Y.Z'
irm https://github.com/resoltico/pdfconcat/releases/latest/download/install.ps1 | iex
```

The environment pin remains set in that session. Clear it to return to latest:

```sh
unset PDFCONCAT_VERSION
```

```powershell
Remove-Item Env:PDFCONCAT_VERSION -ErrorAction SilentlyContinue
```

Keep a copy of the reviewed installer you used, from the selected release tag or
reviewed source checkout. The pipe commands do not save one. Do not assume that
fetching the current latest script later gives the same ownership protocol.

The installer stages files privately on the destination filesystem and uses a
per-product, per-directory exclusive lock. A held lock fails promptly; there are
no automatic lock retries. It attempts to restore the prior owned state if
activation fails. Errors describe any remaining state; an interruption or failed
PATH write is not a promise that nothing was installed. Inspect the reported
paths before trying again.

On Windows an update can retain one uniquely named, receipt-owned running
executable. Close processes using it before another update or uninstall if
cleanup is refused. The installer never adopts or deletes arbitrary `.old` files.

## Ownership and older installations

The current installer uses `.pdfconcat-install` beside the executable
for ownership receipts and managed data. It verifies managed file hashes before
update or removal and refuses existing unowned or changed files. It does not
adopt older installs from their filename, version output or old PATH marker.

For a previous installer or manual/source installation without a valid receipt,
remove the old executable yourself or select a fresh directory. Keep any files
you need and remove obsolete PATH settings by hand after reviewing them. Do not
copy a receipt from another directory or edit hashes to bypass a refusal.
A non-directory target or non-regular managed entry is also left alone.

Pre-existing directories may be shared with other tools. They are never removed
merely because they became empty. Unrelated files are preserved.

## Uninstall

Use the same reviewed installer, saved as `install.sh` / `install.ps1`, to remove
an unchanged installation and only the PATH entries it owns. In particular, do not
fetch the currently published older installer to remove this checkout's new
receipt-based installation:

```sh
sh ./install.sh --uninstall
```

```powershell
powershell -ExecutionPolicy ByPass -File .\install.ps1 -Uninstall
```

Pass the same custom install directory used for installation, for example
`sh ./install.sh --uninstall --install-dir '/absolute/my tools'` or
`powershell -ExecutionPolicy ByPass -File .\install.ps1 -Uninstall -InstallDir 'C:\My Tools'`.
Removal does not
need a download of the program. An offline saved copy of the installer can be
run with the same uninstall option.

Changed or unowned managed files are refused rather than silently deleted. For
manual cleanup, inspect the executable and its private `.pdfconcat-install`
directory, then remove only the files you intend to remove. An installer-created
directory is removed only if empty and never if it is a filesystem root.

Unix PATH cleanup uses the exact recorded startup path and generated line or Fish
entry for this install directory. It preserves other installations and user edits;
symlinked startup files remain symlinks. Windows removes only its exact added user
PATH entry, preserving raw text, empty segments, environment references and value
kind. A PATH entry can be conservatively retained with a warning if unrelated
files remain in the shared install directory; removing it could break other tools.
`NoModifyPath` also suppresses PATH cleanup during
uninstall and reports the entry left for manual removal.

Your PDFs, plans and reports are not installer-managed files.

## Options and mirrors

| macOS/Linux | Windows | Environment | Effect |
|---|---|---|---|
| `--version X.Y.Z` | `-Version X.Y.Z` | `PDFCONCAT_VERSION` | Choose a stable release; one leading `v` is allowed. |
| `--install-dir DIR` | `-InstallDir DIR` | `PDFCONCAT_INSTALL_DIR` | Choose an absolute directory. |
| `--no-modify-path` | `-NoModifyPath` | `PDFCONCAT_NO_MODIFY_PATH=1` | Do not change PATH or shell startup files. |
| `--uninstall` | `-Uninstall` | `PDFCONCAT_UNINSTALL=1` (Windows) | Remove unchanged owned files. |
| `--help` | `-Help` | | Show usage. |

Pass Unix options after `sh -s --`. In PowerShell use environment variables before
`irm ... | iex`, or parameters on the saved, reviewed script shown above.

`PDFCONCAT_DOWNLOAD_BASE` selects an HTTPS release mirror or an absolute local
directory containing the release's archive and `checksums.txt`. HTTPS is required
at every redirect hop. A mirror is explicitly trusted as the release source;
its checksums do not authenticate its publisher. Mirror URL bases must be directory
URLs without credentials, queries or fragments; use an absolute local release
directory if authenticated downloads are handled separately. A local snapshot archive can be tested without a stable release pin.

A PATH entry cannot represent a Unix colon or a Windows semicolon in a directory
name. The installer reports unsupported persistence rather than claiming PATH
was configured. Use the printed, correctly quoted full executable path or choose
another directory. Startup/registry write failures are reported.

## What the installer changes

- The executable at the selected install directory.
- The private `.pdfconcat-install` ownership directory. Its receipt is data, not
  an executable shell configuration file.
- PDFConcat embeds its license notices (`pdfconcat licenses`) and schemas
  (`pdfconcat schema plan` / `pdfconcat schema result`); no external legal files
  are needed by the installed program.
- Only the exact startup entry or user PATH entry added by this installation.

The Unix lock is the sibling `.pdfconcat-install.lock`; a normal run removes its
own lock. Locks serialize cooperating installer operations, not hostile processes.
Do not remove a lock while another installer is running. The CLI does not require
installer receipts to run.

## Trust and verification

Both installer and program are code from this repository's release. SHA-256
verification detects damaged or incomplete downloads, not a compromised repository
or mirror. A candidate is executed as the current user only after checksum
verification. A receipt prevents accidental replacement/removal of other files;
it is not protection from a hostile process with your account's permissions.

Published release provenance can be checked with `gh attestation verify <asset>
--repo resoltico/pdfconcat`. That is a separate optional
verification step; the installer does not claim to authenticate attestations.

The scripts do not disable Gatekeeper, SmartScreen or managed-device policy.
Mac executables are ad hoc signed, without Developer ID or notarization; Windows
executables are unsigned. Browser downloads may carry internet markings and
system policy may block execution even after a matching checksum. Inspect the
candidate diagnostic shown by the installer before its temporary files are cleaned.

## Manual installation

For manual downloads, open [Releases](https://github.com/resoltico/pdfconcat/releases),
choose the native CPU archive on the matching release tag,
verify its entry in `checksums.txt`, then extract into a fresh directory. Keep the
archive's legal material with any copy you redistribute.

## Troubleshooting

If an ownership check fails, inspect the selected path and private directory.
Choose a fresh directory or remove the prior files manually; a version string
is not proof that this installer owns a file. If a lock remains after a crash,
first establish that no installer is using it before manual cleanup.

If the verified candidate cannot run, use its reported diagnostic to check the
CPU, minimum OS/runtime and device policy. Do not bypass checksum verification.
If PATH was not changed, use the quoted full path printed by the installer or
open a new terminal after successful persistence. For shell shadowing use
`command -v pdfconcat`; in PowerShell use `Get-Command pdfconcat` and remove a
conflicting installation using the method that installed it.

## Archive names

The current archive convention is unversioned; choose the release tag first:

| System | Archive |
|---|---|
| macOS Apple Silicon | `pdfconcat_darwin_arm64.tar.gz` |
| macOS Intel | `pdfconcat_darwin_amd64.tar.gz` |
| Linux ARM64 | `pdfconcat_linux_arm64.tar.gz` |
| Linux x86-64 | `pdfconcat_linux_amd64.tar.gz` |
| Windows ARM64 | `pdfconcat_windows_arm64.zip` |
| Windows x64 | `pdfconcat_windows_amd64.zip` |

The native installers select the hardware CPU, including when the current shell
is translated. On a Mac, About This Mac shows a Chip entry for Apple Silicon and
an Intel Processor entry for Intel. Manual downloads must match that hardware.
