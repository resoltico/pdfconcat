# Install, update and uninstall the CLI

Install a standalone PDFConcat release for the current user, or
[build from source](../README.md#build-from-source). No administrator account,
background updater, scheduled task or file association is needed. Your PDFs,
plans and reports are outside the installer's scope.

The commands below download published release installers. The latest published
release is currently **0.10.0**. This checkout adds recovery behavior described
under [ownership and recovery](#ownership-and-recovery); those additions are not
present in the published 0.10.0 installers. Do not assume a saved older script
supports checkout-only options. Use an installer and payload from the same
qualified build, with its matching documentation.

## Download, inspect and install

On macOS or Linux, save the latest installer, inspect it, then run it:

```sh
curl --proto '=https' --proto-redir '=https' -fL -o ./install-pdfconcat.sh https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh &&
  less ./install-pdfconcat.sh &&
  sh ./install-pdfconcat.sh
```

If curl is unavailable, use `wget --https-only -O ./install-pdfconcat.sh URL`
with the same URL and `&&` chain. The installer itself can use curl or wget and
requires standard shell tools, tar and a SHA-256 tool (`sha256sum`, `shasum` or
`openssl`). `less` is only for your review; the [README quick start](../README.md#install)
omits it.

On Windows, use Windows PowerShell 5.1 or PowerShell 7:

```powershell
& {
    $ErrorActionPreference = 'Stop'
    Invoke-WebRequest -UseBasicParsing -Uri https://github.com/resoltico/pdfconcat/releases/latest/download/install.ps1 -OutFile .\install-pdfconcat.ps1 -ErrorAction Stop
    Get-Content .\install-pdfconcat.ps1
    powershell -NoProfile -ExecutionPolicy ByPass -File .\install-pdfconcat.ps1
    if ($LASTEXITCODE -ne 0) { throw "Installer failed ($LASTEXITCODE)." }
}
```

To pause for review, stop after the download, inspect the saved file, and run the
child command only after a successful download. `ByPass` applies only to that
child session; it does not change persistent policy or override Group Policy.
Use `pwsh` instead of `powershell` to select PowerShell 7. Managed systems can
still refuse the program.

The Unix `&&` chain and PowerShell script block prevent a failed fetch from
executing a stale saved installer. An installer failure remains a failure of the
whole command: Unix returns its nonzero status; PowerShell throws when the child
reports a nonzero exit. Keep these guards in automation. A bare `curl | sh`
pipeline can hide a failed download behind a successful empty shell.

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

The Windows command above runs in a child process, so its PATH changes do
not reach an already-open parent terminal. Running the script directly in the
current PowerShell session may also update that session's PATH. On macOS/Linux,
use the quoted full executable path printed by the installer while this terminal
keeps its current PATH, or open a new terminal after successful persistence.
`--no-modify-path` / `-NoModifyPath` leaves persistent and current-session PATH
unchanged.

## Update and choose a release

Repeat the latest download-and-install command to update an unchanged managed
copy. Keep the saved installer for offline uninstall. To pin both installer and
payload to the published 0.10.0 release:

```sh
curl --proto '=https' --proto-redir '=https' -fL -o ./install-pdfconcat.sh https://github.com/resoltico/pdfconcat/releases/download/v0.10.0/install.sh &&
  sh ./install-pdfconcat.sh --version 0.10.0
```

```powershell
& {
    $ErrorActionPreference = 'Stop'
    Invoke-WebRequest -UseBasicParsing -Uri https://github.com/resoltico/pdfconcat/releases/download/v0.10.0/install.ps1 -OutFile .\install-pdfconcat.ps1 -ErrorAction Stop
    powershell -NoProfile -ExecutionPolicy ByPass -File .\install-pdfconcat.ps1 -Version 0.10.0
    if ($LASTEXITCODE -ne 0) { throw "Installer failed ($LASTEXITCODE)." }
}
```

Replace both `0.10.0` values together to choose another published stable release.
Re-running a pinned command reinstalls that release; it does not select future
latest releases.

When no version is supplied, the installer resolves latest once to a stable
`vX.Y.Z` tag, then downloads both archive and `checksums.txt` from that release.
A failed download, checksum or candidate test happens before replacing the active
program. An update requires intact ownership state and every remaining managed
file to match its receipt. The checkout installer also repairs a missing owned executable;
the published 0.10.0 Unix installer refuses that state.

Alternatively, `PDFCONCAT_VERSION` supplies the release when no version argument
is given. Clear an environment pin to return to latest:

```sh
unset PDFCONCAT_VERSION
```

```powershell
Remove-Item Env:PDFCONCAT_VERSION -ErrorAction SilentlyContinue
```

Keep the reviewed installer with the matching release documentation for later
update or removal.

The installer stages files privately on the destination filesystem and uses a
per-product, per-directory exclusive lock. A held lock fails promptly; there are
no automatic lock retries. It attempts to restore the prior owned state if
activation fails. Errors describe any remaining state; an interruption or failed
PATH write is not a promise that nothing was installed. Inspect the reported
paths before trying again.

In the checkout Unix installer, new PATH writes are part of activation: only
completed additions enter the active receipt. A failed transaction attempts to
restore the prior executable and receipt, including an executable's prior
absence. Rollback removes only this transaction's verified additions, preserving
later user content. If a partial write or concurrent edit prevents exact cleanup,
the diagnostic names retained staging evidence; keep it and inspect the reported
startup file before retrying. Later deliberate edits or removals of a successfully
recorded entry are preserved instead of silently re-added.

On Windows an update can retain one uniquely named, receipt-owned running
executable. Close processes using it before another update or uninstall if
cleanup is refused. The installer never adopts or deletes arbitrary `.old` files.

## Custom directory and PATH

Use the saved, reviewed installer with an absolute directory, for example:

```sh
sh ./install-pdfconcat.sh --install-dir "$HOME/my tools" --no-modify-path
```

```powershell
powershell -NoProfile -ExecutionPolicy ByPass -File .\install-pdfconcat.ps1 -InstallDir "$env:LOCALAPPDATA\My Tools" -NoModifyPath
if ($LASTEXITCODE -ne 0) { throw "Installer failed ($LASTEXITCODE)." }
```

Omit the no-PATH option to enable normal PATH setup. With it, use the full
executable path printed by the installer; neither persistent nor current-session
PATH changes. Reuse the same custom directory for update and uninstall.

## Ownership and recovery

The current installer uses `.pdfconcat-install` beside the executable
for ownership receipts and managed data. It verifies managed file hashes before
update or removal and refuses existing unowned or changed files. It does not
adopt unowned installs from their filename, version output or PATH settings.

The published 0.10.0 installer refuses a receiptless executable, including one
installed by 0.9.0. It cannot prove ownership from a version string. Use a fresh
directory or review and preserve the old executable before removing it manually;
review old PATH settings separately. Never delete an entire shared bin directory,
copy a receipt from another directory or edit hashes to bypass a refusal.

**Checkout installer additions (not in published 0.10.0):**

- A receiptless regular executable with no private installer state is still
  refused by default. Explicit `--replace-unmanaged` / `-ReplaceUnmanaged` on
  install authorizes replacing only that executable. The installer reports its
  scope, verifies the new candidate and rechecks the target under its lock.
- The original executable is preserved in a unique recovery location in the
  install directory's parent, outside the bin directory being managed. Its exact
  path is printed. Keep it until you have verified the replacement. Normal
  uninstall does not delete it or treat it as another active tool that requires
  retaining a PATH entry. Pre-existing PATH entries and directories are not adopted.
- A valid receipt with only the managed executable missing can be repaired by
  re-running install, or cleaned up with uninstall. Remaining owned files must
  still verify. Repeating uninstall after complete cleanup is harmless.
- Changed managed files, corrupt or incomplete state, inaccessible files, and
  symlinks or other nonregular executable/state targets remain conservative refusals; the
  replacement option cannot bypass them. Inspect the exact path and cause in
  the diagnostic, preserve valuable files and use a fresh directory if needed.
- Unix directory aliases are accepted only when requested and recorded paths
  still resolve to the same physical directory. Windows accepts normalized
  spellings but refuses reparse-point directories. Moved receipts are not
  adopted; use the recorded path shown in a location-mismatch diagnostic.

After obtaining and reviewing the installer from a qualified build containing
these changes, explicit replacement uses the same selected directory:

```sh
sh ./install-pdfconcat.sh --install-dir "$HOME/.local/bin" --replace-unmanaged
```

```powershell
powershell -NoProfile -ExecutionPolicy ByPass -File .\install-pdfconcat.ps1 -InstallDir "$env:LOCALAPPDATA\Programs\PDFConcat" -ReplaceUnmanaged
if ($LASTEXITCODE -ne 0) { throw "Installer failed ($LASTEXITCODE)." }
```

The flag is install-only, takes no stdin consent and does not authorize an
unmanaged uninstall. It does not execute the old executable to identify it.
If activation fails, the installer attempts to restore the original executable
and prior state, including prior absence. If restoration fails, keep all reported
recovery paths and inspect them before retrying; do not discard evidence based
on a successful earlier download. An operation reporting incomplete restoration
has not restored the prior installation.

Pre-existing directories may be shared with other tools. They are never removed
merely because they became empty. Unrelated files are preserved.

## Uninstall

Use the same reviewed installer, saved as `install-pdfconcat.sh` or
`install-pdfconcat.ps1`, to remove an unchanged installation and only the PATH
entries it owns:

```sh
sh ./install-pdfconcat.sh --uninstall
```

```powershell
powershell -NoProfile -ExecutionPolicy ByPass -File .\install-pdfconcat.ps1 -Uninstall
if ($LASTEXITCODE -ne 0) { throw "Uninstall failed ($LASTEXITCODE)." }
```

Pass the same custom install directory used for installation, for example
`sh ./install-pdfconcat.sh --uninstall --install-dir '/absolute/my tools'` or
`powershell -NoProfile -ExecutionPolicy ByPass -File .\install-pdfconcat.ps1 -Uninstall -InstallDir 'C:\My Tools'`.
Removal does not need a download of the program. An offline saved copy of the installer can be
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

Windows uninstall removes an owned PATH entry before deleting
the executable or receipt. A failed PATH write leaves those files intact for
retry. A later file-removal failure can leave partial progress; inspect the
reported paths before retrying or using the manual cleanup above.

Your PDFs, plans and reports are not installer-managed files.

## Options and mirrors

| macOS/Linux | Windows | Environment | Effect |
|---|---|---|---|
| `--version X.Y.Z` | `-Version X.Y.Z` | `PDFCONCAT_VERSION` | Choose a stable release; one leading `v` is allowed. |
| `--install-dir DIR` | `-InstallDir DIR` | `PDFCONCAT_INSTALL_DIR` | Choose an absolute directory. |
| `--no-modify-path` | `-NoModifyPath` | `PDFCONCAT_NO_MODIFY_PATH=1` | Do not change PATH or shell startup files. |
| `--replace-unmanaged` | `-ReplaceUnmanaged` | | Checkout only: explicitly back up and replace a receiptless regular executable; install only. |
| `--uninstall` | `-Uninstall` | `PDFCONCAT_UNINSTALL=1` (Windows) | Remove unchanged owned files. |
| `--help` | `-Help` | | Show usage. |

Pass options to the saved, reviewed script as shown above. The Windows environment
variables also work when the script runs in the current PowerShell session.

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
- An explicit receiptless replacement also leaves a recovery backup outside the
  install directory; its printed path is preserved by later uninstall.
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
Use the state-specific diagnosis under [ownership and recovery](#ownership-and-recovery);
a version string is not proof that this installer owns a file. If a lock remains after a crash,
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
