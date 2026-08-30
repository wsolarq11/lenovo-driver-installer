function Update-WorkerStatus {
    $process = $script:WorkerProcess
    if ($null -eq $process) {
        if ($null -ne $script:Timer) { $script:Timer.Stop() }
        return
    }
    $stdoutPath = $script:WorkerStdoutPath
    if ($stdoutPath -and (Test-Path -LiteralPath $stdoutPath)) {
        $newLines = @(Get-Content -LiteralPath $stdoutPath -Encoding UTF8 -ErrorAction SilentlyContinue)
        $count = @($newLines).Count
        if ($count -gt $script:WorkerStdoutRead) {
            foreach ($line in @($newLines[$script:WorkerStdoutRead..($count - 1)])) {
                if ($line) { Add-GuiLog -Message ([string]$line) }
            }
            $script:WorkerStdoutRead = $count
        }
    }
    $process.Refresh()
    if (-not $process.HasExited) { return }

    if ($null -ne $script:Timer) { $script:Timer.Stop() }
    $exitCode = $process.ExitCode
    $mode = $script:WorkerMode
    $exportPath = $script:WorkerExportPath
    $outFile = $script:WorkerStdoutPath
    $stderrFile = $script:WorkerStderrPath
    $script:WorkerProcess = $null
    $script:WorkerMode = ''
    $script:WorkerExportPath = ''
    $script:WorkerStdoutPath = ''
    $script:WorkerStderrPath = ''
    $script:WorkerStdoutRead = 0
    Set-GuiBusy -Busy $false

    if ($stderrFile -and (Test-Path -LiteralPath $stderrFile)) {
        $stderrLines = @(Get-Content -LiteralPath $stderrFile -Encoding UTF8 -ErrorAction SilentlyContinue)
        foreach ($line in $stderrLines) {
            if ($line) { Add-GuiLog -Message ("STDERR: {0}" -f [string]$line) }
        }
    }

    if ($exitCode -ne 0) {
        Add-GuiLog -Message ("后台任务结束，退出码 {0}" -f $exitCode)
        return
    }
    if ($mode -eq 'Export' -and $exportPath -and (Test-Path -LiteralPath $exportPath)) {
        try {
            $export = Get-Content -LiteralPath $exportPath -Raw -Encoding UTF8 | ConvertFrom-Json
            $script:LastExport = $export
            $machineText = $script:Ui['MachineText']
            if ($machineText) {
                $machineText.Text = ('{0} / {1}  |  当前系统 {2} (OSID {3})  |  当前列表 {4} (OSID {5})' -f `
                    $export.MachineModel, $export.SerialNumber, $export.CurrentOsName, $export.CurrentOsId, `
                    $export.ListOsName, $export.ListOsId)
            }
            Set-OsComboFromExport -Export $export
            $rows = foreach ($d in @($export.Drivers)) {
                [pscustomobject]@{
                    Selected      = [bool]$d.IsUpdate
                    DriverCode    = [string]$d.DriverCode
                    DriverName    = [string]$d.DriverName
                    Version       = [string]$d.Version
                    LocalVersion  = [string]$d.LocalVersion
                    CompareStatus = [string]$d.CompareStatus
                    SourceAudit   = if ($d.SourceAudit) { [string]$d.SourceAudit } else { [string]$d.CompareSource }
                    FileName      = [string]$d.FileName
                    FilePath      = [string]$d.FilePath
                    FileSize      = [string]$d.FileSize
                    MD5           = [string]$d.MD5
                    IsApplicable  = [bool]$d.IsApplicable
                    IsUpdate      = [bool]$d.IsUpdate
                }
            }
            Set-LenovoDriverRows -Rows $rows
            Add-GuiLog -Message ("已加载 {0} 行驱动列表，来自 {1}" -f $export.Drivers.Count, $export.ListOsName)
        } catch {
            Add-GuiLog -Message ("无法解析 GUI 导出：{0}" -f $_.Exception.Message)
        }
    } elseif ($mode -eq 'Install') {
        Add-GuiLog -Message '驱动下载/安装任务完成。'
    }
}

