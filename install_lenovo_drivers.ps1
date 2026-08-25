<#
.SYNOPSIS
Detects, compares, downloads, and installs applicable Lenovo drivers for the current machine.

.DESCRIPTION
Queries the official Lenovo driver API, matches drivers against locally installed versions,
and installs selected drivers. Current-OS-only comparison is the default; use -LatestAcrossOS
to opt into newer drivers from other OS entries.

.PARAMETER DryRun
Compare versions and write a plan without downloading or installing files.

.PARAMETER IncludeBios
Include BIOS/EC packages. They are skipped by default.

.PARAMETER LatestAcrossOS
Allow newer versions of a driver from other OS entries. Cannot be combined with -CurrentOSOnly.

.PARAMETER CurrentOSOnly
Use only the current OS driver list. This is the default and exists for explicit callers.

.PARAMETER DownloadOnly
Download applicable files without installing them.

.PARAMETER SkipHashCheck
Skip SHA-256 companion-file validation for cached or fresh downloads.

.PARAMETER Elevated
Skip elevation. Used internally by the .bat wrapper.

.PARAMETER Help
Show usage help.

.PARAMETER Model
Override the automatic Lenovo machine model lookup.

.PARAMETER DownloadDir
Directory used for downloads. Defaults to %TEMP%\LenovoDrivers.

.EXAMPLE
.\install_lenovo_drivers.bat -DryRun

.EXAMPLE
.\install_lenovo_drivers.bat -LatestAcrossOS
#>
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

#region Configuration
$ApiBase = 'https://newsupport.lenovo.com.cn/api'
$QuickFixApiBase = 'https://ptstpd.lenovo.com.cn'
$StartTime = Get-Date
$LogPath = Join-Path $env:TEMP 'lenovo_driver_install.log'
$PlanPath = Join-Path $env:TEMP 'lenovo_driver_plan.txt'
$HistoryPath = Join-Path $env:TEMP 'lenovo_driver_history.csv'
#endregion

#region Deterministic core
. (Join-Path $PSScriptRoot 'lenovo_driver_core.ps1')
#endregion

#region Logging
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
#endregion

