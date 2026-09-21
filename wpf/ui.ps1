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

    $evidence = New-Object System.Windows.Controls.DataGridTextColumn
    $evidence.Header = '证据分级'
    $evidence.Width = 100
    $evidence.Binding = New-Object System.Windows.Data.Binding -ArgumentList 'EvidenceBasis'
    $Grid.Columns.Add($evidence) | Out-Null

    $problem = New-Object System.Windows.Controls.DataGridTextColumn
    $problem.Header = '设备问题'
    $problem.Width = 160
    $problem.Binding = New-Object System.Windows.Data.Binding -ArgumentList 'DeviceProblem'
    $Grid.Columns.Add($problem) | Out-Null

    $nonMatch = New-Object System.Windows.Controls.DataGridTextColumn
    $nonMatch.Header = '不适用原因'
    $nonMatch.Width = 260
    $nonMatch.Binding = New-Object System.Windows.Data.Binding -ArgumentList 'NonMatchReason'
    $Grid.Columns.Add($nonMatch) | Out-Null
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

function New-LenovoDriverWindow {
    $xamlPath = Join-Path $PSScriptRoot 'window.xaml'
    $xaml = Get-Content -LiteralPath $xamlPath -Raw
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
        if ($env:LOCALAPPDATA) {
            $plan = Join-Path $env:LOCALAPPDATA 'Lenovo\DriverInstaller\lenovo_driver_plan.txt'
        } else {
            $plan = Join-Path $env:TEMP 'lenovo_driver_plan.txt'
        }
        if (Test-Path -LiteralPath $plan) {
            Start-Process -FilePath 'notepad.exe' -ArgumentList $plan
        } else {
            Add-GuiLog -Message '计划文件尚未生成。'
        }
    })

    return $window
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
