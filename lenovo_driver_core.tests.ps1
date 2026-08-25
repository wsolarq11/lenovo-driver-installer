<#
.SYNOPSIS
Offline self-tests for lenovo_driver_core.ps1.

.DESCRIPTION
This script dot-sources the deterministic core and runs pure-function
assertions without network, registry, PnP, file, console, or process access.
It is intentionally Pester-free so it runs on a stock PowerShell host.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'lenovo_driver_core.ps1')

$script:Passed = 0
$script:Failed = 0

function Assert-True {
    param(
        [bool]$Condition,
        [string]$Name
    )
    if ($Condition) {
        $script:Passed++
        Write-Host ("PASS {0}" -f $Name)
    } else {
        $script:Failed++
        Write-Host ("FAIL {0}" -f $Name) -ForegroundColor Red
    }
}

function Assert-Equal {
    param(
        [AllowNull()]$Expected,
        [AllowNull()]$Actual,
        [string]$Name
    )
    $same = $null -eq $Expected -and $null -eq $Actual
    if (-not $same -and $null -ne $Expected) { $same = "$Expected" -eq "$Actual" }
    Assert-True -Condition $same -Name $Name
}

# Version parsing and status comparison.
Assert-Equal '31.0.15.4630' (Parse-VersionString '31.0.15.4630').ToString() 'Parse-VersionString basic'
Assert-Equal $null (Parse-VersionString 'Intel/Realtek mixed') 'Parse-VersionString multi-vendor returns null'
Assert-Equal 'Update' (Compare-DriverStatus -Remote '2.0.0.5' -Local '1.0.0.1') 'Compare-DriverStatus update'
Assert-Equal 'Up to date' (Compare-DriverStatus -Remote '2.0.0.5' -Local '2.0.0.5') 'Compare-DriverStatus same'
Assert-Equal 'Local newer' (Compare-DriverStatus -Remote '1.0.0.1' -Local '2.0.0.5') 'Compare-DriverStatus local newer'
Assert-Equal 'Not installed' (Compare-DriverStatus -Remote '2.0.0.5' -Local '') 'Compare-DriverStatus missing'

# Hardware and applicability matching.
$localDevices = @(
    [pscustomobject]@{ Name = 'AMD Radeon Graphics'; Class = 'Display'; DeviceId = 'PCI\VEN_1002&DEV_1638&SUBSYS_380317AA&REV_C5'; PnpDeviceId = 'PCI\VEN_1002&DEV_1638&SUBSYS_380317AA&REV_C5' },
    [pscustomobject]@{ Name = 'Realtek Audio'; Class = 'MEDIA'; DeviceId = 'HDAUDIO\FUNC_01&VEN_10EC&DEV_0257&SUBSYS_17AA3803&REV_1000'; PnpDeviceId = 'HDAUDIO\FUNC_01&VEN_10EC&DEV_0257&SUBSYS_17AA3803&REV_1000' },
    [pscustomobject]@{ Name = 'Intel Wi-Fi 6E AX210'; Class = 'Net'; DeviceId = 'PCI\VEN_8086&DEV_2725&SUBSYS_00248086&REV_1A'; PnpDeviceId = 'PCI\VEN_8086&DEV_2725&SUBSYS_00248086&REV_1A' }
)
Assert-True (Test-HardwareMatch -RemoteIds '1002_1638' -LocalPnpId $localDevices[0].PnpDeviceId -LocalDeviceId $localDevices[0].DeviceId) 'Test-HardwareMatch AMD'
Assert-True (-not (Test-HardwareMatch -RemoteIds '10EC_0257' -LocalPnpId $localDevices[2].PnpDeviceId -LocalDeviceId $localDevices[2].DeviceId)) 'Test-HardwareMatch mismatch'

$amdDriver = [pscustomobject]@{ HardwareId = '1002_1638'; DriverName = 'AMD VGA'; Version = '30.0.14052.9003' }
$realtekDriver = [pscustomobject]@{ HardwareId = '10EC_0257'; DriverName = 'Realtek Audio'; Version = '6.0.9363.1' }
Assert-True (Test-DriverApplicable -Driver $amdDriver -LocalDevices $localDevices) 'Test-DriverApplicable AMD'
Assert-True (Test-DriverApplicable -Driver $realtekDriver -LocalDevices $localDevices) 'Test-DriverApplicable Realtek'
Assert-True (-not (Test-DriverApplicable -Driver ([pscustomobject]@{ HardwareId = ''; DriverName = 'NVIDIA VGA'; Version = '1.0' }) -LocalDevices $localDevices)) 'Test-DriverApplicable absent NVIDIA'

