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

$suiteDriver = [pscustomobject]@{ HardwareId = ''; DriverName = 'Intel 连接性性能套件程序'; Version = '4.1024.1009.5' }
$suiteDevices = @([pscustomobject]@{
    Name = 'Intel(R) Wi-Fi 6E AX211 160MHz'
    Class = 'Net'
    DeviceId = 'PCI\VEN_8086&DEV_2725&SUBSYS_00248086&REV_1A'
    PnpDeviceId = 'PCI\VEN_8086&DEV_2725&SUBSYS_00248086&REV_1A'
})
$suiteSnapshot = [pscustomobject]@{
    InstalledApps = @([pscustomobject]@{ DisplayName = 'Intel(R) Connectivity Performance Suite'; DisplayVersion = '4.1024.1009.5' })
    ProvisionedAmdPower = ''
    LenovoFnServiceVersion = ''
}
$suiteMatched = @(Get-MatchingLocalDevices -Driver $suiteDriver -LocalDevices $suiteDevices)
Assert-Equal 0 $suiteMatched.Count 'Get-MatchingLocalDevices software suite ignores Wi-Fi device'
$suiteResolved = Resolve-LocalDriverVersion -Driver $suiteDriver -MatchedDevices $suiteDevices -DriverVersions @('23.100.0.4') -SoftwareSnapshot $suiteSnapshot
Assert-Equal '4.1024.1009.5' $suiteResolved.LocalVersion 'Resolve-LocalDriverVersion software suite overrides device version'
$suiteMissingSnapshot = [pscustomobject]@{ InstalledApps = @(); ProvisionedAmdPower = ''; LenovoFnServiceVersion = '' }
$suiteMissing = Resolve-LocalDriverVersion -Driver $suiteDriver -MatchedDevices $suiteDevices -DriverVersions @('23.100.0.4') -SoftwareSnapshot $suiteMissingSnapshot
Assert-Equal '' $suiteMissing.LocalVersion 'Resolve-LocalDriverVersion software suite ignores unrelated device version'

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

$verifiedHistory = @([pscustomobject]@{
    Timestamp = '2026-08-25 11:00:00'
    DriverCode = 'WLAN'
    OSID = '248'
    OSName = 'Windows 11 64-bit'
    DriverName = 'Intel Wireless'
    Version = '23.30.2.1'
    VerifiedVersion = '23.100.0.4'
    Result = 'Verified'
})
$verifiedDriver = [pscustomobject]@{ DriverCode = 'WLAN'; DriverName = 'Intel Wireless'; LocalVersion = '23.100.0.4'; OSID = '42' }
Assert-Equal 'Local newer (source Windows 11 64-bit)' (Resolve-DriverSourceLabel -Driver $verifiedDriver -History $verifiedHistory -AlternateSourceMap @{}) 'Resolve-DriverSourceLabel verified history'

$sameOsHistory = @([pscustomobject]@{
    Timestamp = '2026-08-25 11:00:00'
    DriverCode = 'FN'
    OSID = '42'
    OSName = 'Windows 10 64-bit'
    DriverName = 'Lenovo Fn and Function Keys'
    Version = '1.0.2.0'
    VerifiedVersion = '2.0.0.25'
    Result = 'Verified'
})
$sameOsDriver = [pscustomobject]@{ DriverCode = 'FN'; DriverName = 'Lenovo Fn and Function Keys'; LocalVersion = '2.0.0.25'; OSID = '42' }
Assert-Equal 'Local newer (same current OS source)' (Resolve-DriverSourceLabel -Driver $sameOsDriver -History $sameOsHistory -AlternateSourceMap @{}) 'Resolve-DriverSourceLabel same OS verified history'

$unchangedHistory = @([pscustomobject]@{
    Timestamp = '2026-08-25 11:00:00'
    DriverCode = 'WLAN'
    OSID = '42'
    OSName = 'Windows 10 64-bit'
    DriverName = 'Intel Wireless'
    Version = '22.160.0.4'
    VerifiedVersion = '23.100.0.4'
    BeforeVersion = '23.100.0.4'
    Result = 'Verified'
})
$unchangedDriver = [pscustomobject]@{ DriverCode = 'WLAN'; DriverName = 'Intel Wireless'; LocalVersion = '23.100.0.4'; OSID = '42' }
Assert-Equal 'Local newer (source unknown)' (Resolve-DriverSourceLabel -Driver $unchangedDriver -History $unchangedHistory -AlternateSourceMap @{}) 'Resolve-DriverSourceLabel excludes unchanged package'

