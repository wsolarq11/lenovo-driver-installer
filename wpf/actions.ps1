function Start-InstallAction {
    param(
        [ValidateSet('Updates', 'Applicable', 'Selected')]
        [string]$Kind,
        [switch]$DownloadOnly
    )
    $rows = @($script:DriverRows)
    if ($rows.Count -eq 0) {
        Add-GuiLog -Message '请先刷新驱动列表。'
        return
    }
    switch ($Kind) {
        'Updates'    { $targets = @($rows | Where-Object { $_.IsUpdate }) }
        'Applicable' { $targets = @($rows | Where-Object { $_.IsApplicable }) }
        'Selected'   { $targets = @($rows | Where-Object { $_.Selected }) }
    }
    if ($targets.Count -eq 0) {
        Add-GuiLog -Message '没有符合条件的驱动。'
        return
    }
    $detail = @($targets | ForEach-Object { "  {0}  {1}  {2} -> {3}" -f $_.DriverCode, $_.DriverName, $_.Version, $_.LocalVersion }) -join "`r`n"
    $action = if ($DownloadOnly) { '仅下载' } else { '下载并安装' }
    $message = "将{0}以下 {1} 个驱动：`r`n`r`n{2}`r`n`r`n确定继续？" -f $action, $targets.Count, $detail
    $answer = [System.Windows.MessageBox]::Show($message, '确认驱动集合', [System.Windows.MessageBoxButton]::YesNo, [System.Windows.MessageBoxImage]::Question)
    if ($answer -ne [System.Windows.MessageBoxResult]::Yes) {
        Add-GuiLog -Message '已取消安装操作。'
        return
    }
    $codes = @($targets | ForEach-Object { $_.DriverCode }) -join ','
    Start-LenovoDriverJob -Install -DownloadOnly:$DownloadOnly -TargetOsId (Get-DisplayedOsId) -InstallCodes $codes
}

function Get-RollbackOfferPath {
    if ($env:LOCALAPPDATA) {
        return Join-Path $env:LOCALAPPDATA 'Lenovo\DriverInstaller\lenovo_driver_rollback.json'
    }
    return Join-Path $env:TEMP 'lenovo_driver_rollback.json'
}

function Read-PendingRollbackOffers {
    $path = Get-RollbackOfferPath
    if (-not (Test-Path -LiteralPath $path)) { return @() }
    try {
        $data = Get-Content -LiteralPath $path -Raw -Encoding UTF8 | ConvertFrom-Json
        return @($data.Offers | Where-Object { $_.State -eq 'pending' })
    } catch {
        return @()
    }
}

function Show-RollbackOffers {
    $offers = Read-PendingRollbackOffers
    if ($offers.Count -eq 0) { return }
    $lines = foreach ($o in $offers) {
        $devices = (@($o.Devices) | ForEach-Object { "    {0}  ({1})" -f $_.PnpDeviceID, $_.Problem }) -join "`r`n"
        "  {0}  {1}`r`n    版本 {2} -> {3}`r`n    旧 INF {4}`r`n{5}" -f $o.DriverCode, $o.DriverName, $o.AfterVersion, $o.BeforeVersion, $o.BeforeInf, $devices
    }
    $message = "以下驱动安装后检测到设备问题，可回退到安装前的驱动程序：`r`n`r`n{0}`r`n`r`n说明：问题码为本机实测；「由本次安装导致」为时间相关性推断，非直接测量。回退会先回退驱动程序，无备份时重装旧 INF，不删除驱动包。是否执行？" -f ($lines -join "`r`n")
    $answer = [System.Windows.MessageBox]::Show($message, '回退确认', [System.Windows.MessageBoxButton]::YesNo, [System.Windows.MessageBoxImage]::Warning)
    if ($answer -ne [System.Windows.MessageBoxResult]::Yes) {
        Add-GuiLog -Message '已跳过回退操作。'
        return
    }
    $codes = @($offers | ForEach-Object { $_.DriverCode }) -join ','
    Start-LenovoDriverJob -Rollback -RollbackCodes $codes
}
