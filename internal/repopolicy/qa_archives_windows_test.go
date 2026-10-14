//go:build windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsOracleExtractionRejectsWrongHashAndMissingExecutable(t *testing.T) {
	t.Parallel()

	command := exec.CommandContext(t.Context(), "pwsh", "-NoProfile", "-NonInteractive", "-Command", archiveChecks)
	command.Env = append(os.Environ(), "QA_INSTALLER_SOURCE="+filepath.Join(repoRoot(t), "tools", "install-qa-windows.ps1"),
		"QA_FIXTURE_ROOT="+t.TempDir())
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("native PowerShell archive checks: %v\n%s", err, output)
	}
	for _, marker := range []string{"wrong-hash rejected before extraction", "missing-oracle rejected", "all-four-oracles retained"} {
		if !strings.Contains(string(output), marker) {
			t.Fatalf("archive control did not complete %q:\n%s", marker, output)
		}
	}
}

const archiveChecks = `
$ErrorActionPreference = 'Stop'
$ast = [System.Management.Automation.Language.Parser]::ParseFile($env:QA_INSTALLER_SOURCE, [ref]$null, [ref]$null)
$functions = @($ast.FindAll({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Expand-QaArchive' }, $true))
if ($functions.Count -ne 1) { throw 'Expected one production archive verifier' }
. ([scriptblock]::Create($functions[0].Extent.Text))
$files = Join-Path $env:QA_FIXTURE_ROOT 'input/bin'
New-Item -ItemType Directory -Path $files -Force | Out-Null
foreach ($name in @('qpdf.exe', 'pdftotext.exe', 'pdfinfo.exe', 'pdftoppm.exe')) { Set-Content -LiteralPath (Join-Path $files $name) -Value 'oracle fixture' }
$archive = Join-Path $env:QA_FIXTURE_ROOT 'oracle.zip'
Compress-Archive -LiteralPath (Join-Path $env:QA_FIXTURE_ROOT 'input/bin') -DestinationPath $archive
$hash = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
$bad = Join-Path $env:QA_FIXTURE_ROOT 'bad-hash'
$rejected = $false
try { Expand-QaArchive $archive ('0' * 64) $bad 'bin' @('qpdf.exe') } catch { if ($_.Exception.Message -notlike 'SHA256 mismatch*') { throw }; $rejected = $true }
if (-not $rejected -or (Test-Path -LiteralPath $bad)) { throw 'Wrong hash reached extraction' }
Write-Output 'wrong-hash rejected before extraction'
$rejected = $false
Remove-Item -LiteralPath (Join-Path $files 'pdfinfo.exe')
$missing = Join-Path $env:QA_FIXTURE_ROOT 'missing.zip'
Compress-Archive -LiteralPath (Join-Path $env:QA_FIXTURE_ROOT 'input/bin') -DestinationPath $missing
$missingHash = (Get-FileHash -LiteralPath $missing -Algorithm SHA256).Hash.ToLowerInvariant()
try { Expand-QaArchive $missing $missingHash (Join-Path $env:QA_FIXTURE_ROOT 'missing') 'bin' @('qpdf.exe', 'pdftotext.exe', 'pdfinfo.exe', 'pdftoppm.exe') } catch { if ($_.Exception.Message -notlike 'Required oracle pdfinfo.exe*') { throw }; $rejected = $true }
if (-not $rejected) { throw 'Missing oracle accepted' }
Write-Output 'missing-oracle rejected'
$bin = Expand-QaArchive $archive $hash (Join-Path $env:QA_FIXTURE_ROOT 'good') 'bin' @('qpdf.exe', 'pdftotext.exe', 'pdfinfo.exe', 'pdftoppm.exe')
foreach ($name in @('qpdf.exe', 'pdftotext.exe', 'pdfinfo.exe', 'pdftoppm.exe')) { if (-not (Test-Path -LiteralPath (Join-Path $bin $name) -PathType Leaf)) { throw 'Missing retained oracle' } }
Write-Output 'all-four-oracles retained'
`