#region Environment and system info
function Test-Admin {
    try {
        $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
        $principal = New-Object Security.Principal.WindowsPrincipal($identity)
        return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    } catch {
        return $false
    }
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
#endregion

#region Lenovo API
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

function Invoke-LenovoQuickFixApi {
    param(
        [string]$SearchKey,
        [string]$OsId
    )
    $headers = @{
        'User-Agent'     = 'Mozilla/5.0'
        'Accept'         = 'application/json, text/plain, */*'
        'Referer'        = 'https://iknow.lenovo.com.cn/'
        'Accept-Language' = 'zh-CN,zh;q=0.9'
    }
    $body = @{
        searchKey = $SearchKey
        osid      = $OsId
    } | ConvertTo-Json -Compress
    $response = Invoke-RestMethod -Uri ($QuickFixApiBase + '/home/driver/SearchForXbb') -Method Post -Headers $headers -ContentType 'application/json;charset=UTF-8' -Body $body -TimeoutSec 30
    $code = [string]$response.StatusCode
    if ($code -notin @('200', '300')) {
        throw "QuickFix API returned $code for $SearchKey / $OsId : $($response.Message)"
    }
    if (-not $response.Data -or -not $response.Data.driverList) {
        throw 'QuickFix API returned no driver list.'
    }
    return $response
}

function Get-OfficialDriverObjects {
    param(
        [string]$CategoryId,
        [string]$OsId,
        [string]$PreferredSource = 'QuickFix'
    )
    $attempts = if ($PreferredSource -eq 'QuickFix') { @('QuickFix', 'Web') } else { @('Web', 'QuickFix') }
    foreach ($source in $attempts) {
        try {
            if ($source -eq 'QuickFix') {
                $data = Invoke-LenovoQuickFixApi -SearchKey $CategoryId -OsId $OsId
                $drivers = @(Get-DriverObjects -ListData $data -OsId $OsId -SourceApi 'QuickFix')
            } else {
                $query = "/drive/drive_listnew?searchKey=$CategoryId&sysid=" + [uri]::EscapeDataString($OsId)
                $data = Invoke-LenovoApi $query
                $drivers = @(Get-DriverObjects -ListData $data -OsId $OsId -SourceApi 'Web')
            }
            if ($drivers.Count -gt 0) {
                return [pscustomobject]@{ Source = $source; Drivers = $drivers }
            }
            throw 'Source returned no driver rows.'
        } catch {
            if ($source -eq 'QuickFix') {
                Write-Log ("QuickFix source unavailable: {0}; falling back to official webpage API." -f $_.Exception.Message) 'WARN'
            } else {
                Write-Log ("Official driver list load failed: {0}" -f $_.Exception.Message) 'ERROR'
            }
        }
    }
    return [pscustomobject]@{ Source = ''; Drivers = @() }
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

function Resolve-LenovoOsEntry {
    param(
        [string]$CategoryId,
        $OsInfo
    )
    function Find-OsEntry {
        param([object[]]$Rows)
        foreach ($row in @($Rows)) {
            if ([string]$row.OSName -like "*$($OsInfo.OsName)*") { return $row }
        }
        foreach ($row in @($Rows)) {
            if ([string]$row.OSName -match [regex]::Escape($OsInfo.Kind)) { return $row }
        }
        return $null
    }

    try {
        $webData = Invoke-LenovoApi "/drive/drive_listnew?searchKey=$CategoryId"
        $webList = @($webData.data.osList)
        $entry = Find-OsEntry $webList
        if ($entry) {
            return [pscustomobject]@{ OsEntry = $entry; OsList = $webList; Source = 'Web' }
        }
        $localId = [string]$webData.data.localOSID
        if ($localId) {
            $entry = $webList | Where-Object { [string]$_.OSID -eq $localId } | Select-Object -First 1
            if ($entry) {
                return [pscustomobject]@{ OsEntry = $entry; OsList = $webList; Source = 'Web' }
            }
        }
    } catch {
        Write-Log ("Official webpage OS list unavailable: {0}" -f $_.Exception.Message) 'WARN'
    }

    foreach ($candidateId in @('42', '248')) {
        try {
            $quickFix = Invoke-LenovoQuickFixApi -SearchKey $CategoryId -OsId $candidateId
            $quickFixList = @($quickFix.Data.osList)
            $entry = Find-OsEntry $quickFixList
            if ($entry) {
                return [pscustomobject]@{ OsEntry = $entry; OsList = $quickFixList; Source = 'QuickFix' }
            }
        } catch {
            Write-Log ("QuickFix OS list unavailable for OSID {0}: {1}" -f $candidateId, $_.Exception.Message) 'WARN'
        }
    }
    return $null
}


function Get-RefreshedDriverUrl {
    param(
        [object]$Driver,
        [string]$CategoryId,
        [string]$SysId,
        [object[]]$OsList,
        [bool]$EnableLatest,
        [bool]$UseQuickFix = $false
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
            if ($UseQuickFix) {
                $data = Invoke-LenovoQuickFixApi -SearchKey $CategoryId -OsId $item.OsId
                $candidates = @(Get-DriverObjects -ListData $data -OsId $item.OsId -SourceApi 'QuickFix')
            } else {
                $data = Invoke-LenovoApi $item.Query
                $candidates = @(Get-DriverObjects -ListData $data -OsId $item.OsId -SourceApi 'Web')
            }
            $match = $candidates | Where-Object { $_.DriverCode -eq $Driver.DriverCode } | Select-Object -First 1
            if ($match -and $match.FilePath) { return $match.FilePath }
        } catch {
            Write-Log ("Could not refresh driver URL for {0}: {1}" -f $Driver.DriverCode, $_.Exception.Message) 'WARN'
        }
    }
    return ''
}

#endregion

#region Local device and app inventory
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

#endregion

#region Driver version comparison
function Get-LocalSoftwareSnapshot {
    param(
        [object[]]$InstalledApps
    )
    $provisionedAmdPower = ''
    try {
        $provisioned = Get-ChildItem 'HKLM:\SOFTWARE\Microsoft\Provisioning\Results' -ErrorAction Stop |
            ForEach-Object { Get-ItemProperty $_.PSPath -ErrorAction SilentlyContinue } |
            Where-Object { $_.PackageFileName -eq 'AMD.Power.Processor.ppkg' } |
            Select-Object -First 1
        if ($provisioned) { $provisionedAmdPower = 'Provisioned' }
    } catch {}

    $fnServiceVersion = ''
    try {
        $svc = Get-CimInstance Win32_Service -Filter "Name='LenovoFnAndFunctionKeys'" -ErrorAction SilentlyContinue
        if ($svc) {
            $exePath = ([string]$svc.PathName).Trim('"')
            if ($exePath -and (Test-Path -LiteralPath $exePath)) {
                $fileVersion = (Get-Item -LiteralPath $exePath).VersionInfo.FileVersion
                if ($fileVersion) { $fnServiceVersion = [string]$fileVersion }
            }
        }
    } catch {}
    return [pscustomobject]@{
        InstalledApps          = $InstalledApps
        ProvisionedAmdPower    = $provisionedAmdPower
        LenovoFnServiceVersion = $fnServiceVersion
    }
}




function Get-LocalDriverVersion {
    param(
        [object]$Driver,
        [object[]]$LocalDevices,
        [object[]]$InstalledApps,
        [object]$SoftwareSnapshot = $null
    )
    $matchedDevices = @(Get-MatchingLocalDevices -Driver $Driver -LocalDevices $LocalDevices)
    $versions = @()
    foreach ($matchedDevice in $matchedDevices) {
        try {
            $versionProp = Get-PnpDeviceProperty -InstanceId $matchedDevice.PnpDeviceId -KeyName 'DEVPKEY_Device_DriverVersion' -ErrorAction Stop
            if ($versionProp.Data) { $versions += [string]$versionProp.Data }
        } catch {}
    }
    if ($null -eq $SoftwareSnapshot) {
        $SoftwareSnapshot = Get-LocalSoftwareSnapshot -InstalledApps $InstalledApps
    }
    $resolved = Resolve-LocalDriverVersion -Driver $Driver -MatchedDevices $matchedDevices -DriverVersions $versions -SoftwareSnapshot $SoftwareSnapshot
    $Driver.LocalVendor = [string]$resolved.LocalVendor
    return [string]$resolved.LocalVersion
}









#endregion

#region Console and plan output
function Show-DriverTable {
    param([object[]]$Drivers)
    $lines = @(Format-DriverTableLines -Drivers $Drivers)
    $lineIndex = 0
    foreach ($line in $lines) {
        if ($lineIndex -eq 1) { Write-Host $line -ForegroundColor Cyan }
        elseif ($lineIndex -eq 2) { Write-Host $line -ForegroundColor DarkGray }
        else { Write-Host $line }
        $lineIndex++
    }
}

function Show-StatusSummary {
    param([object[]]$Drivers)
    $lines = @(Format-StatusSummaryLines -Drivers $Drivers)
    $lineIndex = 0
    foreach ($line in $lines) {
        switch ($lineIndex) {
            1 { Write-Host $line -ForegroundColor Yellow }
            2 { Write-Host $line -ForegroundColor Green }
            3 { Write-Host $line -ForegroundColor Cyan }
            4 { Write-Host $line -ForegroundColor Red }
            5 { Write-Host $line -ForegroundColor DarkGray }
            6 { Write-Host $line -ForegroundColor Magenta }
            default { if ($line -match '^  Note:') { Write-Host $line -ForegroundColor DarkGray } else { Write-Host $line } }
        }
        $lineIndex++
    }
}

function Write-PlanFile {
    param(
        [object[]]$Drivers,
        [string]$Path
    )
    $lines = @(Build-PlanText -Drivers $Drivers -GeneratedAt (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'))
    $lines | Set-Content -LiteralPath $Path -Encoding UTF8
    return $Path
}

#endregion

#region Driver selection

#endregion

#region Download integrity



function Get-FileSha256 {
    param([string]$Path)
    try {
        $hash = Get-FileHash -LiteralPath $Path -Algorithm SHA256 -ErrorAction Stop
        return $hash.Hash.ToLowerInvariant()
    } catch {
        return ''
    }
}

function Get-FileMd5 {
    param([string]$Path)
    try {
        $hash = Get-FileHash -LiteralPath $Path -Algorithm MD5 -ErrorAction Stop
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

#endregion

#region Driver history
function Read-DriverHistory {
    if (-not (Test-Path -LiteralPath $HistoryPath)) { return @() }
    try {
        $rows = @(Import-Csv -LiteralPath $HistoryPath -Encoding UTF8 -ErrorAction Stop)
        return @($rows | Where-Object { $_.DriverCode })
    } catch {
        Write-Log ("Could not read driver history: {0}" -f $_.Exception.Message) 'WARN'
        return @()
    }
}

function Write-DriverHistoryRecord {
    param(
        [object]$Driver,
        [string]$Result,
        [string]$Message = ''
    )
    $record = Build-DriverHistoryRecord -Driver $Driver -Result $Result -Message $Message -Timestamp (Get-Date -Format 'yyyy-MM-dd HH:mm:ss')
    $header = 'Timestamp,DriverCode,OSID,OSName,DriverName,Version,FileName,MD5,Source,Result,Message'
    if (-not (Test-Path -LiteralPath $HistoryPath)) {
        Set-Content -LiteralPath $HistoryPath -Value $header -Encoding UTF8
    }
    $fields = @()
    foreach ($name in $record.Keys) {
        $value = [string]$record[$name]
        $fields += ('"' + ($value -replace '"', '""') + '"')
    }
    Add-Content -LiteralPath $HistoryPath -Value ($fields -join ',') -Encoding UTF8
}



$script:AlternateSourceMap = $null
function Get-DriverSourceLabel {
    param(
        [object]$Driver,
        [object[]]$History,
        [string]$CategoryId,
        [object[]]$OsList,
        [string]$SysId,
        [string]$PreferredSource
    )
    if ($null -eq $script:AlternateSourceMap) {
        $script:AlternateSourceMap = @{}
        foreach ($alt in @($OsList)) {
            $altId = [string]$alt.OSID
            if ($altId -eq $SysId) { continue }
            try {
                $altResult = Get-OfficialDriverObjects -CategoryId $CategoryId -OsId $altId -PreferredSource $PreferredSource
                foreach ($altDriver in @($altResult.Drivers)) {
                    foreach ($versionKey in @(Get-VersionMatchKeys $altDriver.Version)) {
                        $nameKey = '{0}|{1}' -f ([string]$altDriver.DriverName).Trim(), $versionKey
                        $codeKey = '{0}|{1}' -f $altDriver.DriverCode, $versionKey
                        foreach ($key in @($nameKey, $codeKey)) {
                            if (-not $script:AlternateSourceMap.ContainsKey($key)) {
                                $script:AlternateSourceMap[$key] = [pscustomobject]@{
                                    OSID   = [string]$altDriver.OSID
                                    OSName = [string]$altDriver.OsName
                                }
                            }
                        }
                    }
                }
            } catch {
                Write-Log ("Could not load alternate source for attribution ({0}): {1}" -f $altId, $_.Exception.Message) 'WARN'
            }
        }
    }
    return Resolve-DriverSourceLabel -Driver $Driver -History $History -AlternateSourceMap $script:AlternateSourceMap
}
#endregion

#region Process and installer helpers

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
        $installerArgs = @()
        $installCode = [string]$Driver.InstallParameter
        if (-not $installCode) { $installCode = [string]$Driver.InstallCode }
        if ($installCode -and $installCode -notmatch '^/add-driver') {
            $installerArgs = @($installCode -split '\s+' | Where-Object { $_ })
        }
        if ($installerArgs.Count -eq 0) {
            $installerArgs = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART')
        }
        $result = Invoke-ProcessWithTimeout -FilePath $setupPath -ArgumentList $installerArgs -TimeoutSeconds 900 -WorkingDirectory (Split-Path -Parent $setupPath)
        if ($result.TimedOut) {
            Write-Log ("[{0}] Inner installer timed out and its process tree was killed." -f $Driver.DriverCode) 'ERROR'
            return [pscustomobject]@{ Used = $true; ExitCode = -1 }
        }
        if (Test-RebootExitCode -ExitCode $result.ExitCode) {
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
            if (Test-RebootExitCode -ExitCode $result.ExitCode) {
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
            $exeArgs = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', ('/LOG="' + $log + '"'))
            $installCode = [string]$Driver.InstallParameter
            if (-not $installCode) { $installCode = [string]$Driver.InstallCode }
            if ($installCode -and $installCode -notmatch '^/add-driver') {
                $codeArgs = @($installCode -split '\s+' | Where-Object { $_ })
                if ($codeArgs.Count -gt 0) {
                    $exeArgs += $codeArgs
                    Write-Log ("[{0}] Using Lenovo InstallCode arguments: {1}" -f $Driver.DriverCode, ($codeArgs -join ' ')) 'INFO'
                }
            }
            $result = Invoke-ProcessWithTimeout -FilePath $FilePath -ArgumentList $exeArgs -TimeoutSeconds 900 -WorkingDirectory $WorkingDir
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
            if (Test-RebootExitCode -ExitCode $result.ExitCode) {
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

#endregion

#region Help and interactive selection
function Show-Help {
    $help = @'
Lenovo Driver Installer

Usage:
  install_lenovo_drivers.bat [-DryRun] [-CurrentOSOnly] [-LatestAcrossOS] [-SkipHashCheck] [-IncludeBios] [-DownloadOnly] [-DownloadDir <path>] [-Model <model>] [-Help]

Options:
  -DryRun        Compare versions without downloading or installing.
  -CurrentOSOnly Use only the current OS driver list (default).
  -LatestAcrossOS Allow newer drivers from other OS entries. Cannot be used with -CurrentOSOnly.
  -SkipHashCheck Skip local SHA-256 cache and official MD5 validation.
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

Data sources:
  QuickFix official API first; official webpage API fallback.

Driver history:
  %TEMP%\lenovo_driver_history.csv

Log file:
  %TEMP%\lenovo_driver_install.log
'@
    Write-Host $help
}

function Read-DriverSelection {
    param([object[]]$AllSelected)
    $answer = Read-Host 'Enter driver numbers, e.g. 1,3,5'
    $parsed = Parse-DriverSelectionTokens -InputText $answer -AllSelected $AllSelected
    foreach ($token in $parsed.Invalid) {
        Write-Log ("Invalid driver number: {0}" -f $token) 'WARN'
    }
    foreach ($number in $parsed.NotApplicable) {
        Write-Log ("Driver {0} is not applicable and was skipped." -f $number) 'WARN'
    }
    if ($parsed.Selected.Count -eq 0) {
        Write-Log 'No valid driver numbers were selected.'
        return @()
    }
    return @($parsed.Selected)
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

#endregion

#region Main
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

$osResolution = Resolve-LenovoOsEntry -CategoryId $categoryId -OsInfo $osInfo
if (-not $osResolution -or -not $osResolution.OsEntry) {
    throw 'Could not resolve the current OS entry from Lenovo. Use -LatestAcrossOS only after confirming the OS list is complete.'
}
$osList = @($osResolution.OsList)
$osEntry = $osResolution.OsEntry
$sysId = [string]$osEntry.OSID
Write-Log ("Matched OS entry : {0} (OSID {1})" -f $osEntry.OSName, $sysId)
if ($osResolution.Source -eq 'QuickFix') {
    Write-Log 'OS list source : QuickFix (webpage OS list unavailable)' 'WARN'
}

$driverResult = Get-OfficialDriverObjects -CategoryId $categoryId -OsId $sysId -PreferredSource 'QuickFix'
if (-not $driverResult.Source -or $driverResult.Drivers.Count -eq 0) {
    throw 'Could not load the official driver list from QuickFix or the webpage API.'
}
$drivers = @($driverResult.Drivers)
$dataSource = $driverResult.Source
Write-Log ("Driver source : {0} (OSID {1})" -f $dataSource, $sysId)

$enableLatest = $LatestAcrossOS
if ($enableLatest) {
    Write-Log 'Cross-OS latest mode is an experimental exception, not the standard update path.'
    foreach ($alt in $osList) {
        $altId = [string]$alt.OSID
        if ($altId -eq $sysId) { continue }
        $altResult = Get-OfficialDriverObjects -CategoryId $categoryId -OsId $altId -PreferredSource $dataSource
        if ($altResult.Source -and $altResult.Drivers.Count -gt 0) {
            $drivers += @($altResult.Drivers)
        } else {
            Write-Log ("Could not load alternate OS entry {0}." -f $altId) 'WARN'
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
$softwareSnapshot = Get-LocalSoftwareSnapshot -InstalledApps $installedApps
$driverHistory = @(Read-DriverHistory)
foreach ($driver in $selected) {
    if (-not (Test-DriverApplicable -Driver $driver -LocalDevices $localDevices)) {
        $driver.CompareStatus = 'Not applicable'
        continue
    }
    $driver.LocalVersion = Get-LocalDriverVersion -Driver $driver -LocalDevices $localDevices -InstalledApps $installedApps -SoftwareSnapshot $softwareSnapshot
    $driver.CompareStatus = Compare-DriverStatus -Remote $driver.Version -Local $driver.LocalVersion -Vendor $driver.LocalVendor
    if ($driver.CompareStatus -eq 'Local newer') {
        $driver.CompareSource = Get-DriverSourceLabel -Driver $driver -History $driverHistory -CategoryId $categoryId -OsList $osList -SysId $sysId -PreferredSource $dataSource
        Write-Log ("[{0}] {1}" -f $driver.DriverCode, $driver.CompareSource) 'WARN'
    }
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
                if (Test-FileSizeMatch -ExpectedBytes $expectedSize -ActualBytes $existingSize) {
                    $needsDownload = $false
                } else {
                    Write-Log ("[{0}] Cached file size mismatch, redownloading." -f $driver.DriverCode) 'WARN'
                    Remove-Item -LiteralPath $outFile -Force
                    Remove-Item -LiteralPath ($outFile + '.sha256') -Force -ErrorAction SilentlyContinue
                }
                if ($needsDownload -eq $false -and -not (Test-HashCompanion $outFile)) {
                    Write-Log ("[{0}] Cached file hash missing or mismatch, redownloading." -f $driver.DriverCode) 'WARN'
                    Remove-Item -LiteralPath $outFile -Force
                    Remove-Item -LiteralPath ($outFile + '.sha256') -Force -ErrorAction SilentlyContinue
                    $needsDownload = $true
                }
                if ($needsDownload -eq $false -and $driver.OfficialMd5 -and -not $SkipHashCheck) {
                    $cachedMd5 = Get-FileMd5 $outFile
                    if ($cachedMd5 -ne $driver.OfficialMd5) {
                        Write-Log ("[{0}] Cached file MD5 mismatch, redownloading." -f $driver.DriverCode) 'WARN'
                        Remove-Item -LiteralPath $outFile -Force
                        Remove-Item -LiteralPath ($outFile + '.sha256') -Force -ErrorAction SilentlyContinue
                        $needsDownload = $true
                    }
                }
                if ($needsDownload -eq $false) {
                    Write-Log ("[{0}] Using verified cached file ({1} bytes)." -f $driver.DriverCode, $existingSize)
                }
            }
            if ($needsDownload) {
                $null = Invoke-DownloadWithRetry -Url $driver.FilePath -OutFile $outFile
            }
            $downloadedSize = (Get-Item -LiteralPath $outFile).Length
            if (-not (Test-FileSizeMatch -ExpectedBytes $expectedSize -ActualBytes $downloadedSize)) {
                throw "Downloaded size mismatch: expected $expectedSize, got $downloadedSize"
            }
            if ($driver.OfficialMd5 -and -not $SkipHashCheck) {
                $actualMd5 = Get-FileMd5 $outFile
                if (-not $actualMd5 -or $actualMd5 -ne $driver.OfficialMd5) {
                    throw "Official MD5 mismatch: expected $($driver.OfficialMd5), got $actualMd5"
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
            if ($_.Exception.Message -match '403|Forbidden') {
                $refreshedUrl = Get-RefreshedDriverUrl -Driver $driver -CategoryId $categoryId -SysId $sysId -OsList $osList -EnableLatest $enableLatest -UseQuickFix ($dataSource -eq 'QuickFix')
                if (-not $refreshedUrl) {
                    Write-Log ("[{0}] Could not refresh the expired download URL." -f $driver.DriverCode) 'WARN'
                    break
                }
                Write-Log ("[{0}] Download URL expired; refreshing and retrying." -f $driver.DriverCode) 'WARN'
                $driver.FilePath = $refreshedUrl
                Remove-Item -LiteralPath $outFile -Force -ErrorAction SilentlyContinue
                Remove-Item -LiteralPath ($outFile + '.sha256') -Force -ErrorAction SilentlyContinue
            } elseif ($_.Exception.Message -match 'MD5 mismatch') {
                Write-Log ("[{0}] Download failed integrity check; retrying." -f $driver.DriverCode) 'WARN'
                Remove-Item -LiteralPath $outFile -Force -ErrorAction SilentlyContinue
                Remove-Item -LiteralPath ($outFile + '.sha256') -Force -ErrorAction SilentlyContinue
            } else {
                break
            }
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
        Write-DriverHistoryRecord -Driver $driver -Result 'Downloaded' -Message ("size={0} bytes" -f $downloadedSize)
        $success += $driver
        continue
    }

    Write-Log ("[{0}] Installing {1}" -f $driver.DriverCode, $driver.FileName)
    try {
        $code = Install-DriverFile -FilePath $outFile -Driver $driver -WorkingDir $dlDir
        if ($code -eq 0) {
            Write-Log ("[{0}] Install success." -f $driver.DriverCode)
            Write-DriverHistoryRecord -Driver $driver -Result 'Installed' -Message 'exit=0'
            $success += $driver
        } else {
            Write-Log ("[{0}] Install exit code {1}." -f $driver.DriverCode, $code) 'ERROR'
            Write-DriverHistoryRecord -Driver $driver -Result 'Failed' -Message ("exit={0}" -f $code)
            $failed += $driver
        }
    } catch {
        Write-Log ("[{0}] Install failed: {1}" -f $driver.DriverCode, $_.Exception.Message) 'ERROR'
        Write-DriverHistoryRecord -Driver $driver -Result 'Failed' -Message $_.Exception.Message
        $failed += $driver
    }
}

if ($success.Count -gt 0 -and -not $DownloadOnly) {
    Write-Log 'Running post-install verification pass...'
    try {
        $localDevices = @(Get-LocalDeviceSnapshot)
        $installedApps = @(Get-InstalledApps)
        $softwareSnapshot = Get-LocalSoftwareSnapshot -InstalledApps $installedApps
        foreach ($driver in $success) {
            $beforeLocal = $driver.LocalVersion
            $afterLocal = Get-LocalDriverVersion -Driver $driver -LocalDevices $localDevices -InstalledApps $installedApps -SoftwareSnapshot $softwareSnapshot
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
#endregion
