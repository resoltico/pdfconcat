# Install, update and uninstall

PDFConcat is one self-contained program. The installer downloads it from this
project's GitHub releases, checks it and puts it in a folder in your user
account. It needs no administrator password, no Go, no Homebrew and no other
software.

- [macOS and Linux](#macos-and-linux)
- [Windows](#windows)
- [Update](#update)
- [Uninstall](#uninstall)
- [Options](#options)
- [What the installer changes](#what-the-installer-changes)
- [Trust and verification](#trust-and-verification)
- [Manual installation](#manual-installation)
- [Troubleshooting](#troubleshooting)

## macOS and Linux

Open Terminal and run:

```sh
curl -fsSL https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh | sh
```

If `curl` is missing, use `wget -qO- https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh | sh`.

It takes a few seconds and shows each step:

```text
PDFConcat installer

  ✓ System      macOS 15.5 · Apple silicon (arm64)
  ✓ Downloaded  pdfconcat_darwin_arm64.tar.gz · 10.6 MB
                from the latest release
  ✓ Verified    SHA-256 matches the release checksums
  ✓ Tested      pdfconcat 0.7.0 runs on this system
  ✓ Installed   ~/.local/bin/pdfconcat
  ✓ PATH        added ~/.local/bin via ~/.zshrc

  PDFConcat 0.7.0 is installed.

  Open a new terminal, or run this to use it now:
      export PATH="$HOME/.local/bin:$PATH"
```

Then check it:

```sh
pdfconcat version
```

Supported systems: macOS 15 or later on Apple silicon or Intel, and Linux on
x86-64 or arm64. The Linux program is statically linked and works on any
distribution.

## Windows

Open PowerShell (press Start, type **PowerShell**, press Enter) and run:

```powershell
powershell -ExecutionPolicy ByPass -c "irm https://github.com/resoltico/pdfconcat/releases/latest/download/install.ps1 | iex"
```

`-ExecutionPolicy ByPass` applies only to this one command; your PowerShell
settings are not changed. The installer shows the same steps as on macOS and
Linux. Open a new terminal window afterwards, then check it:

```powershell
pdfconcat version
```

Supported systems: Windows 10 and 11 on x64 or Arm64.

## Update

Run the installation command again. It downloads the latest release, checks it
and only then replaces the installed program, so a failed download leaves your
working copy in place. It reports the change, for example
`updated from 0.6.2`. A `pdfconcat` that is already running finishes with the
old version.

To install a specific release instead of the latest, pass its version:

```sh
curl -fsSL https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh | sh -s -- --version 0.7.0
```

```powershell
$env:PDFCONCAT_VERSION = '0.7.0'; irm https://github.com/resoltico/pdfconcat/releases/latest/download/install.ps1 | iex
```

## Uninstall

Uninstalling removes the program and the PATH setting the installer added.
Your PDFs, plans and reports are not touched.

On macOS and Linux:

```sh
curl -fsSL https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh | sh -s -- --uninstall
```

On Windows:

```powershell
powershell -ExecutionPolicy ByPass -c "& ([scriptblock]::Create((irm https://github.com/resoltico/pdfconcat/releases/latest/download/install.ps1))) -Uninstall"
```

If you installed into a custom folder, add `--install-dir DIR` (macOS and
Linux) or `-InstallDir DIR` (Windows) to the uninstall command too.

Without an internet connection, uninstall by hand:

- **macOS and Linux:** delete `~/.local/bin/pdfconcat`. Then delete each line
  ending in `# added by the PDFConcat installer` from your shell startup files
  (`~/.zshrc`, `~/.bashrc`, `~/.bash_profile` or `~/.profile`), and delete
  `~/.config/fish/conf.d/pdfconcat.fish` if it exists.
- **Windows:** delete the folder `%LOCALAPPDATA%\Programs\PDFConcat`. Then press
  Start, type **environment**, open **Edit environment variables for your
  account**, and remove that folder from **Path**.

## Options

| macOS and Linux | Windows | Environment variable | Effect |
| --- | --- | --- | --- |
| `--version X.Y.Z` | `-Version X.Y.Z` | `PDFCONCAT_VERSION` | Install that release instead of the latest. |
| `--install-dir DIR` | `-InstallDir DIR` | `PDFCONCAT_INSTALL_DIR` | Install into `DIR`. |
| `--no-modify-path` | `-NoModifyPath` | `PDFCONCAT_NO_MODIFY_PATH=1` | Leave PATH and shell startup files alone. |
| `--uninstall` | `-Uninstall` | `PDFCONCAT_UNINSTALL=1` (Windows only) | Remove PDFConcat. |
| `--help` | `-Help` | | Show the options. |

Pass macOS and Linux options after `sh -s --`, as in the examples above. On
Windows, set the environment variable before `irm ... | iex`, or pass options
through the script block form shown under [Uninstall](#uninstall).

`PDFCONCAT_DOWNLOAD_BASE` downloads release files from another location instead
of GitHub: an `https://` mirror or a local folder holding the release's archives
and `checksums.txt`. The checksum check still applies.

## What the installer changes

| | macOS and Linux | Windows |
| --- | --- | --- |
| Program | `~/.local/bin/pdfconcat`, or `$XDG_BIN_HOME/pdfconcat` when `XDG_BIN_HOME` is set | `%LOCALAPPDATA%\Programs\PDFConcat\pdfconcat.exe` |
| PATH | If the folder is not already on PATH, one line in the startup file of your login shell: `~/.zshrc` for zsh; `~/.bashrc` and your login file for bash; `~/.profile` otherwise. Fish gets `~/.config/fish/conf.d/pdfconcat.fish`. | If the folder is not already on PATH, it is added to the start of your user PATH. |

The added line only puts the folder on PATH when it is missing, and it ends with
`# added by the PDFConcat installer` so that uninstall can find it. Running the
installer again never adds a second copy. Startup files that are symbolic links,
as in many dotfiles setups, stay links.

Nothing else is changed: no administrator settings, background updater,
scheduled task or file association. Downloads go to a temporary folder that is
deleted when the installer finishes. The documentation, licenses, example plans
and schemas are in each [release archive](https://github.com/resoltico/pdfconcat/releases)
and in this repository; `pdfconcat schema plan` and `pdfconcat schema result`
print the schemas of the installed program.

## Trust and verification

The installer and the program both come from this project's GitHub releases,
over HTTPS. Before installing, the installer checks the download against the
SHA-256 checksums published with the same release, then runs the downloaded
program once to confirm that it works on your system and reports the expected
version. A checksum mismatch stops the installation. Because the checksums come
from the same place as the download, this catches damaged or incomplete
downloads, not a compromised release.

To check where a release came from, use GitHub's
[artifact attestations](https://docs.github.com/actions/security-for-github-actions/using-artifact-attestations):
every archive, `checksums.txt` and both installers carry build provenance from
this repository's release workflow. With the [GitHub CLI](https://cli.github.com/):

```sh
gh attestation verify pdfconcat_linux_amd64.tar.gz --repo resoltico/pdfconcat
```

To read the installer before running it, download it, inspect it, then run the
saved copy:

```sh
curl -fsSLO https://github.com/resoltico/pdfconcat/releases/latest/download/install.sh
less install.sh
sh install.sh
```

**Code signing.** The macOS program has an ad hoc signature, which Apple silicon
requires, but it is not signed with an Apple Developer ID or notarized by Apple.
The Windows program is not code-signed. Programs downloaded by `curl`, `wget` or
PowerShell are not marked as coming from the internet, so Gatekeeper and
SmartScreen do not interrupt the installer. Browser downloads are marked; see
[Manual installation](#manual-installation). Windows 11 Smart App Control, when
turned on, may block unsigned programs such as PDFConcat.

See also [Security](../SECURITY.md#installation-and-release-trust).

## Manual installation

Every release has archives with stable names, so these links always point to the
latest release:

| System | Archive |
| --- | --- |
| macOS, Apple silicon | [pdfconcat_darwin_arm64.tar.gz](https://github.com/resoltico/pdfconcat/releases/latest/download/pdfconcat_darwin_arm64.tar.gz) |
| macOS, Intel | [pdfconcat_darwin_amd64.tar.gz](https://github.com/resoltico/pdfconcat/releases/latest/download/pdfconcat_darwin_amd64.tar.gz) |
| Linux, x86-64 | [pdfconcat_linux_amd64.tar.gz](https://github.com/resoltico/pdfconcat/releases/latest/download/pdfconcat_linux_amd64.tar.gz) |
| Linux, arm64 | [pdfconcat_linux_arm64.tar.gz](https://github.com/resoltico/pdfconcat/releases/latest/download/pdfconcat_linux_arm64.tar.gz) |
| Windows, x64 | [pdfconcat_windows_amd64.zip](https://github.com/resoltico/pdfconcat/releases/latest/download/pdfconcat_windows_amd64.zip) |
| Windows, Arm64 | [pdfconcat_windows_arm64.zip](https://github.com/resoltico/pdfconcat/releases/latest/download/pdfconcat_windows_arm64.zip) |

Download the archive and [checksums.txt](https://github.com/resoltico/pdfconcat/releases/latest/download/checksums.txt)
into the same folder, then check the archive from that folder:

```sh
# macOS
shasum -a 256 --check --ignore-missing checksums.txt
# Linux
sha256sum --check --ignore-missing checksums.txt
```

```powershell
# Windows: compare this with the archive's line in checksums.txt
(Get-FileHash .\pdfconcat_windows_amd64.zip -Algorithm SHA256).Hash.ToLower()
```

Extract the archive. The program, `pdfconcat` or `pdfconcat.exe`, is at its top
level beside the documentation. Move it into a folder on your PATH, or run it by
its full path.

A browser marks downloaded files as coming from the internet:

- **macOS** refuses to open a browser-downloaded PDFConcat because it is not
  notarized. After checking the checksum, clear the mark from the extracted
  program with `xattr -d com.apple.quarantine pdfconcat`. Alternatively, run it
  once, then choose **Open Anyway** in **System Settings → Privacy & Security**.
- **Windows** may show a SmartScreen warning. After checking the checksum, run
  `Unblock-File .\pdfconcat.exe` in PowerShell.

The installer avoids both steps, so prefer it when you can.

## Troubleshooting

### `pdfconcat` is not found after installing

Open a new terminal window: PATH changes apply to terminals started after the
installation. On macOS and Linux you can instead run the `export PATH=...` line
the installer printed. If a new window still cannot find it, your shell may read
a startup file the installer did not edit. Add
`export PATH="$HOME/.local/bin:$PATH"` to that file, or run the program by its
full path, `~/.local/bin/pdfconcat`.

### A different `pdfconcat` runs

Another copy earlier on your PATH takes precedence; the installer warns when it
sees one. Find it with `command -v pdfconcat` (macOS and Linux) or
`Get-Command pdfconcat` (PowerShell), then remove that copy using the method
that installed it.

PDFConcat 0.6.1 and 0.6.2 used a different macOS setup that installed into
`/usr/local`. Remove it with its own helper:

```sh
/bin/bash /usr/local/libexec/pdfconcat/current/setup.sh uninstall
```

### The download fails

Check your internet connection and that
[github.com/resoltico/pdfconcat/releases](https://github.com/resoltico/pdfconcat/releases)
opens. `curl` and `wget` use the `HTTPS_PROXY` environment variable; PowerShell
uses the Windows proxy settings. On networks that block GitHub, place the
release files on an internal server or folder and point
`PDFCONCAT_DOWNLOAD_BASE` at it.

### The checksum does not match

Run the installer again. If it still fails, do not work around the check;
[open an issue](https://github.com/resoltico/pdfconcat/issues) with the version
and the installer's output.

### The downloaded program does not run on this system

The installer tests the program before installing it and reports this when it
fails. Run the command it shows to see the reason. Common causes are a macOS
version older than 15, or a managed computer whose policy blocks programs that
are not notarized or signed. Ask the computer's administrator about such a
policy rather than disabling system protections.

### PowerShell says running scripts is disabled

Use the complete command shown under [Windows](#windows), including
`powershell -ExecutionPolicy ByPass -c`. Pasting only `irm ... | iex` into a
PowerShell window with a restrictive policy can fail.
