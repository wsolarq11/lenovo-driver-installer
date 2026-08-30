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
