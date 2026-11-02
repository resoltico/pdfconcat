# SPDX-License-Identifier: MPL-2.0
#
# Installs, updates or uninstalls PDFConcat for the current Windows user.
# No administrator access is needed. See docs/INSTALL.md.
#
#   powershell -ExecutionPolicy ByPass -c "irm https://github.com/resoltico/pdfconcat/releases/latest/download/install.ps1 | iex"
#
# Options can be passed through a script block, for example:
#   & ([scriptblock]::Create((irm https://github.com/resoltico/pdfconcat/releases/latest/download/install.ps1))) -Uninstall
#
# Keep this file ASCII: Windows PowerShell 5.1 misreads other characters.

param(
    [string]$Version = $env:PDFCONCAT_VERSION,
    [string]$InstallDir = $env:PDFCONCAT_INSTALL_DIR,
    [switch]$NoModifyPath,
    [switch]$Uninstall,
    [switch]$Help
)

function Invoke-PDFConcatInstaller {
    param([string]$Version, [string]$InstallDir, [bool]$ModifyPath, [bool]$Uninstall)

    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    Set-StrictMode -Version 3

    $repoUrl = 'https://github.com/resoltico/pdfconcat'
    $docsUrl = "$repoUrl/blob/main/docs/INSTALL.md"
    $useColor = -not $env:NO_COLOR
    $fancy = [bool]($env:WT_SESSION -or $env:TERM_PROGRAM)
    $ok = if ($fancy) { [string][char]0x2713 } else { '+' }

    function Write-Step([string]$Label, [string]$Value, [string]$Mark = $ok, [string]$Color = 'Green') {
        Write-Host '  ' -NoNewline
        if ($useColor) { Write-Host $Mark -ForegroundColor $Color -NoNewline } else { Write-Host $Mark -NoNewline }
        Write-Host (' {0,-11} {1}' -f $Label, $Value)
    }
    function Write-Note([string]$Label, [string]$Value) { Write-Step $Label $Value '!' 'Yellow' }
    function Write-Detail([string]$Value) { Write-Host ('                {0}' -f $Value) -ForegroundColor DarkGray }
    function Write-Title([string]$Text) { Write-Host ''; Write-Host $Text; Write-Host '' }
    function Exit-Installer([string]$Message, [string[]]$Details = @()) {
        $text = (@($Message) + ($Details | ForEach-Object { "  $_" })) -join "`n    "
        throw [System.InvalidOperationException]::new($text)
    }
    function Get-PrettyPath([string]$Path) {
        $userHome = [Environment]::GetFolderPath('UserProfile')
        if ($Path.StartsWith($userHome + '\', [StringComparison]::OrdinalIgnoreCase)) { return '~' + $Path.Substring($userHome.Length) }
        return $Path
    }
    function Get-ReportedVersion([string]$Exe) {
        try {
            # Collect all output first: in PowerShell 7, Select-Object -First
            # can stop the program early and turn its exit status into a failure.
            $out = @(& $Exe version 2>$null)
            if ($LASTEXITCODE -eq 0 -and $out.Count -gt 0 -and $out[0] -match '^pdfconcat (\S+)') { return $Matches[1] }
            return $null
        } catch {
            return $null
        }
    }
    function Test-SamePath([string]$A, [string]$B) {
        $a2 = [Environment]::ExpandEnvironmentVariables($A).TrimEnd('\')
        $b2 = [Environment]::ExpandEnvironmentVariables($B).TrimEnd('\')
        return [string]::Equals($a2, $b2, [StringComparison]::OrdinalIgnoreCase)
    }
    # The user PATH is read and written raw so %VARIABLE% entries and the
    # registry value type survive. SetEnvironmentVariable would expand them.
    function Get-UserPath {
        $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment')
        try { $raw = [string]$key.GetValue('Path', '', 'DoNotExpandEnvironmentNames') } finally { $key.Close() }
        return @($raw -split ';' | Where-Object { $_ })
    }
    function Save-UserPath([string[]]$Entries) {
        $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
        try {
            $kind = [Microsoft.Win32.RegistryValueKind]::ExpandString
            if ($key.GetValueNames() -contains 'Path') { $kind = $key.GetValueKind('Path') }
            $key.SetValue('Path', ($Entries -join ';'), $kind)
        } finally { $key.Close() }
        # Setting and clearing a user variable broadcasts WM_SETTINGCHANGE, so
        # new terminals opened from Explorer see the updated PATH.
        [Environment]::SetEnvironmentVariable('PDFCONCAT_INSTALLER', '1', 'User')
        [Environment]::SetEnvironmentVariable('PDFCONCAT_INSTALLER', $null, 'User')
    }

    if ($PSVersionTable.PSEdition -eq 'Core' -and -not $IsWindows) {
        Exit-Installer 'This installer is for Windows.' @('On macOS and Linux, run:', "curl -fsSL $repoUrl/releases/latest/download/install.sh | sh")
    }
    if (-not $InstallDir) { $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\PDFConcat' }
    if (-not [IO.Path]::IsPathRooted($InstallDir)) { Exit-Installer "The install directory must be an absolute path: $InstallDir" }
    $InstallDir = [IO.Path]::GetFullPath($InstallDir).TrimEnd('\')
    $exe = Join-Path $InstallDir 'pdfconcat.exe'
    $retired = "$exe.old"

    if ($Uninstall) {
        Write-Title 'PDFConcat uninstaller'
        $changed = $false
        if (Test-Path -LiteralPath $exe -PathType Container) { Exit-Installer "$(Get-PrettyPath $exe) is a folder, so it was left alone." }
        if (Test-Path -LiteralPath $exe) {
            $old = Get-ReportedVersion $exe
            if (-not $old) { $old = 'unknown version' }
            try { Remove-Item -LiteralPath $exe -Force }
            catch { Exit-Installer "Could not remove $(Get-PrettyPath $exe)." @('Close any running pdfconcat, then try again.') }
            Write-Step 'Removed' "$(Get-PrettyPath $exe) ($old)"
            $changed = $true
        }
        Remove-Item -LiteralPath $retired -Force -ErrorAction SilentlyContinue
        if ((Test-Path -LiteralPath $InstallDir) -and -not (Get-ChildItem -LiteralPath $InstallDir -Force)) {
            Remove-Item -LiteralPath $InstallDir -Force
        }
        # Remove the PATH entry only once the folder is gone, so a shared
        # folder holding other programs keeps working.
        if (-not (Test-Path -LiteralPath $InstallDir)) {
            $entries = @(Get-UserPath)
            $kept = @($entries | Where-Object { -not (Test-SamePath $_ $InstallDir) })
            if ($kept.Count -ne $entries.Count) {
                Save-UserPath $kept
                Write-Step 'PATH' "removed $(Get-PrettyPath $InstallDir) from your user PATH"
                $changed = $true
            }
        }
        if (-not $changed) {
            Write-Host "  PDFConcat is not installed in $(Get-PrettyPath $InstallDir). Nothing was changed."
            Write-Host ''
            return
        }
        Write-Host ''
        Write-Host '  PDFConcat has been uninstalled.'
        Write-Host '  Your PDFs, plans and reports were not touched.'
        Write-Host ''
        return
    }

    $Version = "$Version".TrimStart('v')
    if ($Version -and $Version -notmatch '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$') {
        Exit-Installer "Not a release version: $Version" @('Use a stable version such as 0.7.0.')
    }

    Write-Title 'PDFConcat installer'

    # The registry reports the native processor even when this PowerShell runs
    # under x64 emulation on an Arm device.
    $native = (Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Environment').PROCESSOR_ARCHITECTURE
    $arch = switch ($native) { 'AMD64' { 'amd64' } 'ARM64' { 'arm64' } default { $null } }
    if (-not $arch) { Exit-Installer "PDFConcat has no Windows release for the $native processor." @('Build it from source; see the README.') }
    $osVersion = [Environment]::OSVersion.Version
    Write-Step 'System' "Windows $($osVersion.Major).$($osVersion.Minor) build $($osVersion.Build) - $arch"

    $asset = "pdfconcat_windows_$arch.zip"
    if ($env:PDFCONCAT_DOWNLOAD_BASE) {
        $base = $env:PDFCONCAT_DOWNLOAD_BASE.TrimEnd('/', '\')
        if (-not ($base.StartsWith('https://') -or [IO.Path]::IsPathRooted($base))) {
            Exit-Installer 'PDFCONCAT_DOWNLOAD_BASE must be an https:// URL or an absolute directory.'
        }
        $sourceLabel = $base
    } elseif ($Version) {
        $base = "$repoUrl/releases/download/v$Version"
        $sourceLabel = "the v$Version release"
    } else {
        $base = "$repoUrl/releases/latest/download"
        $sourceLabel = 'the latest release'
    }

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("pdfconcat-install-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        if ($base.StartsWith('https://')) {
            [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
        }
        foreach ($name in @($asset, 'checksums.txt')) {
            try {
                if ($base.StartsWith('https://')) {
                    Invoke-WebRequest -Uri "$base/$name" -OutFile (Join-Path $tmp $name) -UseBasicParsing
                } else {
                    Copy-Item -LiteralPath (Join-Path $base $name) -Destination (Join-Path $tmp $name)
                }
            } catch {
                Exit-Installer "Could not download $name from $sourceLabel." @('Check your internet connection and that the release exists:', "$repoUrl/releases")
            }
        }
        $archive = Join-Path $tmp $asset
        $size = (Get-Item -LiteralPath $archive).Length
        Write-Step 'Downloaded' ('{0} - {1:N1} MB' -f $asset, ($size / 1MB))
        Write-Detail "from $sourceLabel"

        $expected = @(Get-Content -LiteralPath (Join-Path $tmp 'checksums.txt') | ForEach-Object {
            $fields = $_ -split '\s+'
            if ($fields.Count -ge 2 -and ($fields[1] -eq $asset -or $fields[1] -eq "*$asset")) { $fields[0].ToLowerInvariant() }
        })
        if ($expected.Count -ne 1) { Exit-Installer "checksums.txt has no unique entry for $asset." @('Nothing was installed.') }
        $actual = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $expected[0]) {
            Exit-Installer 'The download does not match its published SHA-256 checksum.' @('Nothing was installed. Try again; if this persists, please report it:', "$repoUrl/issues")
        }
        Write-Step 'Verified' 'SHA-256 matches the release checksums'

        # Extract only the executable; other archive members are never written.
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        $candidate = Join-Path $tmp 'pdfconcat.exe'
        $zip = [IO.Compression.ZipFile]::OpenRead($archive)
        try {
            $entry = $zip.GetEntry('pdfconcat.exe')
            if (-not $entry) { Exit-Installer "$asset does not contain pdfconcat.exe." }
            [IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $candidate)
        } finally { $zip.Dispose() }

        $newVersion = Get-ReportedVersion $candidate
        if (-not $newVersion) {
            Exit-Installer 'The downloaded program does not run on this system.' @('Nothing was installed.', "Running it directly shows the reason: & '$candidate' version")
        }
        if ($Version -and $newVersion -ne $Version) {
            Exit-Installer "The downloaded program reports version $newVersion, not $Version." @('Nothing was installed.')
        }
        Write-Step 'Tested' "pdfconcat $newVersion runs on this system"

        if (Test-Path -LiteralPath $exe -PathType Container) {
            Exit-Installer "$(Get-PrettyPath $exe) is a folder, so it was left alone." @('Remove it yourself, or choose another location with -InstallDir.')
        }
        $oldVersion = $null
        if (Test-Path -LiteralPath $exe) {
            $oldVersion = Get-ReportedVersion $exe
            if (-not $oldVersion) { $oldVersion = 'an unknown version' }
        }
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        # Windows cannot overwrite a running program but can rename it, so the
        # old file is moved aside first and deleted when it is no longer in use.
        Remove-Item -LiteralPath $retired -Force -ErrorAction SilentlyContinue
        try {
            if (Test-Path -LiteralPath $exe) { Move-Item -LiteralPath $exe -Destination $retired -Force }
            Move-Item -LiteralPath $candidate -Destination $exe
        } catch {
            if ((Test-Path -LiteralPath $retired) -and -not (Test-Path -LiteralPath $exe)) { Move-Item -LiteralPath $retired -Destination $exe }
            Exit-Installer "Could not write $(Get-PrettyPath $exe)." @('Close any running pdfconcat and check that you can write to that folder.')
        }
        Remove-Item -LiteralPath $retired -Force -ErrorAction SilentlyContinue
        if (-not $oldVersion) { Write-Step 'Installed' (Get-PrettyPath $exe) }
        elseif ($oldVersion -eq $newVersion) { Write-Step 'Installed' "$(Get-PrettyPath $exe) (reinstalled $newVersion)" }
        else { Write-Step 'Installed' "$(Get-PrettyPath $exe) (updated from $oldVersion)" }
    } finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }

    $onUserPath = [bool](@(Get-UserPath) | Where-Object { Test-SamePath $_ $InstallDir })
    $onSessionPath = [bool]($env:Path -split ';' | Where-Object { $_ -and (Test-SamePath $_ $InstallDir) })
    if ($onUserPath) {
        Write-Step 'PATH' "already includes $(Get-PrettyPath $InstallDir)"
    } elseif (-not $ModifyPath) {
        Write-Note 'PATH' "$(Get-PrettyPath $InstallDir) is not on your PATH (left unchanged)"
    } else {
        Save-UserPath (@($InstallDir) + @(Get-UserPath))
        Write-Step 'PATH' "added $(Get-PrettyPath $InstallDir) to your user PATH"
    }
    # When run through iex this is the caller's own session, so the command
    # works immediately; in a child powershell it only affects that process.
    if (($onUserPath -or $ModifyPath) -and -not $onSessionPath) { $env:Path = "$InstallDir;$env:Path" }

    $found = Get-Command pdfconcat -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($found -and -not (Test-SamePath $found.Source $exe) -and $onUserPath) {
        Write-Host ''
        Write-Note 'Note' "your PATH finds another pdfconcat first: $(Get-PrettyPath $found.Source)"
        Write-Detail "Remove that copy, or run this one as $exe."
    }

    Write-Host ''
    Write-Host "  PDFConcat $newVersion is installed."
    Write-Host ''
    if ($onUserPath -or $ModifyPath) {
        if (-not $onUserPath) { Write-Host '  Open a new terminal window if pdfconcat is not found in this one.' ; Write-Host '' }
        Write-Host '  Get started:'
        Write-Host '      pdfconcat --help'
    } else {
        Write-Host "  Run it by its full path, or add $(Get-PrettyPath $InstallDir) to your PATH:"
        Write-Host "      & '$exe' --help"
    }
    Write-Host '      pdfconcat build -o blank.pdf --blank --fit-to A4'
    Write-Host ''
    Write-Host "  Guide:     $docsUrl" -ForegroundColor DarkGray
    Write-Host ('  Uninstall: & ([scriptblock]::Create((irm {0}/releases/latest/download/install.ps1))) -Uninstall' -f $repoUrl) -ForegroundColor DarkGray
    Write-Host ''
}

if ($Help) {
    Write-Host @'
Install, update or uninstall PDFConcat for the current Windows user.

Options:
  -Version X.Y.Z     Install that release instead of the latest one
  -InstallDir DIR    Install into DIR (default: %LOCALAPPDATA%\Programs\PDFConcat)
  -NoModifyPath      Do not add the install folder to the user PATH
  -Uninstall         Remove PDFConcat and the PATH entry this installer added
  -Help              Show this help

Environment: PDFCONCAT_VERSION, PDFCONCAT_INSTALL_DIR, PDFCONCAT_NO_MODIFY_PATH=1,
PDFCONCAT_DOWNLOAD_BASE (a release mirror URL or a local folder of release files).
'@
    return
}

try {
    Invoke-PDFConcatInstaller -Version $Version -InstallDir $InstallDir `
        -ModifyPath (-not ($NoModifyPath -or $env:PDFCONCAT_NO_MODIFY_PATH -eq '1')) `
        -Uninstall ($Uninstall -or $env:PDFCONCAT_UNINSTALL -eq '1')
} catch {
    $mark = if ($env:WT_SESSION -or $env:TERM_PROGRAM) { [string][char]0x2717 } else { 'x' }
    Write-Host ''
    Write-Host "  $mark $($_.Exception.Message)" -ForegroundColor Red
    Write-Host ''
    Write-Host '    Help: https://github.com/resoltico/pdfconcat/blob/main/docs/INSTALL.md'
    Write-Host ''
    # Under "irm | iex", exit would close the caller's PowerShell window, so
    # fail with an error instead; scripts still see a nonzero exit status.
    if ($MyInvocation.MyCommand.Path) { exit 1 }
    throw 'PDFConcat setup did not complete.'
}
