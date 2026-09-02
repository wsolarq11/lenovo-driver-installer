<#
.SYNOPSIS
Unified local development loop for the Lenovo driver installer.

.DESCRIPTION
Chains the fast inner loop: go build -> go vet -> gofmt check -> go test ->
build bin\lenovo-driver.exe. It is an offline, fast iteration entry point;
running it never calls the real Lenovo API, downloads drivers, or installs
anything.

Run the full offline acceptance gate with -Verify, which delegates to
scripts/verify.ps1 and exits with its status.

.PARAMETER GoExe
Path to go.exe. When empty, Resolve-GoExe finds the toolchain (PATH, GOROOT,
or the user-local fallback).

.PARAMETER Verify
Run the full acceptance gate (scripts/verify.ps1) instead of the inner loop.

.PARAMETER SkipBin
Skip building bin/lenovo-driver.exe at the end of the inner loop.
#>
[CmdletBinding()]
param(
    [string]$GoExe = '',
    [switch]$Verify,
    [switch]$SkipBin
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $repoRoot

. (Join-Path $PSScriptRoot 'lib\go-toolchain.ps1')
$GoExe = Resolve-GoExe -GoExe $GoExe

$failed = @()
$total = 0

function Invoke-Step {
    param(
        [string]$Name,
        [scriptblock]$Body
    )
    $script:total++
    Write-Host "==> $Name"
    try {
        & $Body
        Write-Host "PASS $Name"
    } catch {
        Write-Host "FAIL ${Name}: $($_.Exception.Message)"
        $script:failed += $Name
    }
}

if ($Verify) {
    $gateScript = Join-Path $PSScriptRoot 'verify.ps1'
    if ($GoExe) {
        & $gateScript -GoExe $GoExe
    } else {
        & $gateScript
    }
    exit $LASTEXITCODE
}

Invoke-Step 'Go build' {
    & $GoExe build ./...
    if ($LASTEXITCODE -ne 0) { throw "go build exited $LASTEXITCODE" }
}

Invoke-Step 'Go vet' {
    & $GoExe vet ./...
    if ($LASTEXITCODE -ne 0) { throw "go vet exited $LASTEXITCODE" }
}

Invoke-Step 'gofmt check' {
    $gofmt = Join-Path (Split-Path $GoExe) 'gofmt.exe'
    if (-not (Test-Path -LiteralPath $gofmt)) { $gofmt = 'gofmt' }
    $unformatted = & $gofmt -l internal cmd
    if ($LASTEXITCODE -ne 0) { throw "gofmt check exited $LASTEXITCODE" }
    if ($unformatted) { throw "files need gofmt: $($unformatted -join ', ')" }
}

Invoke-Step 'Go test' {
    & $GoExe test ./...
    if ($LASTEXITCODE -ne 0) { throw "go test exited $LASTEXITCODE" }
}

if (-not $SkipBin) {
    Invoke-Step 'Build bin/lenovo-driver.exe' {
        $binDir = Join-Path $repoRoot 'bin'
        New-Item -ItemType Directory -Path $binDir -Force | Out-Null
        & $GoExe build -o (Join-Path $binDir 'lenovo-driver.exe') ./cmd/lenovo-driver
        if ($LASTEXITCODE -ne 0) { throw "go build bin exited $LASTEXITCODE" }
    }
}

Write-Host "Dev loop summary: $total steps, $($failed.Count) failed"
Write-Host "Run .\scripts\verify.ps1 (or re-run with -Verify) for the full offline acceptance gate."
if ($failed.Count -gt 0) {
    Write-Output ("Failed: {0}" -f ($failed -join ', '))
    exit 1
}
Write-Host 'DEV_OK'
exit 0