# setupapi import log parsing and source evidence.
$offlineLogLines = @(
    '>>>  [Import Driver Package - G:\Win2025\Lenovo\Intel_WiFi_23.90.0\Netwtw08.INF]'
    '      cmd: G:\NTLite-Temp\D87ABEB6-E304-4385-BD14-BD481D01517B\dismhost.exe {6488D94D-16D0-4260-A5BB-783D7700AB00}'
    '      inf: Driver Version = 11/11/2024,23.100.0.4'
    '      idb: {Register Driver Package: G:\NTLite-Temp\NLTmpMnt\Windows\System32\DriverStore\FileRepository\netwtw08.inf_amd64_10e2b743974372b6\Netwtw08.INF} 17:11:11.122'
    '      idb:      Created driver package object ''netwtw08.inf_amd64_10e2b743974372b6'' in DRIVERS database node.'
    '      idb:      Created driver INF file object ''oem9.inf'' in DRIVERS database node.'
    '      idb:      Registered driver package ''netwtw08.inf_amd64_10e2b743974372b6'' with ''oem9.inf''.'
    '      cpy:      Published ''netwtw08.inf_amd64_10e2b743974372b6\netwtw08.inf'' to ''oem9.inf''.'
    '      sto: Driver Store Filename = G:\NTLite-Temp\NLTmpMnt\Windows\System32\DriverStore\FileRepository\netwtw08.inf_amd64_10e2b743974372b6\Netwtw08.INF'
    '<<<  Section end 2026/08/18 17:11:11.724'
)
$offlineImports = @(ConvertFrom-ImportLogText -Lines $offlineLogLines -LogPath 'C:\Windows\INF\setupapi.offline.log')
Assert-Equal 1 $offlineImports.Count 'ConvertFrom-ImportLogText offline count'
Assert-Equal 'Netwtw08.INF' $offlineImports[0].InfName 'ConvertFrom-ImportLogText offline INF'
Assert-Equal 'oem9.inf' $offlineImports[0].OemInfName 'ConvertFrom-ImportLogText offline OEM INF'
Assert-Equal 'netwtw08.inf_amd64_10e2b743974372b6' (Split-Path -Leaf $offlineImports[0].PackageDir) 'ConvertFrom-ImportLogText offline package dir'
Assert-Equal '11/11/2024,23.100.0.4' $offlineImports[0].Version 'ConvertFrom-ImportLogText offline version'
Assert-True ($offlineImports[0].Command -match 'dismhost') 'ConvertFrom-ImportLogText offline command'

$devLogLines = @(
    '     dvs: {Driver Setup Import Driver Package: C:\Windows\TempInst\is-GQ25K.tmp\Source\WLAN_Intel\Netwtw08.INF} 22:41:50.968'
    '     cpy:                     Target Path = C:\Windows\System32\DriverStore\FileRepository\netwtw08.inf_amd64_0e66256da8b619be'
    '     idb:                Created driver package object ''netwtw08.inf_amd64_0e66256da8b619be'' in DRIVERS database node.'
    '     idb:                Registered driver package ''netwtw08.inf_amd64_0e66256da8b619be'' with ''oem96.inf''.'
    '     utl:                Driver Version - 08/14/2022,22.160.0.4'
    '     dvs: {Driver Setup Import Driver Package - exit (0x00000000)} 22:41:53.347'
)
$devImports = @(ConvertFrom-ImportLogText -Lines $devLogLines -LogPath 'C:\Windows\INF\setupapi.dev.log')
Assert-Equal 1 $devImports.Count 'ConvertFrom-ImportLogText dev count'
Assert-Equal 'netwtw08.inf_amd64_0e66256da8b619be' (Split-Path -Leaf $devImports[0].PackageDir) 'ConvertFrom-ImportLogText dev package dir'
Assert-Equal 'oem96.inf' $devImports[0].OemInfName 'ConvertFrom-ImportLogText dev OEM INF'
Assert-Equal '08/14/2022,22.160.0.4' $devImports[0].Version 'ConvertFrom-ImportLogText dev version'

