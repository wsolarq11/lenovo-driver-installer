param(
    [switch]$DryRun,
    [switch]$IncludeBios,
    [switch]$LatestAcrossOS,
    [switch]$CurrentOSOnly,
    [switch]$DownloadOnly,
    [switch]$SkipHashCheck,
    [switch]$Elevated,
    [switch]$Help,
    [string]$Model = '',
    [string]$DownloadDir = ''
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch {}

try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
} catch {}

$ApiBase = 'https://newsupport.lenovo.com.cn/api'
$StartTime = Get-Date
$LogPath = Join-Path $env:TEMP 'lenovo_driver_install.log'
$PlanPath = Join-Path $env:TEMP 'lenovo_driver_plan.txt'

function Write-Log {
    param(
        [string]$Message,
        [string]$Level = 'INFO'
    )
    $ts = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'
    $line = "[$ts] [$Level] $Message"
    try {
        Add-Content -LiteralPath $LogPath -Value $line -Encoding UTF8
    } catch {}
    Write-Host $line
}

function Test-Admin {
    try {
        $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
        $principal = New-Object Security.Principal.WindowsPrincipal($identity)
        return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    } catch {
        return $false
    }
}

function Invoke-LenovoApi {
    param([string]$RelativeUrl)
    $headers = @{
        'User-Agent' = 'Mozilla/5.0'
        'Referer'    = 'https://newsupport.lenovo.com.cn/driveDownloads_index.html'
    }
    $response = Invoke-RestMethod -Uri ($ApiBase + $RelativeUrl) -Headers $headers -TimeoutSec 30
    if ([string]$response.statusCode -ne '200') {
        throw "Lenovo API returned $($response.statusCode) for $RelativeUrl : $($response.message)"
    }
    return $response
}

function Get-MachineInfo {
    $cs = $null
    $bios = $null
    try { $cs = Get-CimInstance Win32_ComputerSystem } catch { $cs = Get-WmiObject Win32_ComputerSystem }
    try { $bios = Get-CimInstance Win32_BIOS } catch { $bios = Get-WmiObject Win32_BIOS }
    $resolvedModel = if ($Model) { $Model } elseif ($cs) { [string]$cs.Model } else { '' }
    $serial = if ($bios) { [string]$bios.SerialNumber } else { '' }
    return [pscustomobject]@{
        Model  = $resolvedModel
        Serial = $serial
    }
}

function Get-OSInfo {
    $os = $null
    try { $os = Get-CimInstance Win32_OperatingSystem } catch { $os = Get-WmiObject Win32_OperatingSystem }
    $caption = if ($os) { [string]$os.Caption } else { '' }
    $osArch = if ($os) { [string]$os.OSArchitecture } else { $env:PROCESSOR_ARCHITECTURE }
    $osKind = 'Windows'
    if ($caption -match 'Windows 11') { $osKind = 'Windows 11' }
    elseif ($caption -match 'Windows 10') { $osKind = 'Windows 10' }
    elseif ($caption -match 'Windows 8') { $osKind = 'Windows 8' }
    elseif ($caption -match 'Windows 7') { $osKind = 'Windows 7' }
    $bits = if ($osArch -match '64') { '64-bit' } else { '32-bit' }
    return [pscustomobject]@{
        Caption = $caption
        Kind    = $osKind
        OsName  = "$osKind $bits"
        Arch    = $bits
    }
}

function Resolve-LenovoCategoryId {
    param(
        [string]$MachineModel,
        [string]$Serial
    )
    $keys = @()
    if ($MachineModel) { $keys += $MachineModel }
    if ($Serial -and $Serial -notmatch 'To be filled|None|Default') { $keys += $Serial }
    foreach ($key in $keys) {
        try {
            $url = '/drive/drive_query?searchKey=' + [uri]::EscapeDataString($key)
            $res = Invoke-LenovoApi $url
            $items = @($res.data)
            if ($items.Count -gt 0) {
                $cat = [string]$items[0].categoryid
                if ($cat) { return $cat }
            }
        } catch {
            Write-Log ("Machine lookup failed for '{0}': {1}" -f $key, $_.Exception.Message) 'WARN'
        }
    }
    return ''
}

function Get-DriverObjects {
    param(
        $ListData,
        [string]$OsId
    )
    $result = @()
    foreach ($part in @($ListData.data.partList)) {
        foreach ($d in @($part.drivelist)) {
            if (-not $d.FileName -or -not $d.FilePath) { continue }
            $edition = 0
            $rawEdition = [string]$d.DriverEdtionId
            if ($rawEdition) {
                [void][int64]::TryParse(($rawEdition -replace '[^0-9]', ''), [ref]$edition)
            }
            $issued = [datetime]'1900-01-01'
            $rawIssued = [string]$d.DriverIssuedDateTime
            if ($rawIssued -match '\d{4}/\d{1,2}/\d{1,2}') {
                try {
                    $issued = [datetime]::ParseExact($matches[0], 'yyyy/M/d', [Globalization.CultureInfo]::InvariantCulture)
                } catch {}
            }
            $result += [pscustomobject]@{
                PartId           = [string]$part.PartID
                PartName         = [string]$part.PartName
                DriverName       = [string]$d.DriverName
                DriverCode       = [string]$d.DriverCode
                DriverEditionId  = $edition
                Version          = [string]$d.Version
                FileName         = Split-Path -Leaf ([string]$d.FileName)
                FilePath         = [string]$d.FilePath
                FileSize         = [string]$d.FileSize
                FileType         = [string]$d.FileType
                InstallCode      = [string]$d.InstallCode
                HardwareId       = [string]$d.HardwareId
                Status           = ([string]$d.Status).Trim()
                IsEnable         = ([string]$d.IsEnable).Trim()
                IssuedDate       = $issued
                OSID             = $OsId
                LocalVersion     = ''
                LocalVendor      = ''
                CompareStatus    = ''
            }
        }
    }
    return $result
}

