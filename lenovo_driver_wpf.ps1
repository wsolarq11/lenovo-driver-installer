<#
.SYNOPSIS
WPF front end for the Lenovo driver installer.

.DESCRIPTION
Launches a desktop UI that queries the existing installer engine through JSON
export jobs and runs selected driver downloads/installations in background
PowerShell jobs. The CLI remains the deterministic engine; this script only
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

$script:InstallerPath = Join-Path $PSScriptRoot 'install_lenovo_drivers.ps1'
$script:WorkerProcess = $null
$script:WorkerMode = ''
$script:WorkerExportPath = ''
$script:WorkerStdoutPath = ''
$script:Timer = $null
$script:DriverRows = $null
$script:LastExport = $null
$script:Ui = @{}

function Test-IsAdministrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Add-GuiLog {
    param([string]$Message)
    $line = "[{0}] {1}`r`n" -f (Get-Date -Format 'HH:mm:ss'), $Message
    $box = $script:Ui['LogBox']
    if ($null -ne $box) {
        $box.AppendText($line)
        $box.ScrollToEnd()
    }
}

function Set-GuiBusy {
    param([bool]$Busy)
    $names = @('RefreshButton', 'InstallUpdatesButton', 'InstallApplicableButton', 'InstallSelectedButton', 'DownloadSelectedButton')
    foreach ($name in $names) {
        $control = $script:Ui[$name]
        if ($null -ne $control) { $control.IsEnabled = -not $Busy }
    }
    $progress = $script:Ui['ProgressBar']
    if ($null -ne $progress) {
        $progress.Visibility = if ($Busy) { [Windows.Visibility]::Visible } else { [Windows.Visibility]::Collapsed }
        $progress.IsIndeterminate = $Busy
    }
    $status = $script:Ui['StatusText']
    if ($null -ne $status) {
        $status.Text = if ($Busy) { '正在运行 Lenovo 驱动引擎...' } else { '就绪' }
    }
}

function Get-SelectedOsId {
    $item = $script:Ui['OsCombo'].SelectedItem
    if ($null -eq $item -or $null -eq $item.OSID) { return '' }
    return [string]$item.OSID
}

function Get-DisplayedOsId {
    if ($script:LastExport -and $script:LastExport.ListOsId) {
        return [string]$script:LastExport.ListOsId
    }
    return ''
}

function Add-DriverGridColumns {
    param([System.Windows.Controls.DataGrid]$Grid)
    $Grid.AutoGenerateColumns = $false
    $Grid.CanUserAddRows = $false
    $Grid.CanUserDeleteRows = $false
    $Grid.IsReadOnly = $false
    $Grid.SelectionMode = [System.Windows.Controls.DataGridSelectionMode]::Extended
    $Grid.GridLinesVisibility = [System.Windows.Controls.DataGridGridLinesVisibility]::Horizontal
    $Grid.HeadersVisibility = [System.Windows.Controls.DataGridHeadersVisibility]::Column

    $check = New-Object System.Windows.Controls.DataGridCheckBoxColumn
    $check.Header = '安装'
    $check.Width = 56
    $check.Binding = New-Object System.Windows.Data.Binding -ArgumentList 'Selected'
    $check.Binding.UpdateSourceTrigger = [System.Windows.Data.UpdateSourceTrigger]::PropertyChanged
    $Grid.Columns.Add($check) | Out-Null

    $code = New-Object System.Windows.Controls.DataGridTextColumn
    $code.Header = '驱动代码'
    $code.Width = 150
    $code.Binding = New-Object System.Windows.Data.Binding -ArgumentList 'DriverCode'
    $Grid.Columns.Add($code) | Out-Null

    $name = New-Object System.Windows.Controls.DataGridTextColumn
    $name.Header = '驱动名称'
    $name.Width = 320
    $name.Binding = New-Object System.Windows.Data.Binding -ArgumentList 'DriverName'
    $Grid.Columns.Add($name) | Out-Null

    $remote = New-Object System.Windows.Controls.DataGridTextColumn
    $remote.Header = '官方版本'
    $remote.Width = 150
    $remote.Binding = New-Object System.Windows.Data.Binding -ArgumentList 'Version'
    $Grid.Columns.Add($remote) | Out-Null

    $local = New-Object System.Windows.Controls.DataGridTextColumn
    $local.Header = '本地版本'
    $local.Width = 150
    $local.Binding = New-Object System.Windows.Data.Binding -ArgumentList 'LocalVersion'
    $Grid.Columns.Add($local) | Out-Null

    $status = New-Object System.Windows.Controls.DataGridTextColumn
    $status.Header = '状态'
    $status.Width = 120
    $status.Binding = New-Object System.Windows.Data.Binding -ArgumentList 'CompareStatus'
    $Grid.Columns.Add($status) | Out-Null

    $source = New-Object System.Windows.Controls.DataGridTextColumn
    $source.Header = '来源'
    $source.Width = 330
    $source.Binding = New-Object System.Windows.Data.Binding -ArgumentList 'SourceAudit'
    $Grid.Columns.Add($source) | Out-Null
}