# Local device matching and version resolution from a supplied snapshot.
$matched = @(Get-MatchingLocalDevices -Driver $amdDriver -LocalDevices $localDevices)
Assert-Equal 1 $matched.Count 'Get-MatchingLocalDevices count'
$softwareSnapshot = [pscustomobject]@{
    InstalledApps = @()
    ProvisionedAmdPower = ''
    LenovoFnServiceVersion = ''
}
$resolved = Resolve-LocalDriverVersion -Driver $amdDriver -MatchedDevices $matched -DriverVersions @('30.0.14052.9003') -SoftwareSnapshot $softwareSnapshot
Assert-Equal '30.0.14052.9003' $resolved.LocalVersion 'Resolve-LocalDriverVersion device'
Assert-Equal 'AMD' $resolved.LocalVendor 'Resolve-LocalDriverVersion vendor'

$fnDriver = [pscustomobject]@{ HardwareId = ''; DriverName = 'Lenovo Fn and Function Keys'; Version = '2.0.0.25' }
$fnSnapshot = [pscustomobject]@{
    InstalledApps = @([pscustomobject]@{ DisplayName = 'Lenovo Hotkeys'; DisplayVersion = '2.0.0.25' })
    ProvisionedAmdPower = ''
    LenovoFnServiceVersion = ''
}
Assert-Equal '2.0.0.25' (Resolve-InstalledSoftwareVersion -DriverName $fnDriver.DriverName -SoftwareSnapshot $fnSnapshot) 'Resolve-InstalledSoftwareVersion app'
$amdPowerSnapshot = [pscustomobject]@{ InstalledApps = @(); ProvisionedAmdPower = 'Provisioned'; LenovoFnServiceVersion = '' }
Assert-Equal 'Provisioned' (Resolve-InstalledSoftwareVersion -DriverName 'AMD Power Processor' -SoftwareSnapshot $amdPowerSnapshot) 'Resolve-InstalledSoftwareVersion provisioned'

# Source attribution.
$history = @([pscustomobject]@{
    Timestamp = '2026-08-25 10:00:00'
    DriverCode = 'LHK2019'
    OSID = '248'
    OSName = 'Windows 11 64-bit'
    DriverName = 'Lenovo Fn and Function Keys'
    Version = '2.0.0.25'
    Result = 'Installed'
})
$historyDriver = [pscustomobject]@{ DriverCode = 'LHK2019'; DriverName = 'Lenovo Fn and Function Keys'; LocalVersion = '2.0.0.25'; OSID = '42' }
Assert-Equal 'Local newer (source Windows 11 64-bit)' (Resolve-DriverSourceLabel -Driver $historyDriver -History $history -AlternateSourceMap @{}) 'Resolve-DriverSourceLabel history'

$altMap = @{
    'Lenovo Fn and Function Keys|1.0.2.0' = [pscustomobject]@{ OSID = '42'; OSName = 'Windows 10 64-bit' }
    'LHK2019|1.0.2.0' = [pscustomobject]@{ OSID = '42'; OSName = 'Windows 10 64-bit' }
}
$unknownDriver = [pscustomobject]@{ DriverCode = 'LHK2019'; DriverName = 'Lenovo Fn and Function Keys'; LocalVersion = '9.9.9.9'; OSID = '42' }
Assert-Equal 'Local newer (source unknown)' (Resolve-DriverSourceLabel -Driver $unknownDriver -History @() -AlternateSourceMap $altMap) 'Resolve-DriverSourceLabel unknown'

# Driver selection keeps a current-OS row when present, and cross-OS mode
# intentionally picks the newest row after the shell has merged alternate OSes.
$current = [pscustomobject]@{ PartId = 'P1'; DriverName = 'Audio'; OSID = '42'; IssuedDate = [datetime]'2026-01-01'; DriverEditionId = 1 }
$alternate = [pscustomobject]@{ PartId = 'P1'; DriverName = 'Audio'; OSID = '248'; IssuedDate = [datetime]'2026-02-01'; DriverEditionId = 2 }
$selectedCurrent = @(Select-LatestDrivers -Drivers @($current) -CurrentOsId '42')
Assert-Equal 1 $selectedCurrent.Count 'Select-LatestDrivers current OS'
Assert-Equal '42' $selectedCurrent[0].OSID 'Select-LatestDrivers keeps current OS'
$selectedMixed = @(Select-LatestDrivers -Drivers @($alternate, $current) -CurrentOsId '42')
Assert-Equal 1 $selectedMixed.Count 'Select-LatestDrivers mixed count'
Assert-Equal '248' $selectedMixed[0].OSID 'Select-LatestDrivers mixed picks newest'

# Selection token parsing.
$parsed = Parse-DriverSelectionTokens -InputText '1,2,3,foo,99' -AllSelected @(
    [pscustomobject]@{ CompareStatus = 'Update' },
    [pscustomobject]@{ CompareStatus = 'Not applicable' },
    [pscustomobject]@{ CompareStatus = 'Update' }
)
Assert-Equal 2 $parsed.Selected.Count 'Parse-DriverSelectionTokens selected'
Assert-Equal 2 $parsed.Invalid.Count 'Parse-DriverSelectionTokens invalid'
Assert-Equal 1 $parsed.NotApplicable.Count 'Parse-DriverSelectionTokens not applicable'

