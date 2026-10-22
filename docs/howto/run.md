# 构建与运行

“我要跑起来 / 我要审计一台机器 / 我遇到问题”都看这里。运行参数见 `-Help`（机器源 `internal/app/help.go`）；退出码与产物的契约定义见 `docs/spec/contracts.md`。

构建 / 门禁 / 内循环命令的权威清单在 `docs/howto/develop.md`；本文件只保留“刚克隆完、只想先跑起来”的最短路，不重复那套命令表。

## 构建

仓库根目录：

```powershell
go build ./...
go test ./...
go vet ./...
gofmt -w cmd internal
go build -o bin\lenovo-driver.exe .\cmd\lenovo-driver
```

CLI 启动器在 `bin\lenovo-driver.exe` 缺失**或比源码旧**时自动重建（`scripts\lib\ensure-engine.bat`，两个启动器共用同一份判定）。没有 Go 工具链可重建时，启动器打印两行 WARNING 说明本次跑的是旧产物——不再静默使用陈旧二进制。

## 运行

```bat
install_lenovo_drivers.bat
```

正常运行时先打印 `y` / `a` 两套驱动集（驱动 code、名称、远端版本、本地版本、状态），再提示选择：

- `y` 只安装 `Update` 驱动。
- `a` 安装自动安装集（`Update` + `Not installed`）——`Local newer`、`Up to date`、`Not applicable` 不在其中（见 `docs/spec/invariants.md` §3）。
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

## 桌面界面（mygo 原生 UI）

```bat
bin\lenovo-driver.exe -Gui
```

桌面窗口与 CLI 共用同一个引擎二进制（`-Gui` 分发到 `internal/gui`）：启动时非管理员会先经一次 UAC 提权重启自身，然后嵌入引擎加载官方驱动列表，显示相同的 `Update` / `Up to date` / `Not installed` / `Local newer` / `Not applicable` 状态与来源审计标签。按钮对应 CLI 选择：刷新驱动列表、安装更新项 (y)、安装需更新项 (a)、安装选中项、仅下载选中项、打开计划文件。其中 (a) 装的是 CLI 的自动安装集（`Update` + `Not installed`），不是「全部可安装项」。启动前先构建引擎：`go build -o bin\lenovo-driver.exe .\cmd\lenovo-driver`。

## 参数

完整参数、退出码、产物路径的机器源是 `internal/app/help.go`，运行 `-Help` 查看最新清单。本文不复制参数表，避免与代码漂移。

`-Help` 未明确、但影响用法的约束：
- `-CurrentOSOnly`、`-LatestAcrossOS`、`-TargetOS` 互斥；传冲突组合以退出码 `2` 快速失败。
- `-Audit <snapshotPath>` 是独立只读模式（设备快照基线/差分），不能与安装、回退、导出、试运行组合。

## 排障

- **机型查询失败**：`install_lenovo_drivers.bat -Model "82JQ"` 显式指定。
- **找不到当前 OS 条目**：确认机型正确且联想 API 返回 OS 列表；不要用 `-LatestAcrossOS` 盲绕。
- **静默安装器失败**：日志路径已打印；用 `s` 跳过，或从 `%TEMP%\LenovoDrivers` 交互运行下载文件。
- **装后仍报旧版本**：装后复核记 `unchanged`；可能需重启。
- **下载返回 `403`**：从当前驱动列表刷新 URL 后重试一次。

## 干净检出复现

1. 安装官方 Go 1.27（或更新）工具链。跑门禁时版本要与 `go.mod` 的 `toolchain` 指令一致（`GOTOOLCHAIN=auto` 默认即会切到该版本，无需手工安装）。
2. 克隆/复制仓库到 Windows 机器。
3. 仓库根目录打开 PowerShell。
4. `.\scripts\verify.ps1`。
5. `go build -o bin\lenovo-driver.exe .\cmd\lenovo-driver`。
6. `.\install_lenovo_drivers.bat -DryRun` 验证机型/API 解析（不下载不安装）。
7. `.\bin\lenovo-driver.exe -Gui` 验证桌面窗口（连真实 API；会请求 UAC 提权）。
8. 实际安装：管理员 shell 里跑 `.\install_lenovo_drivers.bat`。