function Set-LenovoDriverRows {
    param([object[]]$Rows)
    $collection = New-Object System.Collections.ObjectModel.ObservableCollection[object]
    foreach ($row in $Rows) {
        $collection.Add($row) | Out-Null
    }
    $script:DriverRows = $collection
    $script:Ui['DriverGrid'].ItemsSource = $collection
}

function Set-OsComboFromExport {
    param([object]$Export)
    $items = New-Object System.Collections.ObjectModel.ObservableCollection[object]
    $items.Add([pscustomobject]@{
        Label = ('当前系统 ({0})' -f $Export.CurrentOsName)
        OSID  = $null
    }) | Out-Null
    foreach ($os in @($Export.OsList)) {
        $items.Add([pscustomobject]@{
            Label = ('{0} (OSID {1})' -f $os.OSName, $os.OSID)
            OSID  = $os.OSID
        }) | Out-Null
    }
    $script:Ui['OsCombo'].ItemsSource = $items
    $targetIndex = 0
    if ($Export.ListOsId) {
        for ($i = 0; $i -lt $items.Count; $i++) {
            if ([string]$items[$i].OSID -eq [string]$Export.ListOsId) {
                $targetIndex = $i
                break
            }
        }
    }
    $script:Ui['OsCombo'].SelectedIndex = $targetIndex
}

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
    $script:WorkerProcess = $null
    $script:WorkerMode = ''
    $script:WorkerExportPath = ''
    $script:WorkerStdoutPath = ''
    $script:WorkerStdoutRead = 0
    Set-GuiBusy -Busy $false

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
    $script:WorkerStdoutRead = 0
    Add-GuiLog -Message ("启动后台任务：{0}" -f (($argsList | Where-Object { $_ -notmatch '^-(GuiExportPath|GuiInstallCodes)$' } | ForEach-Object { $_ }) -join ' '))
    Set-GuiBusy -Busy $true

    $valueParams = @('-Model', '-DownloadDir', '-GuiExportPath', '-TargetOS', '-GuiInstallCodes')
    $tokens = @()
    for ($i = 0; $i -lt $argsList.Count; $i++) {
        $token = [string]$argsList[$i]
        if ($valueParams -contains $token) {
            $tokens += $token
            $i++
            $value = [string]$argsList[$i]
            $tokens += ("'" + $value.Replace("'", "''") + "'")
        } else {
            $tokens += $token
        }
    }
    $escapedScript = $script:InstallerPath.Replace("'", "''")
    $escapedOut = $script:WorkerStdoutPath.Replace("'", "''")
    $command = "& '$escapedScript' $($tokens -join ' ') *> '$escapedOut'"
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = 'powershell.exe'
    $psi.Arguments = "-NoProfile -ExecutionPolicy Bypass -Command `"$command`""
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $script:WorkerProcess = [System.Diagnostics.Process]::Start($psi)
    Start-WorkerPolling
}

function New-LenovoDriverWindow {
    $xaml = @'
<Window xmlns="http://schemas.microsoft.com/winfx/2006/xaml/presentation"
        xmlns:x="http://schemas.microsoft.com/winfx/2006/xaml"
        Title="联想驱动安装器" Height="760" Width="1240"
        WindowStartupLocation="CenterScreen" FontSize="13"
        UseLayoutRounding="True" Background="#F7F8FA">
    <DockPanel>
        <Border DockPanel.Dock="Top" Padding="14,12,14,10" Background="#FFFFFF" BorderBrush="#D8DEE6" BorderThickness="0,0,0,1">
            <StackPanel>
                <DockPanel>
                    <TextBlock Text="联想驱动安装器" FontSize="20" FontWeight="Bold" VerticalAlignment="Center" Foreground="#1F2937"/>
                    <TextBlock x:Name="MachineText" DockPanel.Dock="Right" VerticalAlignment="Center" Foreground="#4B5563" Text="正在识别机器..."/>
                </DockPanel>
                <StackPanel Orientation="Horizontal" Margin="0,12,0,0">
                    <TextBlock Text="驱动列表系统：" VerticalAlignment="Center" Foreground="#374151"/>
                    <ComboBox x:Name="OsCombo" Width="300" Margin="8,0,16,0" DisplayMemberPath="Label"/>
                    <CheckBox x:Name="IncludeBiosCheck" Content="包含 BIOS/EC" Margin="0,0,18,0" VerticalAlignment="Center"/>
                    <CheckBox x:Name="SkipHashCheckBox" Content="跳过哈希校验" Margin="0,0,18,0" VerticalAlignment="Center"/>
                    <Button x:Name="RefreshButton" Content="刷新驱动列表" MinWidth="110" Padding="10,5"/>
                </StackPanel>
            </StackPanel>
        </Border>

        <StatusBar DockPanel.Dock="Bottom" Background="#F3F4F6">
            <StatusBarItem>
                <TextBlock x:Name="StatusText" Text="就绪" Foreground="#374151"/>
            </StatusBarItem>
            <StatusBarItem HorizontalAlignment="Right">
                <ProgressBar x:Name="ProgressBar" Width="150" Height="16" Visibility="Collapsed"/>
            </StatusBarItem>
        </StatusBar>

        <Border DockPanel.Dock="Bottom" Padding="14,8,14,8" Background="#FFFFFF" BorderBrush="#D8DEE6" BorderThickness="0,1,0,0">
            <StackPanel Orientation="Horizontal">
                <Button x:Name="InstallUpdatesButton" Content="安装更新项 (y)" MinWidth="130" Padding="10,6" Margin="0,0,10,0"/>
                <Button x:Name="InstallApplicableButton" Content="安装全部可安装 (a)" MinWidth="150" Padding="10,6" Margin="0,0,10,0"/>
                <Button x:Name="InstallSelectedButton" Content="安装选中项" MinWidth="110" Padding="10,6" Margin="0,0,10,0"/>
                <Button x:Name="DownloadSelectedButton" Content="仅下载选中项" MinWidth="110" Padding="10,6" Margin="0,0,10,0"/>
                <Button x:Name="OpenPlanButton" Content="打开计划文件" MinWidth="110" Padding="10,6"/>
            </StackPanel>
        </Border>

        <Grid Margin="14,10,14,10">
            <Grid.RowDefinitions>
                <RowDefinition Height="*"/>
                <RowDefinition Height="Auto"/>
                <RowDefinition Height="170"/>
            </Grid.RowDefinitions>
            <DataGrid x:Name="DriverGrid" Grid.Row="0" RowHeight="24" FontSize="12" Background="#FFFFFF" BorderBrush="#D8DEE6"/>
            <GridSplitter Grid.Row="1" Height="5" HorizontalAlignment="Stretch" Background="#E5E7EB"/>
            <TextBox x:Name="LogBox" Grid.Row="2" IsReadOnly="True" TextWrapping="Wrap"
                     VerticalScrollBarVisibility="Auto" HorizontalScrollBarVisibility="Auto"
                     FontFamily="Consolas" FontSize="12" Padding="6" Background="#0F172A"
                     Foreground="#D1D5DB" BorderBrush="#334155"/>
        </Grid>
    </DockPanel>
</Window>
'@
    $reader = New-Object System.IO.StringReader($xaml)
    $xmlReader = [System.Xml.XmlReader]::Create($reader)
    $window = [System.Windows.Markup.XamlReader]::Load($xmlReader)
    $xmlReader.Dispose()
    $reader.Dispose()

    foreach ($name in @('MachineText', 'OsCombo', 'IncludeBiosCheck', 'SkipHashCheckBox', 'RefreshButton',
        'InstallUpdatesButton', 'InstallApplicableButton', 'InstallSelectedButton', 'DownloadSelectedButton',
        'OpenPlanButton', 'DriverGrid', 'LogBox', 'StatusText', 'ProgressBar')) {
        $script:Ui[$name] = $window.FindName($name)
    }

    Add-DriverGridColumns -Grid $script:Ui['DriverGrid']
    $script:Ui['RefreshButton'].Add_Click({
        Start-LenovoDriverJob -Export -TargetOsId (Get-SelectedOsId)
    })
    $script:Ui['InstallUpdatesButton'].Add_Click({
        Start-InstallAction -Kind Updates
    })
    $script:Ui['InstallApplicableButton'].Add_Click({
        Start-InstallAction -Kind Applicable
    })
    $script:Ui['InstallSelectedButton'].Add_Click({
        Start-InstallAction -Kind Selected
    })
    $script:Ui['DownloadSelectedButton'].Add_Click({
        Start-InstallAction -Kind Selected -DownloadOnly
    })
    $script:Ui['OpenPlanButton'].Add_Click({
        $plan = Join-Path $env:TEMP 'lenovo_driver_plan.txt'
        if (Test-Path -LiteralPath $plan) {
            Start-Process -FilePath 'notepad.exe' -ArgumentList $plan
        } else {
            Add-GuiLog -Message '计划文件尚未生成。'
        }
    })

    return $window
}

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

function Export-SelfTestScreenshot {
    param(
        [System.Windows.Window]$Window,
        [string]$Path
    )
    $rows = @(
        [pscustomobject]@{ Selected = $true; DriverCode = 'DRV202109090051'; DriverName = 'Monitor Driver 显示器驱动程序'; Version = '20.21.0.1'; LocalVersion = '10.0.19041.5794'; CompareStatus = 'Update'; SourceAudit = 'Official list match'; FileName = 'Monitor.exe'; FilePath = ''; FileSize = '123 MB'; MD5 = ''; IsApplicable = $true; IsUpdate = $true }
        [pscustomobject]@{ Selected = $false; DriverCode = 'DRV202109280030'; DriverName = 'Lenovo Energy Management 联想电源管理驱动'; Version = '15.11.29.65'; LocalVersion = 'not installed'; CompareStatus = 'Not installed'; SourceAudit = 'Official list match'; FileName = 'Energy.exe'; FilePath = ''; FileSize = '45 MB'; MD5 = ''; IsApplicable = $true; IsUpdate = $false }
        [pscustomobject]@{ Selected = $false; DriverCode = 'DRV202102040000'; DriverName = 'Wlan Driver 无线网卡设备驱动'; Version = 'Intel_22.10.0.7'; LocalVersion = '23.100.0.4'; CompareStatus = 'Local newer'; SourceAudit = 'Local newer (source offline image integration)'; FileName = 'Wlan.exe'; FilePath = ''; FileSize = '80 MB'; MD5 = ''; IsApplicable = $true; IsUpdate = $false }
    )
    Set-LenovoDriverRows -Rows $rows
    $script:Ui['MachineText'].Text = '82JQ / PF2SBWJA  |  当前系统 Windows 10 64-bit (OSID 42)  |  当前列表 Windows 11 64-bit (OSID 248)'
    $comboItems = New-Object System.Collections.ObjectModel.ObservableCollection[object]
    $comboItems.Add([pscustomobject]@{ Label = '当前系统 (Windows 10 64-bit)'; OSID = $null }) | Out-Null
    $comboItems.Add([pscustomobject]@{ Label = 'Windows 11 64-bit (OSID 248)'; OSID = '248' }) | Out-Null
    $script:Ui['OsCombo'].ItemsSource = $comboItems
    $script:Ui['OsCombo'].SelectedIndex = 1
    $script:Ui['LogBox'].AppendText("[21:49:42] 已加载 23 行驱动列表，来自 Windows 11 64-bit`r`n")

    $window.Width = 1240
    $window.Height = 760
    $window.WindowStartupLocation = [System.Windows.WindowStartupLocation]::Manual
    $window.Left = 0
    $window.Top = 0
    $window.Show()
    $window.UpdateLayout()

    $width = [int]$window.ActualWidth
    $height = [int]$window.ActualHeight
    $bitmap = New-Object System.Windows.Media.Imaging.RenderTargetBitmap($width, $height, 96, 96, [System.Windows.Media.PixelFormats]::Pbgra32)
    $bitmap.Render($window)
    $encoder = New-Object System.Windows.Media.Imaging.PngBitmapEncoder
    $encoder.Frames.Add([System.Windows.Media.Imaging.BitmapFrame]::Create($bitmap)) | Out-Null
    $stream = [System.IO.File]::Open($Path, [System.IO.FileMode]::Create)
    try {
        $encoder.Save($stream)
    } finally {
        $stream.Dispose()
    }
    $window.Close()
}

if ($SelfTest) {
    $window = New-LenovoDriverWindow
    $shot = Join-Path $env:TEMP 'lenovo_driver_wpf_smoke.png'
    Export-SelfTestScreenshot -Window $window -Path $shot
    Write-Host $shot
    exit 0
}

if ($WorkerSmoke) {
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
$app = New-Object System.Windows.Application
$app.ShutdownMode = [System.Windows.ShutdownMode]::OnMainWindowClose
$app.Run($window) | Out-Null
