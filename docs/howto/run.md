# 构建与运行

“我要跑起来 / 我要审计一台机器 / 我遇到问题”都看这里。参数与退出码的契约定义见 `docs/spec/contracts.md`。

## 构建

仓库根目录：

```powershell
go build ./...
go test ./...
go vet ./...
gofmt -w cmd internal
go build -o bin\lenovo-driver.exe .\cmd\lenovo-driver
```

CLI 启动器在 `bin\lenovo-driver.exe` 缺失时自动构建。

## 运行

```bat
install_lenovo_drivers.bat
```

正常运行时先打印 `y` / `a` 两套驱动集（驱动 code、名称、远端版本、本地版本、状态），再提示选择：

- `y` 只安装 `Update` 驱动。
- `a` 安装全部适用驱动。
- `s` 手工选编号，如 `1,3,5`。
- `t` 在支持的 OS 列表间切换。
- `n` 取消。

dry-run 不下载不安装：

```bat
install_lenovo_drivers.bat -DryRun
```

查看另一个支持 OS 的官方列表（不合并）：

```bat
install_lenovo_drivers.bat -DryRun -TargetOS 248
install_lenovo_drivers.bat -DryRun -TargetOS "Windows 11"
```

跨 OS 比较更新版本（实验模式）：

```bat
install_lenovo_drivers.bat -LatestAcrossOS
```

## 桌面界面（WPF）

```bat
install_lenovo_drivers_wpf.bat
```

WPF 窗口通过引擎加载官方驱动列表，显示相同的 `Update` / `Up to date` / `Not installed` / `Local newer` / `Not applicable` 状态与来源审计标签。按钮对应 CLI 选择：刷新列表、安装更新项（y）、安装全部（a）、安装选中项、仅下载选中项。启动前先构建引擎：`go build -o bin\lenovo-driver.exe .\cmd\lenovo-driver`。

## 参数

| 参数 | 含义 |
| --- | --- |
| `-DryRun` | 只对比，不下载不安装。 |
| `-CurrentOSOnly` | 只用当前 OS 列表（默认）。 |
| `-LatestAcrossOS` | 允许其它 OS 条目的更新版本；不能与 `-CurrentOSOnly` 同用。 |
| `-TargetOS <OSID\|OSName>` | 只对比一个支持 OS 列表，如 `248` 或 `Windows 11`；不能与 `-CurrentOSOnly`/`-LatestAcrossOS` 同用。 |
| `-SkipHashCheck` | 跳过本地 SHA-256 伴生校验。 |
| `-SkipSignatureCheck` | 跳过 Authenticode 签名校验。 |
| `-IncludeBios` | 包含固件包（BIOS/EC/ME/TPM/Thunderbolt/UEFI）；默认跳过。 |
| `-DownloadOnly` | 只下载不安装。 |
| `-DownloadDir <path>` | 覆盖下载目录；默认 `%TEMP%\LenovoDrivers`。 |
| `-Model <model>` | 覆盖自动机型查询，如 `82JQ`。 |
| `-Elevated` | 跳过提权；WPF 包装内部使用。 |
| `-GuiExportPath <path>` | 写 WPF 兼容 JSON 视图后退出。 |
| `-GuiInstallCodes <codes>` | 只安装逗号分隔的驱动 code（来自 GUI 导出）。 |
| `-Rollback <codes>` | 回退有 pending offer 的驱动：回退驱动程序，或重装旧 INF；绝不 `pnputil /delete-driver` 包清理。 |
| `-Help` | 显示用法。 |

`-CurrentOSOnly`、`-LatestAcrossOS`、`-TargetOS` 互斥；传冲突组合以退出码 `2` 快速失败。

## 排障

- **机型查询失败**：`install_lenovo_drivers.bat -Model "82JQ"` 显式指定。
- **找不到当前 OS 条目**：确认机型正确且联想 API 返回 OS 列表；不要用 `-LatestAcrossOS` 盲绕。
- **静默安装器失败**：日志路径已打印；用 `s` 跳过，或从 `%TEMP%\LenovoDrivers` 交互运行下载文件。
- **装后仍报旧版本**：装后复核记 `unchanged`；可能需重启。
- **下载返回 `403`**：从当前驱动列表刷新 URL 后重试一次。

## 干净检出复现

1. 安装官方 Go 1.27（或更新）工具链。
2. 克隆/复制仓库到 Windows 机器。
3. 仓库根目录打开 PowerShell。
4. `.\scripts\verify.ps1`。
5. `go build -o bin\lenovo-driver.exe .\cmd\lenovo-driver`。
6. `.\install_lenovo_drivers.bat -DryRun` 验证机型/API 解析（不下载不安装）。
7. `.\lenovo_driver_wpf.ps1 -WorkerSmoke -NoElevation` 验证 WPF JSON 桥（连真实 API）。
8. 实际安装：管理员 shell 里跑 `.\install_lenovo_drivers.bat` 或 `.\install_lenovo_drivers_wpf.bat`。
