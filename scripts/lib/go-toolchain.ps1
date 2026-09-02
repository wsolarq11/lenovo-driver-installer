<#
.SYNOPSIS
Shared Go toolchain discovery for the Lenovo driver installer scripts.

.DESCRIPTION
Dot-source this file (`. (Join-Path $PSScriptRoot 'lib\go-toolchain.ps1')`)
then call Resolve-GoExe. It is the single source of the go.exe lookup rules
used by both scripts/verify.ps1 and scripts/dev.ps1 so the two never drift
apart. It performs no side effects and runs offline.
#>

function Resolve-GoExe {
    param(
        [string]$GoExe = ''
    )
    if ($GoExe) {
        if (-not (Test-Path -LiteralPath $GoExe)) {
            throw "Go executable not found: $GoExe"
        }
        return $GoExe
    }
    $candidate = Get-Command go.exe -ErrorAction SilentlyContinue
    if ($candidate) {
        return $candidate.Source
    }
    if ($env:GOROOT -and (Test-Path -LiteralPath (Join-Path $env:GOROOT 'bin\go.exe'))) {
        return Join-Path $env:GOROOT 'bin\go.exe'
    }
    if ($env:USERPROFILE -and (Test-Path -LiteralPath "$env:USERPROFILE\.local\go\bin\go.exe")) {
        return "$env:USERPROFILE\.local\go\bin\go.exe"
    }
    throw 'go.exe not found. Install the official Go toolchain or pass -GoExe.'
}