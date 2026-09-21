<#
.SYNOPSIS
Behavioral acceptance harness for device-level rollback, run inside a disposable
VM (Hyper-V / VMware / VirtualBox snapshot). It deliberately installs one driver
on a machine that is about to be reverted, then verifies the rollback mechanism
end-to-end: offer -> consent surrogate -> Rollback -> ledger rows -> offer state.

.DESCRIPTION
This is NOT run on a real machine. The whole point is to exercise a state-mutating
path (install a driver, possibly break a device, roll it back) and then throw the
machine away. Provide a snapshot command that captures the VM before the run and a
restore command that reverts it after. The script asserts the mechanism, then
prints the before/after local versions so a human confirms the device actually
returned to its previous state.

.PARAMETER DriverCode
Lenovo DriverCode to install. Pick a device you can afford to break in the VM
(e.g. a cardreader or camera driver). Mandatory.

.PARAMETER Engine
Path to lenovo-driver.exe. Defaults to the repo bin\lenovo-driver.exe.

.PARAMETER OsId
Target OS list id. Defaults to 42 (Windows 10 64-bit) but should match the VM OS.

.PARAMETER SnapshotCommand
PowerShell scriptblock run before the install. Example:
  -SnapshotCommand { Hyper-V\Checkpoint-VM -Name w10test -SnapshotName before }

.PARAMETER RestoreCommand
PowerShell scriptblock run at the end to throw the VM away. Example:
  -RestoreCommand { Hyper-V\Restore-VMCheckpoint -Name w10test -SnapshotName before }

.PARAMETER SkipRollback
Stop after install + offer detection, without rolling back. Useful to first
confirm that the chosen driver actually produces a device problem and an offer.

.EXAMPLE
.\scripts\rollback_behavior_test.ps1 -DriverCode n1stc12w -SnapshotCommand { Hyper-V\Checkpoint-VM w10test before } -RestoreCommand { Hyper-V\Restore-VMCheckpoint w10test before }
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$DriverCode,
    [string]$Engine = (Join-Path $PSScriptRoot '..\bin\lenovo-driver.exe'),
    [string]$OsId = '42',
    [scriptblock]$SnapshotCommand,
    [scriptblock]$RestoreCommand,
    [switch]$SkipRollback
)

$ErrorActionPreference = 'Stop'
$historyPath = Join-Path $env:LOCALAPPDATA 'Lenovo\DriverInstaller\lenovo_driver_history.csv'
$offerPath   = Join-Path $env:LOCALAPPDATA 'Lenovo\DriverInstaller\lenovo_driver_rollback.json'

function Fail($msg) {
    Write-Host "FAIL: $msg" -ForegroundColor Red
    throw $msg
}

# Admin is required for install and rollback alike.
$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Fail 'run this harness from an elevated shell'
}

if (-not (Test-Path -LiteralPath $Engine)) { Fail "engine not found: $Engine" }

if ($SnapshotCommand) {
    Write-Host '==> Snapshot VM'
    & $SnapshotCommand
}

try {
    Write-Host "==> Before install (dry-run local version for $DriverCode)"
    $before = & $Engine -Elevated -DryRun -CurrentOSOnly 2>&1
    if ($LASTEXITCODE -ne 0) { Fail "pre-install dry-run exited $LASTEXITCODE" }
    $beforeRow = ($before | Select-String -SimpleMatch $DriverCode | Select-Object -First 1).Line
    Write-Host ("    before: {0}" -f $beforeRow)

    Write-Host "==> Install $DriverCode"
    & $Engine -Elevated -GuiInstallCodes $DriverCode -TargetOS $OsId 2>&1 | Write-Host
    if ($LASTEXITCODE -ne 0) { Fail "install exited $LASTEXITCODE" }

    $hasOffer = (Test-Path -LiteralPath $offerPath)
    if (-not $hasOffer) {
        Write-Host 'INFO: no rollback offer produced. Chosen driver did not cause a device problem;'
        Write-Host '      pick a driver that actually breaks a device in the VM, or inspect the ledger.'
        return
    }

    $offers = @((Get-Content -LiteralPath $offerPath -Raw | ConvertFrom-Json).Offers)
    $pending = @($offers | Where-Object { $_.State -eq 'pending' -and $_.DriverCode -eq $DriverCode })
    if ($pending.Count -eq 0) {
        Fail "offer file exists but no pending offer for $DriverCode"
    }
    Write-Host "    pending offer: before=$($pending[0].BeforeVersion) after=$($pending[0].AfterVersion) inf=$($pending[0].BeforeInf)"

    if ($SkipRollback) {
        Write-Host '    -SkipRollback set; stopping here.'
        return
    }

    Write-Host '==> Rollback (consent surrogate in disposable VM)'
    & $Engine -Elevated -Rollback $DriverCode 2>&1 | Write-Host
    $rollbackExit = $LASTEXITCODE

    $offerAfter = @((Get-Content -LiteralPath $offerPath -Raw | ConvertFrom-Json).Offers | Where-Object { $_.DriverCode -eq $DriverCode })
    if ($offerAfter.Count -eq 0) { Fail 'offer missing after rollback' }
    Write-Host ("    offer state: {0}" -f $offerAfter[0].State)

    $ledger = Get-Content -LiteralPath $historyPath -Raw
    if ($ledger -notmatch 'RollbackOffered') { Fail 'no RollbackOffered ledger row' }
    if ($ledger -notmatch 'Rollback') { Fail 'no Rollback intent ledger row' }
    if ($ledger -notmatch 'RolledBack' -and $ledger -notmatch 'RollbackFailed') {
        Fail 'no rollback outcome ledger row'
    }

    Write-Host '==> After rollback (dry-run local version)'
    $after = & $Engine -Elevated -DryRun -CurrentOSOnly 2>&1
    $afterRow = ($after | Select-String -SimpleMatch $DriverCode | Select-Object -First 1).Line
    Write-Host ("    after:  {0}" -f $afterRow)

    if ($rollbackExit -ne 0) { Fail "rollback exited $rollbackExit" }
    Write-Host 'PASS rollback mechanism (confirm before/after versions above show a reverted local version)'
}
finally {
    if ($RestoreCommand) {
        Write-Host '==> Restore VM'
        & $RestoreCommand
    }
}
