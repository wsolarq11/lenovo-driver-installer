# Lenovo Driver Installer - Deterministic Core
#
# This file contains only deterministic functions. It must not call the
# network, registry, PnP, file system, console, Read-Host, or process APIs.
# install_lenovo_drivers.ps1 dot-sources this file and supplies every input.

function Get-DriverObjects {
    param(
        $ListData,
        [string]$OsId,
        [string]$SourceApi = 'Web'
    )
    if (-not $ListData) { return @() }

    $partMap = @{}
    $driverRows = @()
    $osName = ''
    $hasQuickFix = $false
    if ($ListData.PSObject.Properties.Name -contains 'Data' -and $ListData.Data) {
        $hasQuickFix = $null -ne $ListData.Data.driverList
    }

    if ($hasQuickFix) {
        $SourceApi = 'QuickFix'
        foreach ($part in @($ListData.Data.partList)) {
            $partMap[[string]$part.PartID] = [string]$part.PartName
        }
        $osNameRow = @($ListData.Data.osList) | Where-Object { [string]$_.OSID -eq [string]$OsId } | Select-Object -First 1
        if ($osNameRow) { $osName = [string]$osNameRow.OSName }
        $driverRows = @($ListData.Data.driverList)
    } else {
        foreach ($part in @($ListData.data.partList)) {
            foreach ($d in @($part.drivelist)) {
                $driverRows += $d
            }
        }
        $osNameRow = @($ListData.data.osList) | Where-Object { [string]$_.OSID -eq [string]$OsId } | Select-Object -First 1
        if ($osNameRow) { $osName = [string]$osNameRow.OSName }
    }

    $result = @()
    foreach ($d in $driverRows) {
        if (-not $d.FileName -or -not $d.FilePath) { continue }
        $partId = [string]$d.PartID
        $partName = [string]$d.PartName
        if (-not $partName -and $partMap.ContainsKey($partId)) { $partName = $partMap[$partId] }

        $edition = 0
        $rawEdition = [string]$d.DriverEdtionId
        if (-not $rawEdition) { $rawEdition = [string]$d.DriverEditionId }
        if ($rawEdition) {
            [void][int64]::TryParse(($rawEdition -replace '[^0-9]', ''), [ref]$edition)
        }

        $issued = [datetime]'1900-01-01'
        $rawIssued = [string]$d.DriverIssuedDateTime
        if (-not $rawIssued) { $rawIssued = [string]$d.PubTime }
        if (-not $rawIssued) { $rawIssued = [string]$d.UpdateTime }
        if ($rawIssued) {
            $dateMatch = [regex]::Match($rawIssued, '\d{4}/\d{1,2}/\d{1,2}')
            if ($dateMatch.Success) {
                $parsedDate = [datetime]'1900-01-01'
                if ([datetime]::TryParseExact($dateMatch.Value, 'yyyy/M/d', [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::None, [ref]$parsedDate)) {
                    $issued = $parsedDate
                }
            }
        }

        $installCode = [string]$d.InstallCode
        if (-not $installCode) { $installCode = [string]$d.Parameter }
        $result += [pscustomobject]@{
            PartId           = $partId
            PartName         = $partName
            DriverName       = [string]$d.DriverName
            DriverCode       = [string]$d.DriverCode
            DriverEditionId  = $edition
            Version          = [string]$d.Version
            FileName         = Split-Path -Leaf ([string]$d.FileName)
            FilePath         = [string]$d.FilePath
            FileSize         = [string]$d.FileSize
            FileType         = [string]$d.FileType
            InstallCode      = $installCode
            InstallParameter = [string]$d.Parameter
            Bootfile         = [string]$d.Bootfile
            HardwareId       = [string]$d.HardwareId
            OfficialMd5      = ([string]$d.MD5).Trim().ToLowerInvariant()
            Status           = ([string]$d.Status).Trim()
            IsEnable         = ([string]$d.IsEnable).Trim()
            IssuedDate       = $issued
            OSID             = $OsId
            OsName           = $osName
            SourceApi        = $SourceApi
            LocalVersion     = ''
            LocalVendor      = ''
            CompareStatus    = ''
            CompareSource    = ''
        }
    }
    return $result
}

function Get-NamePatterns {
    param([string]$DriverName)
    $patterns = @()
    if ($DriverName -match 'Realtek Audio') { $patterns += 'Realtek.*Audio|High Definition Audio' }
    elseif ($DriverName -match 'AMD VGA') { $patterns += 'AMD Radeon|Radeon.*Graphics|AMD.*Display' }
    elseif ($DriverName -match 'NVIDIA VGA') { $patterns += 'NVIDIA GeForce|NVIDIA.*Display' }
    elseif ($DriverName -match 'Realtek Lan') { $patterns += 'Realtek.*Ethernet|Realtek.*PCIe|Realtek.*Gbe' }
    elseif ($DriverName -match 'Wlan') { $patterns += 'Wireless-AC|Wireless LAN|Wi-Fi|WLAN|AX20|8852AE|8822CE|MT7921|MediaTek.*Wi' }
    elseif ($DriverName -match 'BlueTooth') { $patterns += 'Bluetooth' }
    elseif ($DriverName -match 'Cardreader') { $patterns += 'Card Reader|Cardreader' }
    elseif ($DriverName -match 'Camera') { $patterns += 'Camera|Integrated Webcam' }
    elseif ($DriverName -match 'Serial-IO') { $patterns += 'Serial IO|Serial-IO|AMD.*IO' }
    elseif ($DriverName -match 'AMD Power') { $patterns += 'AMD Power|Power Processor' }
    elseif ($DriverName -match 'Lenovo Fn') { $patterns += 'Lenovo Fn|LHK2019' }
    elseif ($DriverName -match 'Lenovo Energy') { $patterns += 'Lenovo Energy|Lenovo Utility' }
    if ($patterns.Count -eq 0) { $patterns += [regex]::Escape(($DriverName -replace ' .*$', '')) }
    return $patterns
}

function Test-HardwareMatch {
    param(
        [string]$RemoteIds,
        [string]$LocalPnpId,
        [string]$LocalDeviceId
    )
    if (-not $RemoteIds) { return $false }
    $localRaw = (($LocalPnpId + ' ' + $LocalDeviceId).ToUpperInvariant())
    $localCompact = $localRaw -replace '[^A-Z0-9]', ''
    foreach ($raw in ($RemoteIds -split ',|;')) {
        $token = $raw.Trim().ToUpperInvariant()
        if (-not $token) { continue }
        if ($token -match '^([0-9A-F]{4})_([0-9A-F]{4})$') {
            $pattern = 'VEN[_]?{0}[&_/]DEV[_]?{1}' -f $matches[1], $matches[2]
            if ($localRaw -match $pattern) { return $true }
        }
        $compact = $token -replace '[^A-Z0-9]', ''
        if ($compact.Length -ge 4 -and ($localRaw.Contains($token) -or $localCompact.Contains($compact))) { return $true }
    }
    return $false
}

function Test-DriverApplicable {
    param(
        [object]$Driver,
        [object[]]$LocalDevices
    )
    $hardwareId = [string]$Driver.HardwareId
    if ($hardwareId) {
        $hwMatches = @($LocalDevices | Where-Object { Test-HardwareMatch $hardwareId $_.PnpDeviceId $_.DeviceId })
        return $hwMatches.Count -gt 0
    }

    $name = $Driver.DriverName
    if ($name -match 'Camera') {
        return @($LocalDevices | Where-Object { $_.Class -in @('Camera', 'Image') -or $_.Name -match 'Camera|Webcam' }).Count -gt 0
    }
    if ($name -match 'Cardreader') {
        return @($LocalDevices | Where-Object { $_.Name -match 'Card Reader|Cardreader|SD|MMC' }).Count -gt 0
    }
    if ($name -match 'Wlan') {
        return @($LocalDevices | Where-Object {
            $_.Class -eq 'Net' -and
            $_.Name -notmatch 'Direct|Virtual' -and
            $_.Name -match 'Intel|Realtek|MediaTek|MTK|Wireless|Wi-Fi|WLAN'
        }).Count -gt 0
    }
    if ($name -match 'BlueTooth') {
        if ($name -match '8852AE') {
            return @($LocalDevices | Where-Object { $_.Class -eq 'Bluetooth' -and $_.Name -match 'Realtek' }).Count -gt 0
        }
        return @($LocalDevices | Where-Object { $_.Class -eq 'Bluetooth' -and $_.Name -match 'Intel|Realtek|MediaTek|MTK|Bluetooth' }).Count -gt 0
    }
    if ($name -match 'Realtek Audio') {
        return @($LocalDevices | Where-Object { $_.Class -in @('MEDIA', 'AudioEndpoint') -and $_.Name -match 'Realtek|Audio' }).Count -gt 0
    }
    if ($name -match 'AMD VGA') {
        return @($LocalDevices | Where-Object { $_.Class -eq 'Display' -and $_.Name -match 'AMD|Radeon' }).Count -gt 0
    }
    if ($name -match 'NVIDIA VGA') {
        return @($LocalDevices | Where-Object { $_.Class -eq 'Display' -and $_.Name -match 'NVIDIA' }).Count -gt 0
    }
    if ($name -match 'Realtek Lan') {
        return @($LocalDevices | Where-Object { $_.Class -eq 'Net' -and $_.Name -match 'Realtek' }).Count -gt 0
    }
    if ($name -match 'Serial-IO') {
        return @($LocalDevices | Where-Object { $_.Name -match 'Serial IO|Serial-IO|I2C|AMD.*IO' }).Count -gt 0
    }
    if ($name -match 'AMD Power') {
        return @($LocalDevices | Where-Object { $_.Name -match 'AMD' }).Count -gt 0
    }
    if ($name -match 'Lenovo Energy|Lenovo Fn|X-Rite') {
        return $true
    }
    return $false
}

function Parse-VersionString {
    param([string]$Value)
    if (-not $Value) { return $null }
    $clean = $Value.Trim()
    if ($clean -match '[/,]') { return $null }
    if ($clean -match '(\d+(\.\d+){1,6})') { $clean = $matches[1] }
    $version = $null
    if ([version]::TryParse($clean, [ref]$version)) { return $version }
    return $null
}

function Get-DeviceVendor {
    param([object[]]$Names)
    foreach ($name in $Names) {
        $value = [string]$name
        if ($value -match 'Intel|\u82f1\u7279\u5c14') { return 'Intel' }
        if ($value -match 'Realtek') { return 'Realtek' }
        if ($value -match 'MediaTek|MTK|MT79') { return 'MediaTek' }
        if ($value -match 'AMD|Radeon') { return 'AMD' }
        if ($value -match 'NVIDIA') { return 'NVIDIA' }
        if ($value -match 'Sonix') { return 'Sonix' }
        if ($value -match 'Sunplus') { return 'Sunplus' }
    }
    return ''
}

function Get-RemoteComponentVendor {
    param([string]$Component)
    if ($Component -match 'Intel') { return 'Intel' }
    if ($Component -match 'Realtek') { return 'Realtek' }
    if ($Component -match 'MediaTek|MTK|MT79') { return 'MediaTek' }
    if ($Component -match 'AMD|Radeon') { return 'AMD' }
    if ($Component -match 'NVIDIA') { return 'NVIDIA' }
    if ($Component -match 'Sonix') { return 'Sonix' }
    if ($Component -match 'Sunplus') { return 'Sunplus' }
    return ''
}

function Get-MatchingRemoteComponent {
    param(
        [string]$Remote,
        [string]$Vendor
    )
    if (-not $Vendor -or $Remote -notmatch '[/,]') { return $null }
    foreach ($part in ($Remote -split '/|,')) {
        $partVendor = Get-RemoteComponentVendor $part
        if ($partVendor -and $partVendor -eq $Vendor) {
            $version = Parse-VersionString $part
            if ($version) { return $version }
        }
    }
    return $null
}

function Compare-DriverStatus {
    param(
        [string]$Remote,
        [string]$Local,
        [string]$Vendor = ''
    )
    if (-not $Local) { return 'Not installed' }
    if (-not $Remote) { return 'Unknown' }
    if ($Local -eq 'Provisioned') { return 'Up to date' }
    $remoteVersion = Get-MatchingRemoteComponent -Remote $Remote -Vendor $Vendor
    if (-not $remoteVersion) { $remoteVersion = Parse-VersionString $Remote }
    $localVersion = Parse-VersionString $Local
    if ($remoteVersion -and $localVersion) {
        if ($localVersion -gt $remoteVersion) { return 'Local newer' }
        if ($localVersion -eq $remoteVersion) { return 'Up to date' }
        return 'Update'
    }
    return 'Unknown'
}

function Get-DisplayWidth {
    param([string]$Text)
    $width = 0
    foreach ($ch in $Text.ToCharArray()) {
        $code = [int]$ch
        if (
            ($code -ge 0x1100 -and $code -le 0x115F) -or
            ($code -ge 0x2E80 -and $code -le 0x303E) -or
            ($code -ge 0x3041 -and $code -le 0x33FF) -or
            ($code -ge 0x3400 -and $code -le 0x4DBF) -or
            ($code -ge 0x4E00 -and $code -le 0x9FFF) -or
            ($code -ge 0xA000 -and $code -le 0xA4CF) -or
            ($code -ge 0xAC00 -and $code -le 0xD7A3) -or
            ($code -ge 0xF900 -and $code -le 0xFAFF) -or
            ($code -ge 0xFE30 -and $code -le 0xFE4F) -or
            ($code -ge 0xFF00 -and $code -le 0xFF60) -or
            ($code -ge 0xFFE0 -and $code -le 0xFFE6)
        ) {
            $width += 2
        } else {
            $width += 1
        }
    }
    return $width
}

function Get-TruncatedText {
    param(
        [string]$Text,
        [int]$MaxWidth
    )
    if (-not $Text) { return '' }
    if ((Get-DisplayWidth $Text) -le $MaxWidth) { return $Text }
    $result = ''
    $used = 0
    foreach ($ch in $Text.ToCharArray()) {
        $charWidth = Get-DisplayWidth ([string]$ch)
        if ($used + $charWidth + 3 -gt $MaxWidth) { return $result + '...' }
        $result += $ch
        $used += $charWidth
    }
    return $result + '...'
}

function Format-Cell {
    param(
        [string]$Text,
        [int]$Width,
        [string]$Align = 'Left'
    )
    $text = [string]$Text
    $displayWidth = Get-DisplayWidth $text
    $pad = $Width - $displayWidth
    if ($pad -lt 0) { $pad = 0 }
    if ($Align -eq 'Right') { return ((' ' * $pad) + $text) }
    return ($text + (' ' * $pad))
}

function Select-LatestDrivers {
    param(
        [object[]]$Drivers,
        [string]$CurrentOsId
    )
    $selected = @()
    $groups = $Drivers | Group-Object @{ Expression = { "$($_.PartId)`t$($_.DriverName)" } }
    foreach ($g in $groups) {
        $groupDrivers = @($g.Group)
        $hasCurrent = @($groupDrivers | Where-Object { $_.OSID -eq $CurrentOsId }).Count -gt 0
        if (-not $hasCurrent) { continue }
        $best = $groupDrivers |
            Sort-Object `
                @{ Expression = { $_.IssuedDate }; Descending = $true },
                @{ Expression = { $_.DriverEditionId }; Descending = $true } |
            Select-Object -First 1
        $selected += $best
    }
    return $selected
}

function ConvertTo-Bytes {
    param([string]$SizeText)
    if (-not $SizeText) { return 0 }
    $match = [regex]::Match($SizeText.Trim(), '^([0-9.]+)\s*(B|KB|MB|GB)?$', 'IgnoreCase')
    if (-not $match.Success) {
        $plain = 0L
        if ([int64]::TryParse(($SizeText -replace '[^0-9]', ''), [ref]$plain)) { return $plain }
        return 0
    }
    $value = [double]$match.Groups[1].Value
    switch ($match.Groups[2].Value.ToUpperInvariant()) {
        'KB' { $value *= 1KB }
        'MB' { $value *= 1MB }
        'GB' { $value *= 1GB }
        default { }
    }
    return [int64]$value
}

function Get-SizeTolerance {
    param([int64]$ExpectedBytes)
    return [int64][math]::Max(1024, [double]($ExpectedBytes * 0.02))
}

function Test-FileSizeMatch {
    param(
        [int64]$ExpectedBytes,
        [int64]$ActualBytes
    )
    if ($ExpectedBytes -le 0) { return $true }
    return [math]::Abs($ActualBytes - $ExpectedBytes) -le (Get-SizeTolerance -ExpectedBytes $ExpectedBytes)
}

function Test-RebootExitCode {
    param([int]$ExitCode)
    return ($ExitCode -eq 3010 -or $ExitCode -eq 1641)
}

function Get-VersionMatchKeys {
    param([string]$Text)
    $keys = @()
    $clean = [string]$Text
    if (-not $clean) { return @() }
    if ($clean -match '[/,]') {
        foreach ($part in ($clean -split '/|,')) {
            $version = Parse-VersionString $part
            if ($version) { $keys += $version.ToString() }
        }
    } else {
        $version = Parse-VersionString $clean
        if ($version) { $keys += $version.ToString() }
    }
    return @($keys | Select-Object -Unique)
}

function Resolve-InstalledSoftwareVersion {
    param(
        [string]$DriverName,
        [object]$SoftwareSnapshot
    )
    if (-not $SoftwareSnapshot) { return '' }
    $patterns = @()
    if ($DriverName -match 'Lenovo Fn') { $patterns += 'Lenovo.*Fn|Lenovo.*Hotkey|Lenovo Utility|Hotkeys' }
    elseif ($DriverName -match 'Energy Management') { $patterns += 'Lenovo Energy|Lenovo.*Power|Energy Management' }
    elseif ($DriverName -match 'X-Rite') { $patterns += 'X-Rite|Color Assistant' }
    elseif ($DriverName -match 'AMD Power Processor') { $patterns += 'AMD Power Processor|AMD Power' }

    foreach ($pattern in $patterns) {
        $app = @($SoftwareSnapshot.InstalledApps) | Where-Object { ([string]$_.DisplayName) -match $pattern } | Select-Object -First 1
        if ($app -and $app.DisplayVersion) { return [string]$app.DisplayVersion }
    }

    if ($DriverName -match 'AMD Power Processor' -and [string]$SoftwareSnapshot.ProvisionedAmdPower -eq 'Provisioned') {
        return 'Provisioned'
    }
    if ($DriverName -match 'Lenovo Fn' -and $SoftwareSnapshot.LenovoFnServiceVersion) {
        return [string]$SoftwareSnapshot.LenovoFnServiceVersion
    }
    return ''
}

function Get-MatchingLocalDevices {
    param(
        [object]$Driver,
        [object[]]$LocalDevices
    )
    $matchedDevices = @()
    if ($Driver.HardwareId) {
        $matchedDevices = @($LocalDevices | Where-Object { Test-HardwareMatch $Driver.HardwareId $_.PnpDeviceId $_.DeviceId })
    }
    if ($matchedDevices.Count -eq 0) {
        $patterns = Get-NamePatterns -DriverName $Driver.DriverName
        foreach ($pattern in $patterns) {
            $matchedDevices = @($LocalDevices | Where-Object { $_.Name -match $pattern -and $_.Name -notmatch 'Direct|Virtual' -and $_.Class -ne 'SoftwareDevice' })
            if ($matchedDevices.Count -gt 0) { break }
        }
    }
    return @($matchedDevices)
}

function Resolve-LocalDriverVersion {
    param(
        [object]$Driver,
        [object[]]$MatchedDevices,
        [string[]]$DriverVersions = @(),
        [object]$SoftwareSnapshot
    )
    $localVersion = ''
    $localVendor = ''
    if ($MatchedDevices.Count -gt 0) {
        $localVendor = Get-DeviceVendor -Names @($MatchedDevices | ForEach-Object { $_.Name })
        $uniqueVersions = @($DriverVersions | Where-Object { $_ } | Sort-Object -Unique)
        if ($uniqueVersions.Count -eq 1) { $localVersion = $uniqueVersions[0] }
        if ($uniqueVersions.Count -gt 1) { $localVersion = ($uniqueVersions -join ', ') }
    }
    if (-not $localVersion) {
        $localVersion = Resolve-InstalledSoftwareVersion -DriverName $Driver.DriverName -SoftwareSnapshot $SoftwareSnapshot
    }
    return [pscustomobject]@{
        LocalVersion = $localVersion
        LocalVendor  = $localVendor
    }
}

function Resolve-ExternalDriverSourceLabel {
    param(
        [object]$Driver,
        [hashtable]$AlternateSourceMap
    )
    if (-not $AlternateSourceMap -or $AlternateSourceMap.Count -eq 0) {
        return 'Local newer (source unknown)'
    }
    $match = $null
    foreach ($versionKey in @(Get-VersionMatchKeys $Driver.LocalVersion)) {
        $nameKey = '{0}|{1}' -f ([string]$Driver.DriverName).Trim(), $versionKey
        $codeKey = '{0}|{1}' -f $Driver.DriverCode, $versionKey
        $match = $AlternateSourceMap[$nameKey]
        if (-not $match) { $match = $AlternateSourceMap[$codeKey] }
        if ($match) { break }
    }
    if ($match) {
        if ($match.OSName) { return ("Local newer (source {0})" -f $match.OSName) }
        return ("Local newer (source OSID {0})" -f $match.OSID)
    }
    return 'Local newer (source unknown)'
}

function Resolve-DriverSourceLabel {
    param(
        [object]$Driver,
        [object[]]$History,
        [hashtable]$AlternateSourceMap
    )
    $latest = @($History |
        Where-Object {
            $_.Result -eq 'Installed' -and
            $_.Version -eq $Driver.LocalVersion -and
            (($_.DriverCode -eq $Driver.DriverCode) -or ($_.DriverName -eq $Driver.DriverName))
        } |
        Sort-Object Timestamp -Descending |
        Select-Object -First 1)
    if ($latest) {
        $sourceOsId = [string]$latest.OSID
        $sourceOsName = [string]$latest.OSName
        if ($sourceOsId -and $sourceOsId -ne [string]$Driver.OSID) {
            if ($sourceOsName) { return ("Local newer (source {0})" -f $sourceOsName) }
            return ("Local newer (source OSID {0})" -f $sourceOsId)
        }
        if ($sourceOsId -eq [string]$Driver.OSID) { return 'Local newer (same current OS source)' }
    }
    return Resolve-ExternalDriverSourceLabel -Driver $Driver -AlternateSourceMap $AlternateSourceMap
}

function Build-PlanText {
    param(
        [object[]]$Drivers,
        [string]$GeneratedAt
    )
    $lines = @()
    $lines += 'Lenovo driver plan'
    $lines += ('Generated: {0}' -f $GeneratedAt)
    $lines += ''
    $index = 0
    foreach ($d in $Drivers) {
        $index++
        $local = if ($d.LocalVersion) { $d.LocalVersion } else { 'not detected' }
        $lines += ('[{0}] {1}' -f $index, $d.DriverName)
        $lines += ('  Remote : {0}' -f $d.Version)
        $lines += ('  Local  : {0}' -f $local)
        $lines += ('  Status : {0}' -f $d.CompareStatus)
        if ($d.CompareSource) { $lines += ('  Source note : {0}' -f $d.CompareSource) }
        $lines += ('  OS     : {0} (OSID {1})' -f $d.OsName, $d.OSID)
        $lines += ('  Source : {0}' -f $d.SourceApi)
        $lines += ('  MD5    : {0}' -f $(if ($d.OfficialMd5) { $d.OfficialMd5 } else { 'not provided' }))
        $lines += ('  File   : {0}' -f $d.FileName)
        $lines += ('  URL    : {0}' -f $d.FilePath)
        $lines += ''
    }
    return @($lines)
}

function Format-DriverTableLines {
    param([object[]]$Drivers)
    $indexWidth = 3
    $driverWidth = 42
    $remoteWidth = 36
    $localWidth = 20
    $statusWidth = 14
    $lines = @()
    $header = ('{0} {1} {2} {3} {4}' -f (Format-Cell '#' $indexWidth 'Right'), (Format-Cell 'Driver' $driverWidth), (Format-Cell 'Remote' $remoteWidth), (Format-Cell 'Local' $localWidth), (Format-Cell 'Status' $statusWidth))
    $separator = ('{0} {1} {2} {3} {4}' -f (Format-Cell '---' $indexWidth 'Right'), (Format-Cell '------' $driverWidth), (Format-Cell '------' $remoteWidth), (Format-Cell '-----' $localWidth), (Format-Cell '------' $statusWidth))
    $lines += ''
    $lines += $header
    $lines += $separator
    $index = 0
    foreach ($d in $Drivers) {
        $index++
        $line = ('{0} {1} {2} {3} {4}' -f (Format-Cell ([string]$index) $indexWidth 'Right'), (Format-Cell (Get-TruncatedText $d.DriverName $driverWidth) $driverWidth), (Format-Cell (Get-TruncatedText $d.Version $remoteWidth) $remoteWidth), (Format-Cell (Get-TruncatedText $d.LocalVersion $localWidth) $localWidth), (Format-Cell $d.CompareStatus $statusWidth))
        $lines += $line
    }
    $lines += ''
    return @($lines)
}

function Format-StatusSummaryLines {
    param([object[]]$Drivers)
    $update = @($Drivers | Where-Object { $_.CompareStatus -eq 'Update' }).Count
    $same = @($Drivers | Where-Object { $_.CompareStatus -eq 'Up to date' }).Count
    $missing = @($Drivers | Where-Object { $_.CompareStatus -eq 'Not installed' }).Count
    $localNewer = @($Drivers | Where-Object { $_.CompareStatus -eq 'Local newer' }).Count
    $unknown = @($Drivers | Where-Object { $_.CompareStatus -eq 'Unknown' }).Count
    $notApplicable = @($Drivers | Where-Object { $_.CompareStatus -eq 'Not applicable' }).Count
    $lines = @()
    $lines += ''
    $lines += ('  Update         : {0}' -f $update)
    $lines += ('  Up to date     : {0}' -f $same)
    $lines += ('  Not installed  : {0}' -f $missing)
    $lines += ('  Local newer    : {0}' -f $localNewer)
    $lines += ('  Unknown        : {0}' -f $unknown)
    $lines += ('  Not applicable : {0}' -f $notApplicable)
    if ($unknown -gt 0) {
        $lines += '  Note: Unknown = multi-vendor package, no matching local component found.'
    }
    if ($notApplicable -gt 0) {
        $lines += '  Note: Not applicable = hardware not detected on this machine.'
    }
    if ($localNewer -gt 0) {
        $sourceNotes = @($Drivers |
            Where-Object { $_.CompareStatus -eq 'Local newer' } |
            Select-Object -First 5 |
            ForEach-Object {
                $note = if ($_.CompareSource) { $_.CompareSource } else { 'Local newer (source unknown)' }
                '{0}: {1}' -f $_.DriverName, $note
            })
        foreach ($note in $sourceNotes) {
            $lines += ('  Note: {0}' -f $note)
        }
    }
    $lines += ''
    return @($lines)
}

function Parse-DriverSelectionTokens {
    param(
        [string]$InputText,
        [object[]]$AllSelected
    )
    $selected = @()
    $invalid = @()
    $notApplicable = @()
    $seen = @{}
    foreach ($token in ($InputText -split '[,; ]')) {
        if (-not $token) { continue }
        $number = 0
        if (-not [int]::TryParse($token, [ref]$number)) {
            $invalid += $token
            continue
        }
        $index = $number - 1
        if ($index -lt 0 -or $index -ge $AllSelected.Count) {
            $invalid += $number
            continue
        }
        if ($AllSelected[$index].CompareStatus -eq 'Not applicable') {
            $notApplicable += $number
            continue
        }
        if ($seen.ContainsKey($index)) { continue }
        $seen[$index] = $true
        $selected += $AllSelected[$index]
    }
    return [pscustomobject]@{
        Selected      = @($selected)
        Invalid       = @($invalid)
        NotApplicable = @($notApplicable)
    }
}

function Build-DriverHistoryRecord {
    param(
        [object]$Driver,
        [string]$Result,
        [string]$Message = '',
        [string]$Timestamp
    )
    return [ordered]@{
        Timestamp  = $Timestamp
        DriverCode = [string]$Driver.DriverCode
        OSID       = [string]$Driver.OSID
        OSName     = [string]$Driver.OsName
        DriverName = [string]$Driver.DriverName
        Version    = [string]$Driver.Version
        FileName   = [string]$Driver.FileName
        MD5        = [string]$Driver.OfficialMd5
        Source     = [string]$Driver.SourceApi
        Result     = $Result
        Message    = $Message
    }
}

