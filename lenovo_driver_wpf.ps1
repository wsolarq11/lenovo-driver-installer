<#
.SYNOPSIS
WPF front end for the Lenovo driver installer.

.DESCRIPTION
Launches a desktop UI that queries the Go installer engine through JSON
export jobs and runs selected driver downloads/installations in background
child processes. The CLI remains the deterministic engine; this script only
provides the interactive presentation layer.

.PARAMETER SelfTest
Build the window, render it to PNG, and exit without starting a worker or
requiring an interactive desktop.

.PARAMETER NoElevation
Skip the automatic UAC elevation request. Intended for automated tests and
read-only previews.

.PARAMETER WorkerSmoke
Run the background worker against the real QuickFix API, parse the JSON
export, print the loaded row count, and exit. Intended for verification.
#>
param(
    [switch]$SelfTest,
    [switch]$NoElevation,
    [switch]$WorkerSmoke,
    [string]$Model = '',
    [string]$DownloadDir = ''
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

Add-Type -AssemblyName PresentationFramework
Add-Type -AssemblyName PresentationCore
Add-Type -AssemblyName WindowsBase

$script:InstallerPath = Join-Path $PSScriptRoot 'bin\lenovo-driver.exe'
$script:WorkerProcess = $null
$script:WorkerMode = ''
$script:WorkerExportPath = ''
$script:WorkerStdoutPath = ''
$script:WorkerStderrPath = ''
$script:WorkerStdoutRead = 0
$script:Timer = $null
$script:DriverRows = $null
$script:LastExport = $null
$script:Ui = @{}

. (Join-Path $PSScriptRoot 'wpf\ui.ps1')
. (Join-Path $PSScriptRoot 'wpf\worker.ps1')
. (Join-Path $PSScriptRoot 'wpf\actions.ps1')

function Test-IsAdministrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

if ($SelfTest) {
    $window = New-LenovoDriverWindow
    $shot = Join-Path $env:TEMP 'lenovo_driver_wpf_smoke.png'
    Export-SelfTestScreenshot -Window $window -Path $shot
    Write-Host $shot
    exit 0
}

if ($WorkerSmoke) {
    if (-not (Test-Path -LiteralPath $script:InstallerPath)) {
        Write-Host "WORKER_SMOKE_NO_GO_ENGINE=$script:InstallerPath"
        exit 1
    }
    $window = New-LenovoDriverWindow
    $window.Hide()
    Start-LenovoDriverJob -Export -TargetOsId '248'
    $smokeExportPath = $script:WorkerExportPath
    while ($null -ne $script:WorkerProcess) {
        Start-Sleep -Milliseconds 300
        Update-WorkerStatus
    }
    if ($script:LastExport -and $script:DriverRows -and $script:DriverRows.Count -gt 0) {
        Write-Host ("WORKER_SMOKE_OK rows={0} list={1} ({2})" -f $script:DriverRows.Count, $script:LastExport.ListOsName, $script:LastExport.ListOsId)
        exit 0
    }
    if ($smokeExportPath -and (Test-Path -LiteralPath $smokeExportPath)) {
        try {
            $smokeExport = Get-Content -LiteralPath $smokeExportPath -Raw -Encoding UTF8 | ConvertFrom-Json
            if (@($smokeExport.Drivers).Count -gt 0) {
                Write-Host ("WORKER_SMOKE_OK rows={0} list={1} ({2})" -f @($smokeExport.Drivers).Count, $smokeExport.ListOsName, $smokeExport.ListOsId)
                exit 0
            }
        } catch {
            Write-Host ("SMOKE_JSON_ERROR={0}" -f $_.Exception.Message)
        }
    }
    Write-Host 'WORKER_SMOKE_FAIL'
    exit 1
}

if (-not $NoElevation -and -not (Test-IsAdministrator)) {
    $relaunchArgs = @('-STA', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $PSCommandPath)
    if ($Model) { $relaunchArgs += '-Model'; $relaunchArgs += $Model }
    if ($DownloadDir) { $relaunchArgs += '-DownloadDir'; $relaunchArgs += $DownloadDir }
    $p = Start-Process -FilePath 'powershell.exe' -Verb RunAs -ArgumentList $relaunchArgs -Wait -PassThru
    exit $p.ExitCode
}

$window = New-LenovoDriverWindow
# 启动后自动执行一次机器识别（Export）。否则标题栏会停在 XAML 默认的“正在识别机器...”，
# 因为没有任何后台任务去更新 MachineText。
$window.Add_Loaded({ Start-LenovoDriverJob -Export })
$app = New-Object System.Windows.Application
$app.ShutdownMode = [System.Windows.ShutdownMode]::OnMainWindowClose
$app.Run($window) | Out-Null