$amdMultiPackageLines = @(
    '     sto: {Setup Import Driver Package: C:\Windows\TempInst\is-HDQ3M.tmp\source\Packages\Drivers\Display\WT6A_INF\U0381698.inf} 21:14:42.632'
    '     inf:      Driver Version: 07/21/2022,30.0.14052.9003'
    '     cpy:           Target Path = C:\Windows\System32\DriverStore\FileRepository\u0381698.inf_amd64_c210a171e1a050ef'
    '     idb:                Created driver package object ''u0381698.inf_amd64_c210a171e1a050ef'' in DRIVERS database node.'
    '     idb:                Created driver INF file object ''oem67.inf'' in DRIVERS database node.'
    '     idb:                Registered driver package ''u0381698.inf_amd64_c210a171e1a050ef'' with ''oem67.inf''.'
    '     cpy:                Target Path = C:\Windows\System32\DriverStore\FileRepository\amdfendr.inf_amd64_3b89fa32321adaa0'
    '     idb:                Created driver package object ''amdfendr.inf_amd64_3b89fa32321adaa0'' in DRIVERS database node.'
    '     idb:                Created driver INF file object ''oem69.inf'' in DRIVERS database node.'
    '     idb:                Registered driver package ''amdfendr.inf_amd64_3b89fa32321adaa0'' with ''oem69.inf''.'
    '     dvs: {Driver Setup Import Driver Package - exit (0x00000000)} 21:14:56.075'
)
$amdMultiPackageImports = @(ConvertFrom-ImportLogText -Lines $amdMultiPackageLines -LogPath 'C:\Windows\INF\setupapi.dev.log')
Assert-Equal 1 $amdMultiPackageImports.Count 'ConvertFrom-ImportLogText AMD CopyINF count'
Assert-Equal 'U0381698.inf' $amdMultiPackageImports[0].InfName 'ConvertFrom-ImportLogText AMD CopyINF INF'
Assert-Equal 'oem67.inf' $amdMultiPackageImports[0].OemInfName 'ConvertFrom-ImportLogText AMD CopyINF OEM'
Assert-Equal 'u0381698.inf_amd64_c210a171e1a050ef' (Split-Path -Leaf $amdMultiPackageImports[0].PackageDir) 'ConvertFrom-ImportLogText AMD CopyINF package'
Assert-Equal '07/21/2022,30.0.14052.9003' $amdMultiPackageImports[0].Version 'ConvertFrom-ImportLogText AMD CopyINF version'

$nvidiaMultiPackageLines = @(
    '     sto: {Setup Import Driver Package: c:\windows\tempinst\is-6uif7.tmp\display.driver\nvlt.inf} 22:37:41.734'
    '     inf:      Driver Version: 11/30/2023,31.0.15.4630'
    '     cpy:           Target Path = C:\Windows\System32\DriverStore\FileRepository\nvlt.inf_amd64_804d30e159655f8c'
    '     cpy:                Target Path = C:\Windows\System32\DriverStore\FileRepository\nvlt.inf_amd64_804d30e159655f8c\NVWMI'
    '     idb:                Created driver package object ''nvlt.inf_amd64_804d30e159655f8c'' in DRIVERS database node.'
    '     idb:                Created driver INF file object ''oem91.inf'' in DRIVERS database node.'
    '     dvs: {Driver Setup Import Driver Package - exit (0x00000000)} 22:39:03.995'
)
$nvidiaMultiPackageImports = @(ConvertFrom-ImportLogText -Lines $nvidiaMultiPackageLines -LogPath 'C:\Windows\INF\setupapi.dev.log')
Assert-Equal 1 $nvidiaMultiPackageImports.Count 'ConvertFrom-ImportLogText NVIDIA CopyINF count'
Assert-Equal 'nvlt.inf' $nvidiaMultiPackageImports[0].InfName 'ConvertFrom-ImportLogText NVIDIA CopyINF INF'
Assert-Equal 'oem91.inf' $nvidiaMultiPackageImports[0].OemInfName 'ConvertFrom-ImportLogText NVIDIA CopyINF OEM'
Assert-Equal 'nvlt.inf_amd64_804d30e159655f8c' (Split-Path -Leaf $nvidiaMultiPackageImports[0].PackageDir) 'ConvertFrom-ImportLogText NVIDIA CopyINF package'
Assert-Equal '11/30/2023,31.0.15.4630' $nvidiaMultiPackageImports[0].Version 'ConvertFrom-ImportLogText NVIDIA CopyINF version'