function Get-RefreshedDriverUrl {
    param(
        [object]$Driver,
        [string]$CategoryId,
        [string]$SysId,
        [object[]]$OsList,
        [bool]$EnableLatest
    )
    $queries = @(
        [pscustomobject]@{
            Query = "/drive/drive_listnew?searchKey=$CategoryId&sysid=$SysId"
            OsId  = $SysId
        }
    )
    if ($EnableLatest) {
        foreach ($alt in @($OsList)) {
            $altId = [string]$alt.OSID
            if ($altId -eq $SysId) { continue }
            $queries += [pscustomobject]@{
                Query = "/drive/drive_listnew?searchKey=$CategoryId&sysid=" + [uri]::EscapeDataString($altId)
                OsId  = $altId
            }
        }
    }
    foreach ($item in $queries) {
        try {
            $data = Invoke-LenovoApi $item.Query
            $candidates = @(Get-DriverObjects $data $item.OsId)
            $match = $candidates | Where-Object { $_.DriverCode -eq $Driver.DriverCode } | Select-Object -First 1
            if ($match -and $match.FilePath) { return $match.FilePath }
        } catch {
            Write-Log ("Could not refresh driver URL for {0}: {1}" -f $Driver.DriverCode, $_.Exception.Message) 'WARN'
        }
    }
    return ''
}

function Get-LocalDeviceSnapshot {
    $devices = @()
    try {
        $entities = @(Get-CimInstance Win32_PnPEntity -ErrorAction Stop)
    } catch {
        $entities = @(Get-WmiObject Win32_PnPEntity -ErrorAction Stop)
    }
    foreach ($e in $entities) {
        $present = $true
        try { $present = [bool]$e.Present } catch {}
        if (-not $present) { continue }
        $devices += [pscustomobject]@{
            Name        = [string]$e.Name
            Class       = [string]$e.PNPClass
            DeviceId    = [string]$e.DeviceID
            PnpDeviceId = [string]$e.PNPDeviceID
        }
    }
    return $devices
}

function Get-InstalledApps {
    $paths = @(
        'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*',
        'HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*',
        'HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*'
    )
    $apps = @()
    foreach ($path in $paths) {
        try { $apps += @(Get-ItemProperty -Path $path -ErrorAction Stop) } catch {}
    }
    return $apps
}

