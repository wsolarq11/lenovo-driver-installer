<#
.SYNOPSIS
    Single source of truth for the ps1 UTF-8 BOM rule, plus an idempotent fixer.

.DESCRIPTION
    Windows PowerShell 5.1 decodes a BOM-less .ps1 with the active ANSI code
    page (GBK on Chinese Windows). A UTF-8 file containing non-ASCII text is
    therefore mis-decoded and can fail to parse. The rule is mechanical and
    total: every project .ps1 that contains non-ASCII bytes MUST carry a UTF-8
    BOM. Pure-ASCII scripts are exempt because ANSI decodes ASCII identically.

    This file is deliberately pure ASCII so it never depends on its own rule.
    scripts/verify.ps1 dot-sources it and auto-restores any drifted BOM before
    running the gate. It can also be run directly:

        powershell -NoProfile -File scripts\fix-bom.ps1

.PARAMETER RepoRoot
    Repository root. Defaults to the parent of this file's directory.
#>
[CmdletBinding()]
param(
    [string]$RepoRoot = ''
)

$ErrorActionPreference = 'Stop'

if (-not $RepoRoot) {
    $RepoRoot = Split-Path -Parent $PSScriptRoot
}

function Get-ProjectPs1Files {
    param([string]$RepoRoot)
    return @(Get-ChildItem -LiteralPath $RepoRoot -Recurse -Filter '*.ps1' -File -ErrorAction SilentlyContinue |
        Where-Object { $_.FullName -notmatch '\\\.git\\' })
}

function Test-NeedsBom {
    param([System.IO.FileInfo]$File)
    $bytes = [System.IO.File]::ReadAllBytes($File.FullName)
    $hasBom = ($bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF)
    if ($hasBom) { return $false }
    foreach ($b in $bytes) {
        if ($b -gt 0x7F) { return $true }
    }
    return $false
}

function Restore-Ps1Bom {
    param([string]$RepoRoot)
    # throwOnInvalidBytes=true: refuse to touch a file that is not valid UTF-8
    # (e.g. a GBK-encoded legacy script) instead of corrupting it silently.
    $strictReader = New-Object System.Text.UTF8Encoding($false, $true)
    $bomWriter = New-Object System.Text.UTF8Encoding($true)
    $fixed = @()
    foreach ($file in (Get-ProjectPs1Files -RepoRoot $RepoRoot)) {
        if (Test-NeedsBom -File $file) {
            $content = [System.IO.File]::ReadAllText($file.FullName, $strictReader)
            [System.IO.File]::WriteAllText($file.FullName, $content, $bomWriter)
            $fixed += $file.FullName
        }
    }
    return ,$fixed
}

# Direct invocation applies the fix immediately. When dot-sourced, InvocationName
# is '.', so verify.ps1 controls the restore through its own gate step instead.
if ($MyInvocation.InvocationName -and $MyInvocation.InvocationName -ne '.') {
    $fixed = Restore-Ps1Bom -RepoRoot $RepoRoot
    if ($fixed.Count -eq 0) {
        Write-Host 'UTF-8 BOM consistent: every non-ASCII ps1 already carries a BOM.'
    } else {
        foreach ($path in $fixed) { Write-Host ("fixed BOM: {0}" -f $path) }
    }
}