$fnDriverInstallLines = @(
    '>>>  [Driver Install (DrvSetupInstallDriver) - C:\Windows\TempInst\is-EFU44.tmp\source\LenovoFnAndFunctionKeys.inf]'
    '>>>  Section start 2026/08/24 19:00:07.162'
    '      cmd: "C:\Windows\system32\pnputil.exe" /add-driver C:\Windows\TempInst\is-EFU44.tmp\source\LenovoFnAndFunctionKeys.inf /install'
    '     dvs: {Driver Setup Import Driver Package: C:\Windows\TempInst\is-EFU44.tmp\source\LenovoFnAndFunctionKeys.inf} 19:00:07.162'
    '     sto: {Core Driver Package Import: lenovofnandfunctionkeys.inf_amd64_9bbed958a3d29caf} 19:00:07.344'
    '     idb:                Created driver package object ''lenovofnandfunctionkeys.inf_amd64_9bbed958a3d29caf'' in DRIVERS database node.'
    '     idb:                Created driver INF file object ''oem55.inf'' in DRIVERS database node.'
    '     idb:                Registered driver package ''lenovofnandfunctionkeys.inf_amd64_9bbed958a3d29caf'' with ''oem55.inf''.'
    '     dvs: {Driver Setup Import Driver Package - exit (0x00000000)} 19:00:07.500'
)
$fnDriverInstallImports = @(ConvertFrom-ImportLogText -Lines $fnDriverInstallLines -LogPath 'C:\Windows\INF\setupapi.dev.log')
Assert-Equal 1 $fnDriverInstallImports.Count 'ConvertFrom-ImportLogText Fn Driver Install count'
Assert-Equal 'LenovoFnAndFunctionKeys.inf' $fnDriverInstallImports[0].InfName 'ConvertFrom-ImportLogText Fn Driver Install INF'
Assert-Equal 'oem55.inf' $fnDriverInstallImports[0].OemInfName 'ConvertFrom-ImportLogText Fn Driver Install OEM'
Assert-Equal 'lenovofnandfunctionkeys.inf_amd64_9bbed958a3d29caf' (Split-Path -Leaf $fnDriverInstallImports[0].PackageDir) 'ConvertFrom-ImportLogText Fn Driver Install package'
Assert-True ($fnDriverInstallImports[0].Command -match 'pnputil.exe.*LenovoFnAndFunctionKeys\.inf') 'ConvertFrom-ImportLogText Fn Driver Install command'