function Get-InstalledSoftwareVersion {
    param(
        [string]$DriverName,
        [object[]]$InstalledApps
    )
    $patterns = @()
    if ($DriverName -match 'Lenovo Fn') { $patterns += 'Lenovo.*Fn|Lenovo.*Hotkey|Lenovo Utility|Hotkeys' }
    elseif ($DriverName -match 'Energy Management') { $patterns += 'Lenovo Energy|Lenovo.*Power|Energy Management' }
    elseif ($DriverName -match 'X-Rite') { $patterns += 'X-Rite|Color Assistant' }
    elseif ($DriverName -match 'AMD Power Processor') { $patterns += 'AMD Power Processor|AMD Power' }

    foreach ($pattern in $patterns) {
        $app = $InstalledApps | Where-Object { ([string]$_.DisplayName) -match $pattern } | Select-Object -First 1
        if ($app -and $app.DisplayVersion) { return [string]$app.DisplayVersion }
    }

    if ($DriverName -match 'AMD Power Processor') {
        try {
            $provisioned = Get-ChildItem 'HKLM:\SOFTWARE\Microsoft\Provisioning\Results' -ErrorAction Stop |
                ForEach-Object { Get-ItemProperty $_.PSPath -ErrorAction SilentlyContinue } |
                Where-Object { $_.PackageFileName -eq 'AMD.Power.Processor.ppkg' } |
                Select-Object -First 1
            if ($provisioned) { return 'Provisioned' }
        } catch {}
    }

    if ($DriverName -match 'Lenovo Fn') {
        try {
            $svc = Get-CimInstance Win32_Service -Filter "Name='LenovoFnAndFunctionKeys'" -ErrorAction SilentlyContinue
            if ($svc) {
                $exePath = ([string]$svc.PathName).Trim('"')
                if ($exePath -and (Test-Path -LiteralPath $exePath)) {
                    $fileVersion = (Get-Item -LiteralPath $exePath).VersionInfo.FileVersion
                    if ($fileVersion) { return [string]$fileVersion }
                }
            }
        } catch {}
    }
    return ''
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

function Get-LocalDriverVersion {
    param(
        [object]$Driver,
        [object[]]$LocalDevices,
        [object[]]$InstalledApps
    )
    $matches = @()
    if ($Driver.HardwareId) {
        $matches = @($LocalDevices | Where-Object { Test-HardwareMatch $Driver.HardwareId $_.PnpDeviceId $_.DeviceId })
    }
    if ($matches.Count -eq 0) {
        $patterns = Get-NamePatterns -DriverName $Driver.DriverName
        foreach ($pattern in $patterns) {
            $matches = @($LocalDevices | Where-Object { $_.Name -match $pattern -and $_.Name -notmatch 'Direct|Virtual' -and $_.Class -ne 'SoftwareDevice' })
            if ($matches.Count -gt 0) { break }
        }
    }
    if ($matches.Count -gt 0) {
        $versions = @()
        foreach ($matchedDevice in $matches) {
            try {
                $versionProp = Get-PnpDeviceProperty -InstanceId $matchedDevice.PnpDeviceId -KeyName 'DEVPKEY_Device_DriverVersion' -ErrorAction Stop
                if ($versionProp.Data) { $versions += [string]$versionProp.Data }
            } catch {}
        }
        $Driver.LocalVendor = Get-DeviceVendor -Names @($matches | ForEach-Object { $_.Name })
        $uniqueVersions = @($versions | Sort-Object -Unique)
        if ($uniqueVersions.Count -eq 1) { return $uniqueVersions[0] }
        if ($uniqueVersions.Count -gt 1) { return ($uniqueVersions -join ', ') }
    }
    return Get-InstalledSoftwareVersion -DriverName $Driver.DriverName -InstalledApps $InstalledApps
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

function Show-DriverTable {
    param([object[]]$Drivers)
    $indexWidth = 3
    $driverWidth = 42
    $remoteWidth = 36
    $localWidth = 20
    $statusWidth = 14

    $header = '{0} {1} {2} {3} {4}' -f `
        (Format-Cell '#' $indexWidth 'Right'), `
        (Format-Cell 'Driver' $driverWidth), `
        (Format-Cell 'Remote' $remoteWidth), `
        (Format-Cell 'Local' $localWidth), `
        (Format-Cell 'Status' $statusWidth)
    $separator = '{0} {1} {2} {3} {4}' -f `
        (Format-Cell '---' $indexWidth 'Right'), `
        (Format-Cell '------' $driverWidth), `
        (Format-Cell '------' $remoteWidth), `
        (Format-Cell '-----' $localWidth), `
        (Format-Cell '------' $statusWidth)

    Write-Host ''
    Write-Host $header -ForegroundColor Cyan
    Write-Host $separator -ForegroundColor DarkGray

    $index = 0
    foreach ($d in $Drivers) {
        $index++
        $line = '{0} {1} {2} {3} {4}' -f `
            (Format-Cell ([string]$index) $indexWidth 'Right'), `
            (Format-Cell (Get-TruncatedText $d.DriverName $driverWidth) $driverWidth), `
            (Format-Cell (Get-TruncatedText $d.Version $remoteWidth) $remoteWidth), `
            (Format-Cell (Get-TruncatedText $d.LocalVersion $localWidth) $localWidth), `
            (Format-Cell $d.CompareStatus $statusWidth)
        Write-Host $line
    }
    Write-Host ''
}

function Show-StatusSummary {
    param([object[]]$Drivers)
    $update = @($Drivers | Where-Object { $_.CompareStatus -eq 'Update' }).Count
    $same = @($Drivers | Where-Object { $_.CompareStatus -eq 'Up to date' }).Count
    $missing = @($Drivers | Where-Object { $_.CompareStatus -eq 'Not installed' }).Count
    $localNewer = @($Drivers | Where-Object { $_.CompareStatus -eq 'Local newer' }).Count
    $unknown = @($Drivers | Where-Object { $_.CompareStatus -eq 'Unknown' }).Count
    $notApplicable = @($Drivers | Where-Object { $_.CompareStatus -eq 'Not applicable' }).Count
    Write-Host ''
    Write-Host ('  Update         : {0}' -f $update) -ForegroundColor Yellow
    Write-Host ('  Up to date     : {0}' -f $same) -ForegroundColor Green
    Write-Host ('  Not installed  : {0}' -f $missing) -ForegroundColor Cyan
    Write-Host ('  Local newer    : {0}' -f $localNewer) -ForegroundColor Red
    Write-Host ('  Unknown        : {0}' -f $unknown) -ForegroundColor DarkGray
    Write-Host ('  Not applicable : {0}' -f $notApplicable) -ForegroundColor Magenta
    if ($unknown -gt 0) {
        Write-Host '  Note: Unknown = multi-vendor package, no matching local component found.' -ForegroundColor DarkGray
    }
    if ($notApplicable -gt 0) {
        Write-Host '  Note: Not applicable = hardware not detected on this machine.' -ForegroundColor DarkGray
    }
    Write-Host ''
}

function Write-PlanFile {
    param(
        [object[]]$Drivers,
        [string]$Path
    )
    $lines = @()
    $lines += 'Lenovo driver plan'
    $lines += ('Generated: {0}' -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'))
    $lines += ''
    $index = 0
    foreach ($d in $Drivers) {
        $index++
        $local = if ($d.LocalVersion) { $d.LocalVersion } else { 'not detected' }
        $lines += ('[{0}] {1}' -f $index, $d.DriverName)
        $lines += ('  Remote : {0}' -f $d.Version)
        $lines += ('  Local  : {0}' -f $local)
        $lines += ('  Status : {0}' -f $d.CompareStatus)
        $lines += ('  File   : {0}' -f $d.FileName)
        $lines += ('  URL    : {0}' -f $d.FilePath)
        $lines += ''
    }
    $lines | Set-Content -LiteralPath $Path -Encoding UTF8
    return $Path
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

function Get-FileSha256 {
    param([string]$Path)
    try {
        $hash = Get-FileHash -LiteralPath $Path -Algorithm SHA256 -ErrorAction Stop
        return $hash.Hash.ToLowerInvariant()
    } catch {
        return ''
    }
}

function Write-HashCompanion {
    param(
        [string]$FilePath,
        [string]$Hash
    )
    $companion = $FilePath + '.sha256'
    $temp = $companion + '.tmp'
    $fileName = Split-Path -Leaf $FilePath
    try {
        Set-Content -LiteralPath $temp -Value ("SHA256 {0} {1}" -f $Hash.ToLowerInvariant(), $fileName) -Encoding ASCII -NoNewline
        Move-Item -LiteralPath $temp -Destination $companion -Force
    } catch {
        Remove-Item -LiteralPath $temp -Force -ErrorAction SilentlyContinue
        throw
    }
}

function Test-HashCompanion {
    param([string]$FilePath)
    if ($SkipHashCheck) { return $true }
    $companion = $FilePath + '.sha256'
    if (-not (Test-Path -LiteralPath $companion)) { return $false }
    $lines = @(Get-Content -LiteralPath $companion -ErrorAction SilentlyContinue)
    if ($lines.Count -ne 1) { return $false }
    $fields = $lines[0].Trim() -split '\s+'
    if ($fields.Count -lt 2 -or $fields[0] -ne 'SHA256') { return $false }
    $expected = $fields[1].ToLowerInvariant()
    $actual = Get-FileSha256 $FilePath
    return $actual -eq $expected
}

function Invoke-DownloadWithRetry {
    param(
        [string]$Url,
        [string]$OutFile,
        [int]$MaxAttempts = 3
    )
    for ($attempt = 1; $attempt -le $MaxAttempts; $attempt++) {
        try {
            Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing -TimeoutSec 180
            if ((Get-Item -LiteralPath $OutFile -ErrorAction Stop).Length -gt 0) { return $true }
            throw 'Downloaded file is empty.'
        } catch {
            if ($attempt -lt $MaxAttempts) {
                Write-Log ("Download attempt {0} failed: {1}. Retrying..." -f $attempt, $_.Exception.Message) 'WARN'
                Start-Sleep -Seconds 3
            } else {
                throw
            }
        }
    }
    return $false
}

function Invoke-ProcessWithTimeout {
    param(
        [string]$FilePath,
        [string[]]$ArgumentList,
        [int]$TimeoutSeconds,
        [string]$WorkingDirectory = ''
    )
    $startArgs = @{
        FilePath = $FilePath
        PassThru = $true
    }
    if ($ArgumentList -and $ArgumentList.Count -gt 0) { $startArgs.ArgumentList = $ArgumentList }
    if ($WorkingDirectory) { $startArgs.WorkingDirectory = $WorkingDirectory }
    $process = Start-Process @startArgs
    if (-not $process.WaitForExit($TimeoutSeconds * 1000)) {
        & taskkill /PID $process.Id /T /F 2>&1 | Out-Null
        return [pscustomobject]@{ ExitCode = -1; TimedOut = $true }
    }
    return [pscustomobject]@{ ExitCode = $process.ExitCode; TimedOut = $false }
}

function Invoke-PnPUtilWithTimeout {
    param(
        [string]$InfPath,
        [string]$WorkingDir
    )
    $baseName = [System.IO.Path]::GetFileNameWithoutExtension($InfPath)
    $stdout = Join-Path $WorkingDir ("pnputil.{0}.out.log" -f $baseName)
    $stderr = Join-Path $WorkingDir ("pnputil.{0}.err.log" -f $baseName)
    Remove-Item -LiteralPath $stdout, $stderr -Force -ErrorAction SilentlyContinue
    $process = Start-Process -FilePath 'pnputil.exe' -ArgumentList @('/add-driver', ('"' + $InfPath + '"'), '/install') -RedirectStandardOutput $stdout -RedirectStandardError $stderr -PassThru
    if (-not $process.WaitForExit(900000)) {
        & taskkill /PID $process.Id /T /F 2>&1 | Out-Null
        return [pscustomobject]@{ ExitCode = -1; TimedOut = $true }
    }
    if (Test-Path -LiteralPath $stdout) {
        Get-Content -LiteralPath $stdout | ForEach-Object { Write-Log $_ 'INSTALL' }
    }
    if (Test-Path -LiteralPath $stderr) {
        Get-Content -LiteralPath $stderr | ForEach-Object { Write-Log $_ 'INSTALL' }
    }
    if ($process.ExitCode -eq 1) {
        Write-Log ("pnputil exited 1; reboot may be required: {0}" -f $InfPath) 'WARN'
        return [pscustomobject]@{ ExitCode = 0; TimedOut = $false }
    }
    return [pscustomobject]@{ ExitCode = $process.ExitCode; TimedOut = $false }
}

function Get-InnoTempRoots {
    $roots = @(
        (Join-Path $env:SystemRoot 'TempInst'),
        (Join-Path $env:TEMP 'TempInst'),
        $env:TEMP
    ) | Select-Object -Unique
    $dirs = @()
    foreach ($root in $roots) {
        if (-not $root -or -not (Test-Path -LiteralPath $root)) { continue }
        $dirs += @(Get-ChildItem -LiteralPath $root -Directory -Filter 'is-*.tmp' -ErrorAction SilentlyContinue)
    }
    return @($dirs | Sort-Object LastWriteTime -Descending)
}

function Get-TempDirFromLog {
    param([string]$LogPath)
    if (-not $LogPath -or -not (Test-Path -LiteralPath $LogPath)) { return '' }
    $text = Get-Content -LiteralPath $LogPath -Raw -ErrorAction SilentlyContinue
    if (-not $text) { return '' }
    $found = [regex]::Matches($text, '[A-Za-z]:\\[^"\r\n]*is-[A-Za-z0-9]+\.tmp')
    if ($found.Count -eq 0) { return '' }
    $dir = $found[$found.Count - 1].Value.TrimEnd('\')
    if (Test-Path -LiteralPath $dir) { return $dir }
    return ''
}

function Find-ExtractedInfs {
    param([string]$LogPath)
    $dir = Get-TempDirFromLog -LogPath $LogPath
    if (-not $dir) { return @() }
    return @(Get-ChildItem -LiteralPath $dir -Filter *.inf -File -Recurse -ErrorAction SilentlyContinue)
}

function Find-InnerSetup {
    param(
        [object]$Driver,
        [string]$WorkingDir
    )
    $log = Join-Path $WorkingDir ($Driver.DriverCode + '.log')
    $logDir = Get-TempDirFromLog -LogPath $log
    if ($logDir) {
        foreach ($name in @('setup.exe', 'nvsetup.exe')) {
            $candidate = Join-Path $logDir $name
            if (Test-Path -LiteralPath $candidate) { return $candidate }
        }
    }
    if (([string]$Driver.DriverName) -notmatch 'NVIDIA') { return '' }
    $cutoff = (Get-Date).AddHours(-2)
    foreach ($tempDir in @(Get-InnoTempRoots)) {
        if ($tempDir.LastWriteTime -lt $cutoff) { continue }
        if (-not (Test-Path -LiteralPath (Join-Path $tempDir.FullName 'Display.Driver'))) { continue }
        foreach ($name in @('setup.exe', 'nvsetup.exe')) {
            $candidate = Join-Path $tempDir.FullName $name
            if (Test-Path -LiteralPath $candidate) { return $candidate }
        }
    }
    return ''
}

function Invoke-ExtractedDriverFallback {
    param(
        [object]$Driver,
        [string]$WorkingDir
    )
    $log = Join-Path $WorkingDir ($Driver.DriverCode + '.log')
    $infs = @(Find-ExtractedInfs -LogPath $log)
    if ($infs.Count -gt 0) {
        Write-Log ("[{0}] Found {1} extracted driver INF(s); using pnputil fallback." -f $Driver.DriverCode, $infs.Count) 'WARN'
        $exitCode = 0
        foreach ($inf in $infs) {
            $result = Invoke-PnPUtilWithTimeout -InfPath $inf.FullName -WorkingDir $WorkingDir
            if ($result.TimedOut) {
                Write-Log ("[{0}] pnputil fallback timed out." -f $Driver.DriverCode) 'ERROR'
                return [pscustomobject]@{ Used = $true; ExitCode = -1 }
            }
            if ($result.ExitCode -ne 0) { $exitCode = $result.ExitCode }
        }
        return [pscustomobject]@{ Used = $true; ExitCode = $exitCode }
    }
    $setupPath = Find-InnerSetup -Driver $Driver -WorkingDir $WorkingDir
    if ($setupPath) {
        Write-Log ("[{0}] Found inner installer {1}; retrying silently." -f $Driver.DriverCode, $setupPath) 'WARN'
        $args = @()
        $installCode = [string]$Driver.InstallCode
        if ($installCode -and $installCode -notmatch '^/add-driver') {
            $args = @($installCode -split '\s+' | Where-Object { $_ })
        }
        if ($args.Count -eq 0) {
            $args = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART')
        }
        $result = Invoke-ProcessWithTimeout -FilePath $setupPath -ArgumentList $args -TimeoutSeconds 900 -WorkingDirectory (Split-Path -Parent $setupPath)
        if ($result.TimedOut) {
            Write-Log ("[{0}] Inner installer timed out and its process tree was killed." -f $Driver.DriverCode) 'ERROR'
            return [pscustomobject]@{ Used = $true; ExitCode = -1 }
        }
        if ($result.ExitCode -eq 3010 -or $result.ExitCode -eq 1641) {
            Write-Log ("[{0}] Inner installer succeeded; reboot may be required (exit {1})." -f $Driver.DriverCode, $result.ExitCode) 'WARN'
            return [pscustomobject]@{ Used = $true; ExitCode = 0 }
        }
        if ($result.ExitCode -ne 0) {
            Write-Log ("[{0}] Inner installer exit {1}." -f $Driver.DriverCode, $result.ExitCode) 'WARN'
        }
        return [pscustomobject]@{ Used = $true; ExitCode = $result.ExitCode }
    }
    return [pscustomobject]@{ Used = $false; ExitCode = -1 }
}

function Install-DriverFile {
    param(
        [string]$FilePath,
        [object]$Driver,
        [string]$WorkingDir
    )
    $ext = [System.IO.Path]::GetExtension($FilePath).ToLowerInvariant()
    switch ($ext) {
        '.msi' {
            $result = Invoke-ProcessWithTimeout -FilePath 'msiexec.exe' -ArgumentList @('/i', ('"' + $FilePath + '"'), '/qn', '/norestart') -TimeoutSeconds 1800 -WorkingDirectory $WorkingDir
            if ($result.TimedOut) {
                Write-Log "MSI timed out and its process tree was killed: $FilePath" 'ERROR'
                return -1
            }
            if ($result.ExitCode -eq 3010 -or $result.ExitCode -eq 1641) {
                Write-Log ("MSI install succeeded; reboot may be required (exit {0})." -f $result.ExitCode) 'WARN'
                return 0
            }
            return $result.ExitCode
        }
        '.inf' {
            $result = Invoke-PnPUtilWithTimeout -InfPath $FilePath -WorkingDir $WorkingDir
            if ($result.TimedOut) {
                Write-Log "pnputil timed out and its process tree was killed: $FilePath" 'ERROR'
                return -1
            }
            return $result.ExitCode
        }
        '.zip' {
            $extract = Join-Path $WorkingDir ([System.IO.Path]::GetFileNameWithoutExtension($FilePath))
            Expand-Archive -LiteralPath $FilePath -DestinationPath $extract -Force
            $infs = @(Get-ChildItem -LiteralPath $extract -Filter *.inf -Recurse -ErrorAction SilentlyContinue)
            if ($infs.Count -eq 0) {
                Write-Log "No .inf found inside $FilePath" 'WARN'
                return 2
            }
            $exitCode = 0
            foreach ($inf in $infs) {
                $result = Invoke-PnPUtilWithTimeout -InfPath $inf.FullName -WorkingDir $WorkingDir
                if ($result.TimedOut) { return -1 }
                if ($result.ExitCode -ne 0) { $exitCode = $result.ExitCode }
            }
            return $exitCode
        }
        '.cab' {
            $extract = Join-Path $WorkingDir ([System.IO.Path]::GetFileNameWithoutExtension($FilePath))
            New-Item -ItemType Directory -Force -Path $extract | Out-Null
            $expandResult = Invoke-ProcessWithTimeout -FilePath 'expand.exe' -ArgumentList @(('"' + $FilePath + '"'), '-F:*', ('"' + $extract + '"')) -TimeoutSeconds 900 -WorkingDirectory $WorkingDir
            if ($expandResult.TimedOut) {
                Write-Log "expand.exe timed out and its process tree was killed: $FilePath" 'ERROR'
                return -1
            }
            if ($expandResult.ExitCode -ne 0) {
                Write-Log ("expand.exe exited {0}: {1}" -f $expandResult.ExitCode, $FilePath) 'ERROR'
                return $expandResult.ExitCode
            }
            $infs = @(Get-ChildItem -LiteralPath $extract -Filter *.inf -Recurse -ErrorAction SilentlyContinue)
            if ($infs.Count -eq 0) {
                Write-Log "No .inf found inside $FilePath" 'WARN'
                return 2
            }
            $exitCode = 0
            foreach ($inf in $infs) {
                $result = Invoke-PnPUtilWithTimeout -InfPath $inf.FullName -WorkingDir $WorkingDir
                if ($result.TimedOut) { return -1 }
                if ($result.ExitCode -ne 0) { $exitCode = $result.ExitCode }
            }
            return $exitCode
        }
        '.exe' {
            $log = Join-Path $WorkingDir ($Driver.DriverCode + '.log')
            $args = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', ('/LOG="' + $log + '"'))
            $installCode = [string]$Driver.InstallCode
            if ($installCode -and $installCode -notmatch '^/add-driver') {
                $codeArgs = @($installCode -split '\s+' | Where-Object { $_ })
                if ($codeArgs.Count -gt 0) {
                    $args += $codeArgs
                    Write-Log ("[{0}] Using Lenovo InstallCode arguments: {1}" -f $Driver.DriverCode, ($codeArgs -join ' ')) 'INFO'
                }
            }
            $result = Invoke-ProcessWithTimeout -FilePath $FilePath -ArgumentList $args -TimeoutSeconds 900 -WorkingDirectory $WorkingDir
            if ($result.TimedOut) {
                Write-Log ("[{0}] EXE timed out and its process tree was killed; checking extracted installer fallback." -f $Driver.DriverCode) 'WARN'
                $fallback = Invoke-ExtractedDriverFallback -Driver $Driver -WorkingDir $WorkingDir
                if ($fallback.Used -and $fallback.ExitCode -eq 0) { return 0 }
                if ($fallback.Used) {
                    Write-Log ("[{0}] Extracted installer fallback exit {1}." -f $Driver.DriverCode, $fallback.ExitCode) 'ERROR'
                    return $fallback.ExitCode
                }
                Write-Log ("[{0}] EXE timed out and no extracted installer fallback was available." -f $Driver.DriverCode) 'ERROR'
                return -1
            }
            if ($result.ExitCode -eq 3010 -or $result.ExitCode -eq 1641) {
                Write-Log ("[{0}] Install succeeded; reboot may be required (exit {1})." -f $Driver.DriverCode, $result.ExitCode) 'WARN'
                return 0
            }
            if ($result.ExitCode -ne 0) {
                Write-Log ("[{0}] Silent install exit {1}. Installer log: {2}" -f $Driver.DriverCode, $result.ExitCode, $log) 'WARN'
                $fallback = Invoke-ExtractedDriverFallback -Driver $Driver -WorkingDir $WorkingDir
                if ($fallback.Used -and $fallback.ExitCode -eq 0) { return 0 }
                if ($fallback.Used) {
                    Write-Log ("[{0}] Extracted installer fallback exit {1}." -f $Driver.DriverCode, $fallback.ExitCode) 'WARN'
                    return $fallback.ExitCode
                }
                $answer = Read-Host ("Type r to run {0} interactively, or s to skip" -f $Driver.FileName)
                $answerText = if ($null -eq $answer) { '' } else { [string]$answer }
                if ($answerText.Trim().ToLowerInvariant() -eq 'r') {
                    $p = Start-Process -FilePath $FilePath -Wait -PassThru
                    return $p.ExitCode
                }
                if ($answerText.Trim() -eq '') {
                    Write-Log ("[{0}] No interactive fallback answer was received; skipping." -f $Driver.DriverCode) 'WARN'
                }
                return $result.ExitCode
            }
            return 0
        }
        default {
            $result = Invoke-ProcessWithTimeout -FilePath $FilePath -ArgumentList @() -TimeoutSeconds 1800 -WorkingDirectory $WorkingDir
            if ($result.TimedOut) {
                Write-Log ("Installer timed out and its process tree was killed: {0}" -f $FilePath) 'ERROR'
                return -1
            }
            return $result.ExitCode
        }
    }
}

function Show-Help {
    $help = @'
Lenovo Driver Installer

Usage:
  install_lenovo_drivers.bat [-DryRun] [-CurrentOSOnly] [-LatestAcrossOS] [-SkipHashCheck] [-IncludeBios] [-DownloadOnly] [-DownloadDir <path>] [-Model <model>] [-Help]

Options:
  -DryRun        Compare versions without downloading or installing.
  -CurrentOSOnly Use only the current OS driver list (default).
  -LatestAcrossOS Allow newer drivers from other OS entries. Cannot be used with -CurrentOSOnly.
  -SkipHashCheck Skip local SHA-256 cache validation.
  -IncludeBios   Include BIOS/EC packages.
  -DownloadOnly  Download only; do not install.
  -DownloadDir   Download directory.
  -Model         Override machine model lookup.
  -Elevated      Skip elevation; used internally.
  -Help          Show this help.

Interactive choices:
  y = install update-only drivers
  a = install all applicable drivers
  s = select driver numbers manually, e.g. 1,3,5
  n = cancel

Plan file:
  %TEMP%\lenovo_driver_plan.txt
'@
    Write-Host $help
}

function Read-DriverSelection {
    param([object[]]$AllSelected)
    $answer = Read-Host 'Enter driver numbers, e.g. 1,3,5'
    $selected = @()
    $seen = @{}
    foreach ($token in ($answer -split '[,; ]')) {
        if (-not $token) { continue }
        $number = 0
        if (-not [int]::TryParse($token, [ref]$number)) {
            Write-Log ("Invalid driver number: {0}" -f $token) 'WARN'
            continue
        }
        $index = $number - 1
        if ($index -lt 0 -or $index -ge $AllSelected.Count) {
            Write-Log ("Invalid driver number: {0}" -f $number) 'WARN'
            continue
        }
        if ($AllSelected[$index].CompareStatus -eq 'Not applicable') {
            Write-Log ("Driver {0} is not applicable and was skipped." -f $number) 'WARN'
            continue
        }
        if ($seen.ContainsKey($index)) { continue }
        $seen[$index] = $true
        $selected += $AllSelected[$index]
    }
    if ($selected.Count -eq 0) {
        Write-Log 'No valid driver numbers were selected.'
        return @()
    }
    return @($selected)
}

function Select-InteractiveDrivers {
    param(
        [object[]]$AllSelected,
        [object[]]$UpdateDrivers,
        [object[]]$AllApplicable
    )
    if ($UpdateDrivers.Count -gt 0) {
        Write-Host ("Ready: {0} update-only drivers, {1} all applicable drivers." -f $UpdateDrivers.Count, $AllApplicable.Count) -ForegroundColor Yellow
        $answer = Read-Host 'Type y to install updates, a to install all applicable, s to select, n to cancel'
        $choice = $answer.Trim().ToLowerInvariant()
        if ($choice -eq 'y') { return @($UpdateDrivers) }
        if ($choice -eq 'a') { return @($AllApplicable) }
        if ($choice -eq 's') {
            $manual = @(Read-DriverSelection -AllSelected $AllSelected)
            if ($manual.Count -eq 0) { Write-Log 'No drivers selected.'; return $null }
            return $manual
        }
        return $null
    }
    Write-Host ("No clear updates detected. {0} applicable candidates remain." -f $AllApplicable.Count) -ForegroundColor Yellow
    $answer = Read-Host 'Type a to install all applicable, s to select, n to cancel'
    $choice = $answer.Trim().ToLowerInvariant()
    if ($choice -eq 'a') { return @($AllApplicable) }
    if ($choice -eq 's') {
        $manual = @(Read-DriverSelection -AllSelected $AllSelected)
        if ($manual.Count -eq 0) { Write-Log 'No drivers selected.'; return $null }
        return $manual
    }
    return $null
}

if ($Help) {
    Show-Help
    exit 0
}

if ($CurrentOSOnly -and $LatestAcrossOS) {
    Write-Host 'Error: -CurrentOSOnly and -LatestAcrossOS cannot be used together.' -ForegroundColor Red
    exit 2
}

Write-Log '=== Lenovo driver update started ==='

$isAdmin = Test-Admin
if (-not $isAdmin -and -not $Elevated -and -not $DryRun -and -not $DownloadOnly) {
    $argList = @(
        '-NoProfile',
        '-ExecutionPolicy', 'Bypass',
        '-File', ('"' + $PSCommandPath + '"'),
        '-Elevated'
    )
    foreach ($key in $PSBoundParameters.Keys) {
        if ($key -eq 'Elevated') { continue }
        $value = $PSBoundParameters[$key]
        if ($value -is [switch]) {
            if ($value) { $argList += '-' + $key }
        } else {
            $argList += '-' + $key
            $argList += '"' + [string]$value + '"'
        }
    }
    Write-Log 'Requesting administrator privileges...' 'WARN'
    $p = Start-Process -FilePath (Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe') -Verb RunAs -ArgumentList $argList -Wait -PassThru
    exit $p.ExitCode
}

if (-not $DryRun -and -not $DownloadOnly -and -not $isAdmin) {
    throw 'This script must run as administrator. Use install_lenovo_drivers.bat, which requests elevation.'
}

$machine = Get-MachineInfo
$osInfo = Get-OSInfo
Write-Log ("Machine model : {0}" -f $machine.Model)
Write-Log ("Serial number : {0}" -f $machine.Serial)
Write-Log ("System        : {0}" -f $osInfo.Caption)
Write-Log ("OS match key  : {0}" -f $osInfo.OsName)

$categoryId = Resolve-LenovoCategoryId -MachineModel $machine.Model -Serial $machine.Serial
if (-not $categoryId) {
    throw 'Could not resolve the Lenovo machine category. Use -Model "82JQ" if the automatic lookup fails.'
}
Write-Log ("Lenovo category ID : {0}" -f $categoryId)

$listData = Invoke-LenovoApi "/drive/drive_listnew?searchKey=$categoryId"
$osList = @($listData.data.osList)
$osEntry = $null
if ($osList.Count -gt 0) {
    $osEntry = $osList | Where-Object { ([string]$_.OSName) -like "*$($osInfo.OsName)*" } | Select-Object -First 1
    if (-not $osEntry) {
        $osEntry = $osList | Where-Object { ([string]$_.OSName) -match [regex]::Escape($osInfo.Kind) } | Select-Object -First 1
    }
    if (-not $osEntry) {
        $osEntry = $osList | Where-Object { [string]$_.OSID -eq [string]$listData.data.localOSID } | Select-Object -First 1
    }
}
if (-not $osEntry) {
    throw 'Could not resolve the current OS entry from Lenovo. Use -LatestAcrossOS only after confirming the OS list is complete.'
}
$sysId = [string]$osEntry.OSID
Write-Log ("Matched OS entry : {0} (OSID {1})" -f $osEntry.OSName, $sysId)

$query = "/drive/drive_listnew?searchKey=$categoryId"
if ($sysId) {
    $query += '&sysid=' + [uri]::EscapeDataString($sysId)
}
$osListData = Invoke-LenovoApi $query
$drivers = @(Get-DriverObjects $osListData $sysId)

$enableLatest = $LatestAcrossOS
if ($enableLatest) {
    Write-Log 'Cross-OS latest mode enabled: checking other OS entries for newer versions of the same driver.'
    foreach ($alt in $osList) {
        $altId = [string]$alt.OSID
        if ($altId -eq $sysId) { continue }
        try {
            $altQuery = "/drive/drive_listnew?searchKey=$categoryId&sysid=" + [uri]::EscapeDataString($altId)
            $altData = Invoke-LenovoApi $altQuery
            $drivers += Get-DriverObjects $altData $altId
        } catch {
            Write-Log ("Could not load alternate OS entry {0}: {1}" -f $altId, $_.Exception.Message) 'WARN'
        }
    }
} else {
    Write-Log 'Current OS mode enabled (default). Use -LatestAcrossOS to compare newer drivers from other OS entries.'
}

$drivers = @($drivers | Where-Object {
    ($_.Status -eq '' -or $_.Status -ne '0') -and
    ($_.IsEnable -eq '' -or $_.IsEnable -ne '0')
})

if (-not $IncludeBios) {
    $beforeBios = $drivers.Count
    $drivers = @($drivers | Where-Object { $_.DriverName -notmatch 'BIOS|EC Version' })
    if ($drivers.Count -lt $beforeBios) {
        Write-Log 'BIOS/EC package skipped by default. Use -IncludeBios to install it.' 'WARN'
    }
}

$installableExts = @('.exe', '.msi', '.zip', '.inf', '.cab')
$beforeExt = $drivers.Count
$drivers = @($drivers | Where-Object {
    $installableExts -contains ([System.IO.Path]::GetExtension($_.FileName).ToLowerInvariant())
})
if ($drivers.Count -lt $beforeExt) {
    Write-Log 'Non-installable files (readme/text/etc.) skipped.' 'WARN'
}

$selected = @(Select-LatestDrivers -Drivers $drivers -CurrentOsId $sysId)
Write-Log ("Drivers selected : {0}" -f $selected.Count)
Write-Log 'Comparing with locally installed versions...'

$localDevices = @(Get-LocalDeviceSnapshot)
$installedApps = @(Get-InstalledApps)
foreach ($driver in $selected) {
    if (-not (Test-DriverApplicable -Driver $driver -LocalDevices $localDevices)) {
        $driver.CompareStatus = 'Not applicable'
        continue
    }
    $driver.LocalVersion = Get-LocalDriverVersion -Driver $driver -LocalDevices $localDevices -InstalledApps $installedApps
    $driver.CompareStatus = Compare-DriverStatus -Remote $driver.Version -Local $driver.LocalVersion -Vendor $driver.LocalVendor
}

$allSelected = $selected
try {
    Write-PlanFile -Drivers $allSelected -Path $PlanPath | Out-Null
    Write-Log ("Plan file : {0}" -f $PlanPath)
} catch {
    Write-Log ("Could not write plan file: {0}" -f $_.Exception.Message) 'WARN'
}
Write-Host ''
Write-Host 'Driver version comparison:' -ForegroundColor Cyan
Show-DriverTable -Drivers $allSelected
Show-StatusSummary -Drivers $allSelected

$allApplicable = @($allSelected | Where-Object { $_.CompareStatus -ne 'Not applicable' })
$updateDrivers = @($allApplicable | Where-Object { $_.CompareStatus -eq 'Update' })
Write-Log ("Applicable candidates : {0}; update-only drivers : {1}" -f $allApplicable.Count, $updateDrivers.Count)
if ($updateDrivers.Count -eq 0 -and -not $LatestAcrossOS) {
    Write-Log 'No current-OS updates detected. Use -LatestAcrossOS on a new run to compare newer drivers from other OS entries.' 'WARN'
}

if ($allApplicable.Count -eq 0) {
    Write-Log 'No installable drivers found.'
    exit 0
}

if ($DryRun) {
    Write-Log 'Dry run finished. No files were downloaded or installed.'
    exit 0
}

$selectionResult = Select-InteractiveDrivers -AllSelected $allSelected -UpdateDrivers $updateDrivers -AllApplicable $allApplicable
if ($null -eq $selectionResult) {
    Write-Log 'No drivers were selected.'
    exit 0
}
$selected = @($selectionResult)
if ($selected.Count -eq 0) {
    Write-Log 'No drivers selected for installation.'
    exit 0
}
Write-Log ("Selected for installation : {0}" -f $selected.Count)

$dlDir = if ($DownloadDir) { $DownloadDir } else { Join-Path $env:TEMP 'LenovoDrivers' }
New-Item -ItemType Directory -Force -Path $dlDir | Out-Null
Write-Log ("Download directory : {0}" -f $dlDir)

$success = @()
$failed = @()

foreach ($driver in $selected) {
    $outFile = Join-Path $dlDir ("{0}_{1}" -f $driver.DriverCode, $driver.FileName)
    $expectedSize = ConvertTo-Bytes $driver.FileSize
    Write-Log ("[{0}] Downloading {1}" -f $driver.DriverCode, $driver.FileName)
    $downloadError = $null
    $downloadAttempt = 0
    while ($true) {
        $downloadAttempt++
        try {
            $needsDownload = $true
            if (Test-Path -LiteralPath $outFile) {
                $existingSize = (Get-Item -LiteralPath $outFile).Length
                if ($expectedSize -gt 0) {
                    $tolerance = [int64][math]::Max(1024, [double]($expectedSize * 0.02))
                    if ([math]::Abs($existingSize - $expectedSize) -le $tolerance) {
                        $needsDownload = $false
                    } else {
                        Write-Log ("[{0}] Cached file size mismatch, redownloading." -f $driver.DriverCode) 'WARN'
                        Remove-Item -LiteralPath $outFile -Force
                        Remove-Item -LiteralPath ($outFile + '.sha256') -Force -ErrorAction SilentlyContinue
                    }
                } elseif ($existingSize -gt 0) {
                    $needsDownload = $false
                }
                if ($needsDownload -eq $false -and -not (Test-HashCompanion $outFile)) {
                    Write-Log ("[{0}] Cached file hash missing or mismatch, redownloading." -f $driver.DriverCode) 'WARN'
                    Remove-Item -LiteralPath $outFile -Force
                    Remove-Item -LiteralPath ($outFile + '.sha256') -Force -ErrorAction SilentlyContinue
                    $needsDownload = $true
                }
                if ($needsDownload -eq $false) {
                    Write-Log ("[{0}] Using verified cached file ({1} bytes)." -f $driver.DriverCode, $existingSize)
                }
            }
            if ($needsDownload) {
                $null = Invoke-DownloadWithRetry -Url $driver.FilePath -OutFile $outFile
            }
            if ($expectedSize -gt 0) {
                $downloadedSize = (Get-Item -LiteralPath $outFile).Length
                $tolerance = [int64][math]::Max(1024, [double]($expectedSize * 0.02))
                if ([math]::Abs($downloadedSize - $expectedSize) -gt $tolerance) {
                    throw "Downloaded size mismatch: expected $expectedSize, got $downloadedSize"
                }
            }
            if ($needsDownload -and -not $SkipHashCheck) {
                $downloadedHash = Get-FileSha256 $outFile
                if (-not $downloadedHash) { throw 'Could not compute SHA-256 for downloaded file.' }
                Remove-Item -LiteralPath ($outFile + '.sha256') -Force -ErrorAction SilentlyContinue
                Write-HashCompanion -FilePath $outFile -Hash $downloadedHash
            }
            $downloadError = $null
            break
        } catch {
            $downloadError = $_
            if ($downloadAttempt -ge 2) { break }
            if ($_.Exception.Message -notmatch '403|Forbidden') { break }
            $refreshedUrl = Get-RefreshedDriverUrl -Driver $driver -CategoryId $categoryId -SysId $sysId -OsList $osList -EnableLatest $enableLatest
            if (-not $refreshedUrl) {
                Write-Log ("[{0}] Could not refresh the expired download URL." -f $driver.DriverCode) 'WARN'
                break
            }
            Write-Log ("[{0}] Download URL expired; refreshing and retrying." -f $driver.DriverCode) 'WARN'
            $driver.FilePath = $refreshedUrl
            Remove-Item -LiteralPath $outFile -Force -ErrorAction SilentlyContinue
            Remove-Item -LiteralPath ($outFile + '.sha256') -Force -ErrorAction SilentlyContinue
        }
    }
    if ($downloadError) {
        Remove-Item -LiteralPath $outFile -Force -ErrorAction SilentlyContinue
        Remove-Item -LiteralPath ($outFile + '.sha256') -Force -ErrorAction SilentlyContinue
        Write-Log ("[{0}] Download failed: {1}" -f $driver.DriverCode, $downloadError.Exception.Message) 'ERROR'
        $failed += $driver
        continue
    }

    if ($DownloadOnly) {
        Write-Log ("[{0}] Downloaded only." -f $driver.DriverCode)
        $success += $driver
        continue
    }

    Write-Log ("[{0}] Installing {1}" -f $driver.DriverCode, $driver.FileName)
    try {
        $code = Install-DriverFile -FilePath $outFile -Driver $driver -WorkingDir $dlDir
        if ($code -eq 0) {
            Write-Log ("[{0}] Install success." -f $driver.DriverCode)
            $success += $driver
        } else {
            Write-Log ("[{0}] Install exit code {1}." -f $driver.DriverCode, $code) 'ERROR'
            $failed += $driver
        }
    } catch {
        Write-Log ("[{0}] Install failed: {1}" -f $driver.DriverCode, $_.Exception.Message) 'ERROR'
        $failed += $driver
    }
}

if ($success.Count -gt 0 -and -not $DownloadOnly) {
    Write-Log 'Running post-install verification pass...'
    try {
        $localDevices = @(Get-LocalDeviceSnapshot)
        $installedApps = @(Get-InstalledApps)
        foreach ($driver in $success) {
            $beforeLocal = $driver.LocalVersion
            $afterLocal = Get-LocalDriverVersion -Driver $driver -LocalDevices $localDevices -InstalledApps $installedApps
            if ($afterLocal) {
                if ($afterLocal -eq $beforeLocal) {
                    Write-Log ("[{0}] Recheck: unchanged ({1}); reboot may be needed." -f $driver.DriverCode, $afterLocal) 'WARN'
                } else {
                    Write-Log ("[{0}] Recheck: {1} -> {2}" -f $driver.DriverCode, $(if ($beforeLocal) { $beforeLocal } else { 'not detected' }), $afterLocal)
                }
            } else {
                Write-Log ("[{0}] Recheck: version not detectable yet." -f $driver.DriverCode) 'WARN'
            }
        }
    } catch {
        Write-Log ("Post-install verification failed: {0}" -f $_.Exception.Message) 'ERROR'
    }
}

$elapsed = [math]::Round(((Get-Date) - $StartTime).TotalMinutes, 2)
Write-Host ''
Write-Log ("Finished: success={0}, failed={1}, elapsed={2} min" -f $success.Count, $failed.Count, $elapsed)
Write-Log "Log file: $LogPath"

if ($failed.Count -gt 0) {
    exit 1
}
exit 0
