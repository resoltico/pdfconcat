# SPDX-License-Identifier: MPL-2.0
#
# Exercises install.ps1 against release files from a GoReleaser dist directory,
# under both Windows PowerShell 5.1 and PowerShell 7 when available.
#
#   pwsh -File install/install_test.ps1 C:\path\to\dist
#
# Install folders are temporary. The user PATH case changes HKCU\Environment
# and restores it afterwards; it runs only when CI is set.

param([Parameter(Mandatory = $true)][string]$Dist)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 3
$installer = Join-Path $PSScriptRoot 'install.ps1'
$work = Join-Path ([IO.Path]::GetTempPath()) ("pdfconcat-install-test-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
$script:failures = 0

function Check([string]$Why, [bool]$Condition) {
    if (-not $Condition) {
        Write-Host "    FAIL: $Why" -ForegroundColor Red
        $script:failures++
        $out = Join-Path $work 'out.txt'
        if (Test-Path $out) { Get-Content $out | ForEach-Object { Write-Host "      | $_" } }
    }
}

# Runs the installer as a file in a fresh process and returns its exit code;
# output goes to $work\out.txt.
function Invoke-Installer([string]$Shell, [string[]]$Arguments, [string]$Base = $Dist) {
    $env:PDFCONCAT_DOWNLOAD_BASE = $Base
    try {
        & $Shell -NoProfile -ExecutionPolicy Bypass -File $installer @Arguments *> (Join-Path $work 'out.txt')
        return $LASTEXITCODE
    } finally {
        Remove-Item Env:\PDFCONCAT_DOWNLOAD_BASE -ErrorAction SilentlyContinue
    }
}
function Get-Output { Get-Content -Raw (Join-Path $work 'out.txt') }
function Get-UserPathRaw {
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment')
    try { return [string]$key.GetValue('Path', '', 'DoNotExpandEnvironmentNames') } finally { $key.Close() }
}

$shells = @('powershell', 'pwsh') | Where-Object { Get-Command $_ -ErrorAction SilentlyContinue }
try {
    foreach ($shell in $shells) {
        Write-Host "=== $shell"
        $dir = Join-Path $work "$shell-app"
        $exe = Join-Path $dir 'pdfconcat.exe'

        Write-Host '--- fresh install, reinstall and uninstall'
        Check 'install succeeds' ((Invoke-Installer $shell @('-InstallDir', $dir, '-NoModifyPath')) -eq 0)
        Check 'exe installed' (Test-Path $exe)
        if (-not (Test-Path $exe)) { continue }
        Check 'installed exe runs' ((& $exe version) -match '^pdfconcat ')
        $pdf = Join-Path $work "$shell-blank.pdf"
        & $exe build -o $pdf --blank --fit-to A4 | Out-Null
        Check 'installed exe builds a PDF' ($LASTEXITCODE -eq 0 -and (Test-Path $pdf))
        Check 'install reported' ((Get-Output) -match 'is installed')
        Check 'reinstall succeeds' ((Invoke-Installer $shell @('-InstallDir', $dir, '-NoModifyPath')) -eq 0)
        Check 'reinstall reported' ((Get-Output) -match 'reinstalled')
        Check 'no retired copy left' (-not (Test-Path "$exe.old"))

        Write-Host '--- update while pdfconcat is running'
        # A pdfconcat waiting on its plan from stdin keeps the exe in use.
        $start = [Diagnostics.ProcessStartInfo]::new($exe, 'check --plan -')
        $start.UseShellExecute = $false
        $start.RedirectStandardInput = $true
        $start.RedirectStandardOutput = $true
        $start.RedirectStandardError = $true
        $running = [Diagnostics.Process]::Start($start)
        try {
            Check 'install over a running exe succeeds' ((Invoke-Installer $shell @('-InstallDir', $dir, '-NoModifyPath')) -eq 0)
            Check 'updated exe runs' ((& $exe version) -match '^pdfconcat ')
        } finally {
            $running.StandardInput.Close()
            $running.WaitForExit()
        }
        Remove-Item -LiteralPath "$exe.old" -Force -ErrorAction SilentlyContinue

        Write-Host '--- checksum mismatch leaves the installation untouched'
        $before = (Get-FileHash $exe).Hash
        $tampered = Join-Path $work "$shell-tampered"
        New-Item -ItemType Directory -Path $tampered | Out-Null
        Copy-Item (Join-Path $Dist 'pdfconcat_windows_*.zip') $tampered
        Get-Content (Join-Path $Dist 'checksums.txt') | ForEach-Object { ('0' * 64) + '  ' + ($_ -split '\s+')[1] } | Set-Content (Join-Path $tampered 'checksums.txt')
        Check 'tampered install fails' ((Invoke-Installer $shell @('-InstallDir', $dir, '-NoModifyPath') $tampered) -ne 0)
        Check 'mismatch reported' ((Get-Output) -match 'does not match its published SHA-256')
        Check 'exe unchanged' ((Get-FileHash $exe).Hash -eq $before)

        Write-Host '--- version mismatch and missing files fail cleanly'
        Check 'version mismatch fails' ((Invoke-Installer $shell @('-InstallDir', $dir, '-NoModifyPath', '-Version', '999.0.0')) -ne 0)
        Check 'version mismatch reported' ((Get-Output) -match 'not 999.0.0')
        $empty = Join-Path $work "$shell-empty"
        New-Item -ItemType Directory -Path $empty | Out-Null
        Check 'empty release fails' ((Invoke-Installer $shell @('-InstallDir', $dir, '-NoModifyPath') $empty) -ne 0)

        Write-Host '--- uninstall'
        Check 'uninstall succeeds' ((Invoke-Installer $shell @('-InstallDir', $dir, '-Uninstall')) -eq 0)
        Check 'install folder removed' (-not (Test-Path $dir))
        Check 'second uninstall succeeds' ((Invoke-Installer $shell @('-InstallDir', $dir, '-Uninstall')) -eq 0)
        Check 'no-op reported' ((Get-Output) -match 'Nothing was changed')

        Write-Host '--- piped "irm | iex" form with environment options'
        $env:PDFCONCAT_DOWNLOAD_BASE = $Dist
        $env:PDFCONCAT_INSTALL_DIR = $dir
        $env:PDFCONCAT_NO_MODIFY_PATH = '1'
        try {
            & $shell -NoProfile -ExecutionPolicy Bypass -Command "Get-Content -Raw '$installer' | Invoke-Expression" *> (Join-Path $work 'out.txt')
            Check 'iex install succeeds' ($LASTEXITCODE -eq 0 -and (Test-Path $exe))
            $env:PDFCONCAT_UNINSTALL = '1'
            & $shell -NoProfile -ExecutionPolicy Bypass -Command "Get-Content -Raw '$installer' | Invoke-Expression" *> (Join-Path $work 'out.txt')
            Check 'iex uninstall succeeds' ($LASTEXITCODE -eq 0 -and -not (Test-Path $exe))
            Remove-Item Env:\PDFCONCAT_UNINSTALL
            & $shell -NoProfile -ExecutionPolicy Bypass -Command "`$env:PDFCONCAT_VERSION='999.0.0'; Get-Content -Raw '$installer' | Invoke-Expression" *> (Join-Path $work 'out.txt')
            Check 'iex failure exits nonzero' ($LASTEXITCODE -ne 0)
        } finally {
            'PDFCONCAT_DOWNLOAD_BASE', 'PDFCONCAT_INSTALL_DIR', 'PDFCONCAT_NO_MODIFY_PATH', 'PDFCONCAT_UNINSTALL' | ForEach-Object { Remove-Item "Env:\$_" -ErrorAction SilentlyContinue }
        }

        if ($env:CI) {
            Write-Host '--- user PATH is added and removed'
            $saved = Get-UserPathRaw
            try {
                Check 'install with PATH succeeds' ((Invoke-Installer $shell @('-InstallDir', $dir)) -eq 0)
                $entries = (Get-UserPathRaw) -split ';'
                Check 'PATH entry added first' ($entries[0] -eq $dir)
                $savedEntries = @($saved -split ';' | Where-Object { $_ })
                Check 'existing entries kept' ((@($entries | Select-Object -Skip 1) -join ';') -eq ($savedEntries -join ';'))
                Check 'reinstall keeps one entry' ((Invoke-Installer $shell @('-InstallDir', $dir)) -eq 0 -and @((Get-UserPathRaw) -split ';' | Where-Object { $_ -eq $dir }).Count -eq 1)
                Check 'uninstall succeeds' ((Invoke-Installer $shell @('-InstallDir', $dir, '-Uninstall')) -eq 0)
                Check 'PATH restored' ((@((Get-UserPathRaw) -split ';' | Where-Object { $_ }) -join ';') -eq ($savedEntries -join ';'))
            } finally {
                $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
                try { $key.SetValue('Path', $saved, [Microsoft.Win32.RegistryValueKind]::ExpandString) } finally { $key.Close() }
            }
        }
    }
} finally {
    Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host ''
if ($script:failures -gt 0) { Write-Host "$($script:failures) check(s) failed." -ForegroundColor Red; exit 1 }
Write-Host 'All installer checks passed.'