# Plan and table formatting remain deterministic and text-only.
$planDriver = [pscustomobject]@{
    DriverName = 'AMD VGA'
    Version = '30.0.14052.9003'
    LocalVersion = '27.20.15026.8004'
    CompareStatus = 'Update'
    CompareSource = ''
    OsName = 'Windows 10 64-bit'
    OSID = '42'
    SourceApi = 'QuickFix'
    OfficialMd5 = 'abc123'
    FileName = 'amd.exe'
    FilePath = 'https://example.invalid/amd.exe'
}
$planLines = @(Build-PlanText -Drivers @($planDriver) -GeneratedAt '2026-08-25 10:00:00')
Assert-True (($planLines -join "`n") -match 'AMD VGA') 'Build-PlanText driver'
Assert-True (($planLines -join "`n") -match 'abc123') 'Build-PlanText md5'
$tableLines = @(Format-DriverTableLines -Drivers @($planDriver))
Assert-Equal 5 $tableLines.Count 'Format-DriverTableLines rows'
$summaryLines = @(Format-StatusSummaryLines -Drivers @([pscustomobject]@{ CompareStatus = 'Update'; CompareSource = '' }))
Assert-True (($summaryLines -join "`n") -match 'Update') 'Format-StatusSummaryLines update'

# Download/installer pure helpers.
Assert-Equal 1048576 (ConvertTo-Bytes '1 MB') 'ConvertTo-Bytes MB'
Assert-True (Test-FileSizeMatch -ExpectedBytes 1000 -ActualBytes 1002) 'Test-FileSizeMatch tolerance'
Assert-True (-not (Test-FileSizeMatch -ExpectedBytes 1000 -ActualBytes 5000)) 'Test-FileSizeMatch mismatch'
Assert-True (Test-RebootExitCode -ExitCode 3010) 'Test-RebootExitCode 3010'
Assert-True (-not (Test-RebootExitCode -ExitCode 0)) 'Test-RebootExitCode normal'

# Driver list parser stays pure and handles both source shapes.
$quickFixPayload = [pscustomobject]@{
    Data = [pscustomobject]@{
        partList = @([pscustomobject]@{ PartID = '7'; PartName = 'Video' })
        osList = @([pscustomobject]@{ OSID = '42'; OSName = 'Windows 10 64-bit' })
        driverList = @([pscustomobject]@{
            PartID = '7'
            DriverName = 'AMD VGA'
            DriverCode = 'AMDVGA'
            DriverEdtionId = '1'
            Version = '30.0.14052.9003'
            FileName = 'amd.exe'
            FilePath = 'https://example.invalid/amd.exe'
            FileSize = '1 MB'
            FileType = 'exe'
            Parameter = '/VERYSILENT'
            Bootfile = ''
            HardwareId = '1002_1638'
            MD5 = 'abc123'
            Status = ''
            IsEnable = ''
            PubTime = '2026/1/1'
        })
    }
}
$parsedDrivers = @(Get-DriverObjects -ListData $quickFixPayload -OsId '42' -SourceApi 'QuickFix')
Assert-Equal 1 $parsedDrivers.Count 'Get-DriverObjects QuickFix'
Assert-Equal 'abc123' $parsedDrivers[0].OfficialMd5 'Get-DriverObjects official md5'

$webPayload = [pscustomobject]@{
    data = [pscustomobject]@{
        partList = @([pscustomobject]@{ PartID = '7'; drivelist = @([pscustomobject]@{
            PartID = '7'
            DriverName = 'AMD VGA'
            DriverCode = 'AMDVGA'
            DriverEditionId = '1'
            Version = '30.0.14052.9003'
            FileName = 'amd.exe'
            FilePath = 'https://example.invalid/amd.exe'
            FileSize = '1 MB'
            FileType = 'exe'
            InstallCode = '/VERYSILENT'
            Bootfile = ''
            HardwareId = '1002_1638'
            MD5 = ''
            Status = ''
            IsEnable = ''
            DriverIssuedDateTime = '2026/1/1'
        }) })
        osList = @([pscustomobject]@{ OSID = '42'; OSName = 'Windows 10 64-bit' })
    }
}
$webDrivers = @(Get-DriverObjects -ListData $webPayload -OsId '42' -SourceApi 'Web')
Assert-Equal 1 $webDrivers.Count 'Get-DriverObjects Web'
Assert-Equal '' $webDrivers[0].OfficialMd5 'Get-DriverObjects web md5 empty'

Write-Host ''
Write-Host ("Core tests: passed={0}, failed={1}" -f $script:Passed, $script:Failed)
if ($script:Failed -gt 0) { exit 1 }
exit 0