$nvDeviceInstallLines = @(
    '>>>  [Device Install (DiInstallDevice) - PCI\VEN_10DE&DEV_2560&SUBSYS_3A8017AA&REV_A1\4&1D71CFEE&0&0009]'
    '>>>  Section start 2026/08/24 22:37:40.247'
    '      cmd: C:\Windows\system32\RunDll32.EXE "C:\Program Files\NVIDIA Corporation\Installer2\CoreTemp.{6285A5D5-8970-44E6-925A-A465803F451B}\NVPrxy64.DLL",Proxy {EED03C53-2CA4-4C4E-B541-C0D0FA00D442} false'
    '     sto: {Setup Import Driver Package: c:\windows\tempinst\is-6uif7.tmp\display.driver\nvlt.inf} 22:37:41.734'
    '     sto: {Core Driver Package Import: nvlt.inf_amd64_804d30e159655f8c} 22:38:55.553'
    '     idb:                Created driver package object ''nvlt.inf_amd64_804d30e159655f8c'' in DRIVERS database node.'
    '     idb:                Created driver INF file object ''oem91.inf'' in DRIVERS database node.'
    '     idb:                Registered driver package ''nvlt.inf_amd64_804d30e159655f8c'' with ''oem91.inf''.'
    '     dvs: {Driver Setup Import Driver Package - exit (0x00000000)} 22:39:04.000'
)
$nvDeviceInstallImports = @(ConvertFrom-ImportLogText -Lines $nvDeviceInstallLines -LogPath 'C:\Windows\INF\setupapi.dev.log')
Assert-Equal 1 $nvDeviceInstallImports.Count 'ConvertFrom-ImportLogText NVIDIA Device Install count'
Assert-Equal 'nvlt.inf' $nvDeviceInstallImports[0].InfName 'ConvertFrom-ImportLogText NVIDIA Device Install INF'
Assert-Equal 'oem91.inf' $nvDeviceInstallImports[0].OemInfName 'ConvertFrom-ImportLogText NVIDIA Device Install OEM'
Assert-Equal 'nvlt.inf_amd64_804d30e159655f8c' (Split-Path -Leaf $nvDeviceInstallImports[0].PackageDir) 'ConvertFrom-ImportLogText NVIDIA Device Install package'
Assert-True ($nvDeviceInstallImports[0].Command -match 'RunDll32\.EXE.*NVPrxy64\.DLL') 'ConvertFrom-ImportLogText NVIDIA Device Install command'

$offlineDevice = [pscustomobject]@{
    DeviceName = 'Intel(R) Wi-Fi 6E AX210'
    DriverVersion = '23.100.0.4'
    DriverDate = '11/11/2024'
    InfName = 'oem9.inf'
    ProviderName = 'Intel'
    InstallDate = '2026-08-21 00:00:00'
    PackageDir = 'C:\Windows\System32\DriverStore\FileRepository\netwtw08.inf_amd64_10e2b743974372b6'
    PackageCreationTime = '2026-08-18 17:11:00'
    PackageDriverVer = '11/11/2024,23.100.0.4'
    ImportSource = 'G:\Win2025\Lenovo\Intel_WiFi_23.90.0\Netwtw08.INF'
    ImportCommand = 'G:\NTLite-Temp\D87ABEB6-E304-4385-BD14-BD481D01517B\dismhost.exe {6488D94D-16D0-4260-A5BB-783D7700AB00}'
    ImportLog = 'C:\Windows\INF\setupapi.offline.log'
    ImportLine = 32215
    ImportKind = 'Offline'
}
$offlineAuditDriver = [pscustomobject]@{ DriverCode = 'WLAN'; DriverName = 'Intel Wireless'; LocalVersion = '23.100.0.4'; OSID = '42' }
$offlineAudit = Resolve-DriverSourceEvidence -Driver $offlineAuditDriver -DeviceEvidence @($offlineDevice) -History @() -AlternateSourceMap @{} -CurrentOsMap @{}
Assert-Equal 'Offline image integration' $offlineAudit.Category 'Resolve-DriverSourceEvidence offline category'
Assert-True ($offlineAudit.Summary -match 'Netwtw08\.INF') 'Resolve-DriverSourceEvidence offline summary'
Assert-True (($offlineAudit.EvidenceLines -join "`n") -match 'Import: source=') 'Resolve-DriverSourceEvidence import evidence'
Assert-Equal 'Local newer (source offline image integration)' (Resolve-DriverSourceLabel -Driver $offlineAuditDriver -History @() -AlternateSourceMap @{} -SourceAudit $offlineAudit) 'Resolve-DriverSourceLabel uses import audit'

$historyFirst = [pscustomobject]@{
    Result = 'Installed'
    Version = '23.100.0.4'
    VerifiedVersion = ''
    BeforeVersion = ''
    DriverCode = 'WLAN'
    DriverName = 'Intel Wireless'
    OSID = '42'
    OSName = 'Windows 10 64-bit'
    Timestamp = '2026-08-24 21:00:00'
}
$historyFirstAudit = Resolve-DriverSourceEvidence -Driver $offlineAuditDriver -DeviceEvidence @($offlineDevice) -History @($historyFirst) -AlternateSourceMap @{} -CurrentOsMap @{}
Assert-Equal 'Install history' $historyFirstAudit.Category 'Resolve-DriverSourceEvidence history beats import evidence'

