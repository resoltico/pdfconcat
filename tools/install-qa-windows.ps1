# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Ervins Strauhmanis

# Install the exact Windows oracle distributions pinned in tools/versions.env.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Expand-QaArchive {
    param([string]$Archive, [string]$Sha256, [string]$Destination,
          [string]$BinaryDirectory, [string[]]$Executables)
    if ($Sha256 -cnotmatch '^[a-f0-9]{64}$' -or
        (Get-FileHash -LiteralPath $Archive -Algorithm SHA256).Hash.ToLowerInvariant() -cne $Sha256) {
        throw "SHA256 mismatch for $Archive"
    }
    Write-Host "Verified SHA256 $Sha256 for $(Split-Path -Leaf $Archive)"
    Expand-Archive -LiteralPath $Archive -DestinationPath $Destination
    $bin = Join-Path $Destination $BinaryDirectory
    foreach ($name in $Executables) {
        if (-not (Test-Path -LiteralPath (Join-Path $bin $name) -PathType Leaf)) {
            throw "Required oracle $name is absent from $Archive"
        }
    }
    return $bin
}

function Get-QaArchive {
    param([string]$Url, [string]$Archive)
    # curl 8.4 added enforcement while receiving unknown-length responses.
    $banner = & curl.exe --version
    if ($LASTEXITCODE -ne 0 -or $banner[0] -notmatch '^curl (\d+\.\d+\.\d+) ' -or
        [version]$Matches[1] -lt [version]'8.4.0') {
        throw 'Windows QA downloads require curl 8.4 or later for streaming size enforcement'
    }
    Write-Host $banner[0]
    & curl.exe --fail --location --silent --show-error --proto '=https' --proto-redir '=https' `
        --connect-timeout 15 --max-time 120 --max-filesize 67108864 --output $Archive $Url
    if ($LASTEXITCODE -ne 0) { throw "Oracle archive download failed with curl exit $LASTEXITCODE" }
}

$owned = Join-Path $env:RUNNER_TEMP ('pdfconcat-qa-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $owned | Out-Null
$installed = $false
try {
    $qpdfVersion = $env:QPDF_WINDOWS_VERSION
    $popplerRelease = $env:POPPLER_WINDOWS_RELEASE
    $popplerVersion = $popplerRelease -replace '-\d+$', ''
    if ($qpdfVersion -cnotmatch '^\d+\.\d+\.\d+$' -or $popplerRelease -cnotmatch '^\d+\.\d+\.\d+-\d+$') {
        throw 'Windows oracle versions must identify exact release archives'
    }
    $qpdf = Join-Path $owned 'qpdf.zip'
    $poppler = Join-Path $owned 'poppler.zip'
    try {
        Get-QaArchive "https://github.com/qpdf/qpdf/releases/download/v$qpdfVersion/qpdf-$qpdfVersion-mingw64.zip" $qpdf
        $qpdfBin = Expand-QaArchive $qpdf $env:QPDF_WINDOWS_SHA256 (Join-Path $owned 'qpdf') `
            "qpdf-$qpdfVersion-mingw64/bin" @('qpdf.exe')
        Get-QaArchive "https://github.com/oschwartz10612/poppler-windows/releases/download/v$popplerRelease/Release-$popplerRelease.zip" $poppler
        $popplerBin = Expand-QaArchive $poppler $env:POPPLER_WINDOWS_SHA256 (Join-Path $owned 'poppler') `
            "poppler-$popplerVersion/Library/bin" @('pdftotext.exe', 'pdfinfo.exe', 'pdftoppm.exe')
    } finally {
        foreach ($archive in @($qpdf, $poppler)) {
            if (Test-Path -LiteralPath $archive) { Remove-Item -LiteralPath $archive -Force }
        }
    }
    # Later steps inherit these directories; the version step also prepends them over Git Bash Xpdf.
    Add-Content -LiteralPath $env:GITHUB_PATH -Value @($qpdfBin, $popplerBin)
    Add-Content -LiteralPath $env:GITHUB_ENV -Value @("PDFCONCAT_QPDF_BIN=$qpdfBin", "PDFCONCAT_POPPLER_BIN=$popplerBin")
    $installed = $true
} finally {
    if (-not $installed) { Remove-Item -LiteralPath $owned -Recurse -Force }
}