function Start-WorkerPolling {
    if ($null -ne $script:Timer) { $script:Timer.Stop() }
    $script:Timer = New-Object Windows.Threading.DispatcherTimer
    $script:Timer.Interval = [TimeSpan]::FromMilliseconds(400)
    $handler = { Update-WorkerStatus }
    $script:Timer.Add_Tick($handler) | Out-Null
    $script:Timer.Start()
}

function ConvertTo-CommandLineArgument {
    param([string]$Argument)
    if ($Argument -eq '') { return '""' }
    if ($Argument -notmatch '[\s"]') { return $Argument }
    $builder = New-Object System.Text.StringBuilder
    [void]$builder.Append('"')
    $backslashes = 0
    foreach ($char in $Argument.ToCharArray()) {
        if ($char -eq '\') {
            $backslashes++
            continue
        }
        if ($char -eq '"') {
            [void]$builder.Append('\' * ($backslashes * 2))
            $backslashes = 0
            [void]$builder.Append('\"')
            continue
        }
        [void]$builder.Append('\' * $backslashes)
        $backslashes = 0
        [void]$builder.Append($char)
    }
    [void]$builder.Append('\' * ($backslashes * 2))
    [void]$builder.Append('"')
    return $builder.ToString()
}

function ConvertTo-ProcessArgumentList {
    param([object[]]$Arguments)
    $tokens = @()
    foreach ($argument in $Arguments) {
        $tokens += ConvertTo-CommandLineArgument -Argument ([string]$argument)
    }
    return $tokens
}

function Start-LenovoDriverJob {
    param(
        [switch]$Export,
        [switch]$Install,
        [switch]$DownloadOnly,
        [string]$TargetOsId = '',
        [string]$InstallCodes = ''
    )
    if ($null -ne $script:WorkerProcess) {
        Add-GuiLog -Message '已有任务正在运行，请等待完成。'
        return
    }

    $argsList = @('-Elevated')
    if ($Model) {
        $argsList += '-Model'
        $argsList += $Model
    }
    if ($DownloadDir) {
        $argsList += '-DownloadDir'
        $argsList += $DownloadDir
    }
    if ($script:Ui['IncludeBiosCheck'].IsChecked -eq $true) {
        $argsList += '-IncludeBios'
    }
    if ($script:Ui['SkipHashCheckBox'].IsChecked -eq $true) {
        $argsList += '-SkipHashCheck'
    }
    if ($Export) {
        $script:WorkerExportPath = Join-Path $env:TEMP ('lenovo_gui_export_{0}.json' -f ([guid]::NewGuid().ToString('N')))
        $argsList += '-GuiExportPath'
        $argsList += $script:WorkerExportPath
        $argsList += '-DryRun'
    }
    if ($TargetOsId) {
        $argsList += '-TargetOS'
        $argsList += $TargetOsId
    }
    if ($InstallCodes) {
        $argsList += '-GuiInstallCodes'
        $argsList += $InstallCodes
    }
    if ($DownloadOnly) {
        $argsList += '-DownloadOnly'
    }

    $script:WorkerMode = if ($Export) { 'Export' } else { 'Install' }
    $script:WorkerStdoutPath = Join-Path $env:TEMP ('lenovo_gui_out_{0}.txt' -f ([guid]::NewGuid().ToString('N')))
    $script:WorkerStderrPath = Join-Path $env:TEMP ('lenovo_gui_err_{0}.txt' -f ([guid]::NewGuid().ToString('N')))
    $script:WorkerStdoutRead = 0
    Add-GuiLog -Message ("启动后台任务：{0}" -f $script:WorkerMode)
    Set-GuiBusy -Busy $true

    if (-not (Test-Path -LiteralPath $script:InstallerPath)) {
        Add-GuiLog -Message ("Go 引擎不存在，请先构建：{0}" -f $script:InstallerPath)
        Set-GuiBusy -Busy $false
        return
    }

    $tokens = ConvertTo-ProcessArgumentList -Arguments $argsList
    $script:WorkerProcess = Start-Process -FilePath $script:InstallerPath `
        -ArgumentList $tokens `
        -RedirectStandardOutput $script:WorkerStdoutPath `
        -RedirectStandardError $script:WorkerStderrPath `
        -PassThru -WindowStyle Hidden
    Start-WorkerPolling
}