$auditPlanDriver = [pscustomobject]@{
    DriverName = 'Intel Wireless'
    Version = '22.160.0.4'
    LocalVersion = '23.100.0.4'
    CompareStatus = 'Local newer'
    CompareSource = 'Local newer (source unknown)'
    OsName = 'Windows 10 64-bit'
    OSID = '42'
    SourceApi = 'Web'
    OfficialMd5 = ''
    FileName = 'wlan.exe'
    FilePath = 'https://example.invalid/wlan.exe'
    SourceAudit = $offlineAudit
}
$auditPlanLines = @(Build-PlanText -Drivers @($auditPlanDriver) -GeneratedAt '2026-08-25 12:00:00')
Assert-True (($auditPlanLines -join "`n") -match 'Source audit') 'Build-PlanText source audit'

# Target OS resolution supports an OSID or an OS name without guessing.
$targetOsList = @(
    [pscustomobject]@{ OSID = '42'; OSName = 'Windows 10 64-bit' },
    [pscustomobject]@{ OSID = '248'; OSName = 'Windows 11 64-bit' }
)
Assert-Equal '248' (Resolve-TargetOsEntry -OsList $targetOsList -TargetOS '248').OSID 'Resolve-TargetOsEntry OSID'
Assert-Equal '248' (Resolve-TargetOsEntry -OsList $targetOsList -TargetOS 'Windows 11').OSID 'Resolve-TargetOsEntry OS name'
Assert-Equal '42' (Resolve-TargetOsEntry -OsList $targetOsList -TargetOS 'windows 10').OSID 'Resolve-TargetOsEntry OS name case-insensitive'
Assert-Equal $null (Resolve-TargetOsEntry -OsList $targetOsList -TargetOS 'Windows 12') 'Resolve-TargetOsEntry no match'
Assert-Equal $null (Resolve-TargetOsEntry -OsList $targetOsList -TargetOS '') 'Resolve-TargetOsEntry empty'

# Driver selection keeps a current-OS row when present, and cross-OS mode
# picks the actual newest version after the shell has merged alternate OSes.
$current = [pscustomobject]@{ PartId = 'P1'; DriverName = 'Audio'; OSID = '42'; IssuedDate = [datetime]'2026-01-01'; DriverEditionId = 1; Version = '1.0.0.1' }
$alternate = [pscustomobject]@{ PartId = 'P1'; DriverName = 'Audio'; OSID = '248'; IssuedDate = [datetime]'2026-02-01'; DriverEditionId = 2; Version = '2.0.0.5' }
$selectedCurrent = @(Select-LatestDrivers -Drivers @($current) -CurrentOsId '42')
Assert-Equal 1 $selectedCurrent.Count 'Select-LatestDrivers current OS'
Assert-Equal '42' $selectedCurrent[0].OSID 'Select-LatestDrivers keeps current OS'
$selectedMixed = @(Select-LatestDrivers -Drivers @($alternate, $current) -CurrentOsId '42')
Assert-Equal 1 $selectedMixed.Count 'Select-LatestDrivers mixed count'
Assert-Equal '248' $selectedMixed[0].OSID 'Select-LatestDrivers mixed picks newest version'
$newerDateOlderVersion = [pscustomobject]@{ PartId = 'P2'; DriverName = 'Audio2'; OSID = '248'; IssuedDate = [datetime]'2026-02-01'; DriverEditionId = 2; Version = '1.0.0.1' }
$olderDateNewerVersion = [pscustomobject]@{ PartId = 'P2'; DriverName = 'Audio2'; OSID = '42'; IssuedDate = [datetime]'2026-01-01'; DriverEditionId = 1; Version = '2.0.0.5' }
$selectedVersion = @(Select-LatestDrivers -Drivers @($newerDateOlderVersion, $olderDateNewerVersion) -CurrentOsId '42')
Assert-Equal '42' $selectedVersion[0].OSID 'Select-LatestDrivers version beats issue date'

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
