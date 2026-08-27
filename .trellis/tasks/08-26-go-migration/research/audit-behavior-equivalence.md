# Lenovo Driver Installer PowerShell 到 Go 行为等价审计

## 摘要

结论：**当前 Go 实现不能视为与 PS1 行为等价，迁移处于阻塞状态，不能按现状验收。**

已确认等价的部分主要是：
- CLI 参数面完整，`-Help` 与三种非法组合的退出码已对齐（实测 Go 二进制返回 `0/2`）。
- 参数互斥校验、GUI 驱动码不匹配返回 `3` 的逻辑已实现。
- 大量离线纯逻辑（版本解析/比较、硬件匹配、目标 OS 解析、选择 token 解析、计划文本、setupapi 解析、来源审计核心）在代码层面与 PS core 对应函数高度一致，PS core 86/86 测试通过。
- 下载大小容差、SHA-256 companion、官方 MD5、缓存复用和 `-SkipHashCheck` 的路径已接线。

存在三类阻塞问题：
1. `-LatestAcrossOS` 只解析参数，主流程没有拉取和合并其他 OS 列表。
2. 下载 403/URL 过期后的 `GetRefreshedDriverURL` 已实现但从未被调用。
3. EXE 超时路径没有进入 extracted fallback；EXE 失败 fallback 本身与 PS1 算法也不等价，且缺失交互式 `r` 重试。

此外，WPF 仍未切换到 Go，README/verify/结构收口也未完成。迁移阻塞等级：**Critical blockers**。

## 验证记录

- `pwsh -NoProfile -ExecutionPolicy Bypass -File .\lenovo_driver_core.tests.ps1`：`passed=86, failed=0`。
- `go version; go test ./...`：`go` 不在 PATH，`C:\Program Files\Go\bin\go.exe` 与 `%LOCALAPPDATA%\Programs\Go\bin\go.exe` 均不存在；**Go 单元测试未能运行**。
- `bin\lenovo-driver.exe` CLI 冒烟（不需要网络）：
  - `-Help` 退出码 `0`。
  - `-CurrentOSOnly -LatestAcrossOS`、`-CurrentOSOnly -TargetOS 248`、`-LatestAcrossOS -TargetOS 248` 均退出码 `2`。
  - `-UnknownFlag` 退出码 `2`；`-Help -CurrentOSOnly -LatestAcrossOS` 退出码 `0`（Help 优先）。
- 审计边界说明：探测 PS1 未知参数行为时，PowerShell 脚本忽略未知参数并进入主流程，发生了意外的 Lenovo API 调用；命令随即被终止，未留下残留进程，也未修改工作区。后续未再执行任何会进入主流程的 PS1/Go 命令。

## 对照矩阵

| # | 行为项 | PS1 位置 | Go 位置 | 状态 | 证据 |
|---|---|---|---|---|---|
| 1 | CLI 参数完整性 | `install_lenovo_drivers.ps1` 55-69 | `internal/app/app.go` 21-35, 72-84 | 等价 | 12 个 PS1 参数全部存在；额外 `-GuiExportPath/-GuiInstallCodes` 与 PS1 一致 |
| 2 | 参数互斥与退出码 | PS1 1345-1361 | `app.go` 99-126 | 等价 | 三种非法组合实测均为 2；Help 优先为 0 |
| 3 | GUI 码不匹配退出码 | PS1 1532-1537, 1600-1607 | `app.go` 229-234 | 等价 | 无匹配时均返回 3 |
| 4 | 交互 y/a/s/t/n | PS1 1303-1340 | `internal/app/interactive.go` 22-125 | 部分等价 | t 会重载 OS 列表；但驱动集合预览、无候选提示、下一 OS 文案不同 |
| 5 | 数据源与 OS 解析 | PS1 151-294 | `internal/api/client.go` 87-243, `internal/api/parse.go` | 部分等价 | QuickFix 优先/Web 回退一致；`findOSEntry` 首分支大小写敏感，web statusCode 未校验 |
| 6 | LatestAcrossOS | PS1 1429-1470 | `view.go` 30-121 | 缺失 | Go 仅解析参数，主流程只加载 `listOsID` 一个列表 |
| 7 | 下载完整性 | PS1 583-659, 1636-1723 | `internal/download/download.go`, `internal/app/install.go` 92-163 | 未接线 | 大小/MD5/SHA companion 已实现；403 刷新函数存在但无调用者 |
| 8 | 安装器 | PS1 881-1166 | `internal/install/install.go` | 缺失/部分等价 | MSI/INF/ZIP/CAB/EXE 基本策略存在；EXE 超时不走 fallback，失败 fallback 算法不同，缺 r 交互 |
| 9 | 来源审计 | PS1 core 539-751, shell 698-878 | `internal/audit/audit.go`, `internal/app/evidence.go` | 部分等价 | 字段与类别基本一致；CoreImport PackageDir 表示、alternate map 加载时机、JSON SourceAudit 值不同 |
| 10 | JSON 协议 | PS1 1258-1301 | `view.go` 209-291 | 部分等价 | 字段名兼容；`SourceAudit` 值格式与 PS1 不同；WPF 尚未消费 Go |
| 11 | 日志/计划/历史路径与格式 | PS1 83-85, 478-486, 664-694, core 753-785 | `app.go` 58-60, `view.go` 202-207, `history.go`, `plan.go` | 部分等价 | 路径/标题/表头一致；历史 CSV 写入格式不同且 BOM 兼容存在风险 |

## 发现列表

### Critical-1：`-LatestAcrossOS` 未实现跨 OS 合并

- 位置：`internal/app/view.go` 30-121；`internal/app/app.go` 208
- 问题：PS1 在 `install_lenovo_drivers.ps1` 1429-1470 先加载当前 OS 列表，再遍历 `$osList` 拉取所有其他 OS 列表，合并后按 `CurrentOsId=$sysId` 调 `Select-LatestDrivers`。Go `CompareOSDriverView` 只调用一次 `GetDriverObjects(ctx, categoryID, listOsID, "QuickFix")`，没有合并其他 OS。
- 证据：grep 显示 `opts.LatestAcrossOS` 只出现在参数定义、校验、Help 和 `interactive.go` 的 toggle 判断中；`view.go` 42-50 只有一个列表源。PRD 验收项 `prd.md` 34 明确要求“主流程实际拉取并合并其他 OS 列表”。
- 影响：`-LatestAcrossOS` 实际等同于当前 OS 模式，跨 OS 最新版本永远不会被选中。
- 建议：在 `CompareOSDriverView` 或独立加载函数中实现 PS1 的合并流程，并用多 OS fixture 验证 `SelectLatestDrivers` 选择跨 OS 最新版本。

### Critical-2：403/URL 过期刷新未接线

- 位置：`internal/api/client.go` 245-273；`internal/app/install.go` 92-163
- 问题：`GetRefreshedDriverURL` 已完整实现，但没有任何调用者。`downloadVerified` 只调用 `Downloader.DownloadWithRetry(ctx, driver.FilePath, outFile, 3)`，HTTP 403 会直接失败。
- 证据：grep `GetRefreshedDriverURL` 只有定义和注释；PS1 在 `install_lenovo_drivers.ps1` 1698-1707 收到 403 后刷新 URL 并重试一次。PRD 验收项 `prd.md` 35 明确要求接通。
- 影响：CDN URL 过期后，Go 会在 3 次重试后失败，无法像 PS1 一样刷新 URL。
- 建议：在 `downloadVerified` 捕获 HTTP 403/Forbidden 或下载失败后调用 `GetRefreshedDriverURL`，更新 `driver.FilePath` 并重试一次；用 `httptest` 覆盖。

### Critical-3：EXE 超时没有进入 extracted fallback

- 位置：`internal/install/install.go` 147-149
- 问题：PS1 在 EXE 超时后调用 `Invoke-ExtractedDriverFallback`（`install_lenovo_drivers.ps1` 1120-1131）。Go 在 `result.TimedOut` 时直接返回错误，`ExtractedDriverFallback` 只在退出码非 0 时被调用（`install.go` 157）。
- 证据：`install.go` 147-149 与 151-161 对比；README `README.md` 160-165 声称“stalls or fails”都会尝试 fallback。
- 影响：EXE 卡死时不会尝试提取 INF 或内部安装器，违反 README 安全/恢复契约，安装成功率下降。
- 建议：EXE 超时路径调用 `ExtractedDriverFallback`，按 PS1 逻辑返回 fallback 成功、失败或不可用的结果。

### High-1：EXE fallback 算法与 PS1 不等价，可能安装错误 INF

- 位置：`internal/install/install.go` 244-311
- 问题：PS1 先从本次 EXE 的 `DriverCode.log` 解析出唯一的 `is-*.tmp` 目录（`install_lenovo_drivers.ps1` 945-962），再在该目录中找 INF 或内部安装器；NVIDIA 才扫描全局 `TempInst`。Go 直接扫描 `SystemRoot\TempInst`、`%TEMP%\TempInst`、`%TEMP%` 下所有近期 `is-*.tmp` 目录，按修改时间排序后逐个安装其中的 INF。
- 证据：PS1 957-988 与 Go 245-310 对比；Go 不调用 `Get-TempDirFromLog` 等价逻辑；NVIDIA 内部安装器检查 `dir\setup.exe`/`dir\nvsetup.exe`，而不是 PS1 的 `dir\Display.Driver\setup.exe`；非 NVIDIA 内部安装器在全局扫描中不会被执行；内层安装器也没有使用 PS1 的 `InstallCode`/`InstallParameter` 参数。
- 影响：可能选中另一个无关 Inno 临时目录并安装不属于本次驱动的 INF；也可能漏掉本应使用的内部安装器。
- 建议：先按驱动日志定位提取目录；无日志证据时才按 PS1 的 NVIDIA 限制扫描；内部安装器补 InstallCode 参数和 `Display.Driver` 路径。

### High-2：EXE 静默失败后的交互式 `r` 重试缺失

- 位置：`internal/install/install.go` 157-161
- 问题：PS1 在 EXE 失败且 fallback 不可用时提示“Type r to run ... interactively, or s to skip”（`install_lenovo_drivers.ps1` 1144-1153），选择 `r` 会启动交互安装。Go 直接返回 `"silent install exit %d"`，没有提示。
- 证据：Go 的 `InstallDriverFile` 无 `Read-Host`/交互输入等价逻辑。
- 影响：用户无法在静默安装失败后选择交互式重跑，行为和帮助文档不一致。
- 建议：在 EXE 失败分支增加与 PS1 相同的 `r/s` 交互，或明确记录为有意的 CLI 差异并更新 README。

### High-3：Go 读取 PS1 生成的历史 CSV 存在 BOM 兼容风险

- 位置：`internal/app/history.go` 19-67；`install_lenovo_drivers.ps1` 686
- 问题：PS1 使用 `Set-Content -Encoding UTF8` 创建历史文件；仓库入口 `install_lenovo_drivers.bat` 使用 `powershell.exe`（Windows PowerShell 5.1），该编码会写 UTF-8 BOM。Go `encoding/csv.Reader` 不会自动剥离 BOM，第一列名会变成 `\ufeffTimestamp`，`ReadHistory` 的 header 索引可能找不到 `Timestamp`，导致全部行被过滤。
- 证据：`history.go` 34-45 用原样 header 建立索引；PS1 686-693 创建和追加历史文件；未能在本机实机验证（无历史文件、无 Go 工具链），但风险明确。
- 影响：Go 无法读取旧 PS1 历史时，来源审计的 “Install history” 证据和 `Local newer (source ...)` 标签会丢失。
- 建议：`ReadHistory` 对首个字段剥离 UTF-8 BOM，并增加带 BOM fixture 的回归测试。

### High-4：非管理员自动提权后的退出码丢失

- 位置：`internal/app/app.go` 131-138, 250-265
- 问题：PS1 在 1384-1385 使用 `Start-Process -Verb RunAs -Wait -PassThru` 后 `exit $p.ExitCode`。Go `RelaunchElevated` 构造 PowerShell 脚本后 `_ = cmd.Run()`，无条件返回 `true`，`Run` 随后返回 `0`。
- 证据：`app.go` 250-265；PS1 1383-1385。
- 影响：非管理员调用未加 `-Elevated` 时，即使提权后的 Go 子进程下载/安装失败或 UAC 被取消，父进程仍报告退出码 `0`。
- 建议：捕获子进程退出码并返回；提权取消/失败时按 PS1 行为返回失败而不是成功。

### Medium-1：交互提示没有打印“y/a 精确驱动集合”

- 位置：`internal/app/interactive.go` 127-129
- 问题：PS1 `Show-DriverActionPreview`（`install_lenovo_drivers.ps1` 1232-1247）会逐条打印 DriverCode、DriverName、remote、local、status。Go 只打印 `"  y = update-only (2)"` 这样的计数。
- 证据：`README.md` 41-43 明确承诺提示前打印精确驱动集合；Go `showActionPreview` 只有一行计数。
- 影响：用户无法在选择前看到每个驱动的完整集合，CLI 可观测性不一致。
- 建议：按 PS1 输出逐条预览，并补交互测试。

### Medium-2：无可用候选与 toggle 文案/状态处理不一致

- 位置：`internal/app/interactive.go` 38-65, 102-124, 156-162
- 问题：PS1 在无 applicable 且不可切换时直接退出 0（`install_lenovo_drivers.ps1` 1584-1598）；可切换时只提示 `t/n`。Go 始终进入通用 `a/s/t/n` 提示。另外 Go `nextOSLabel` 只返回 OSName，PS1 返回 `"OSName (OSID)"`；Go 在 toggle 加载失败时已经把 `currentListOsID` 改成新值但 `currentView` 仍是旧列表，状态可能不一致。
- 证据：PS1 1311-1316, 1584-1598；Go `promptSelection` 与 `nextOSLabel`。
- 影响：提示文本、无候选退出路径和失败后的选择状态不等价。
- 建议：按 PS1 分支处理零候选，恢复失败时保留原 OS 状态，标签补 OSID。

### Medium-3：JSON 的 `SourceAudit` 字段值不逐字段兼容

- 位置：`internal/app/view.go` 267-270；`install_lenovo_drivers.ps1` 1276
- 问题：PS1 对 `SourceAudit` 执行 `[string]$d.SourceAudit`，实测 PowerShell 5.1/7 输出类似 `@{Category=Install history; Summary=...; EvidenceLines=System.Object[]}`。Go 输出 `"Install history: ..."` 的拼接字符串。
- 证据：本地 PowerShell 实测：`PSObjectString=[@{Category=Install history; Summary=...; EvidenceLines=System.Object[]}]`；Go `sourceAudit = driver.SourceAudit.Category + ": " + driver.SourceAudit.Summary`。
- 影响：WPF 当前能解析（它只做 `[string]`），但 JSON fixture 逐字段对比会失败；若 WPF 后续依赖 PS1 格式会受影响。
- 建议：若目标是逐字段兼容，应复刻 PS1 的字符串化格式；或明确更新 WPF 协议和契约测试。

### Medium-4：alternate source map 加载时机与 PS1 不同

- 位置：`internal/app/view.go` 77-80；`install_lenovo_drivers.ps1` 1487-1491
- 问题：PS1 仅在 `CompareStatus -eq 'Local newer'` 且尚未缓存时才初始化 alternate source map。Go 对任意 `LocalVersion != ""` 的 driver 都会初始化，导致额外拉取所有其他 OS 列表，并可能影响 `Local newer` 标签的可用性。
- 证据：`view.go` 77-80；PS1 1487-1491。
- 影响：额外网络请求、API 暂时不可用时与 PS1 的来源审计结果可能不同。
- 建议：按 PS1 的惰性初始化时机实现，或增加可解释的等价 fixture。

### Medium-5：OS 名称解析存在大小写与 web statusCode 差异

- 位置：`internal/api/client.go` 192-204, 226-243
- 问题：PS1 `Find-OsEntry` 的 `-like`/`-match` 默认不区分大小写；Go `findOSEntry` 第一分支 `strings.Contains(entry.OSName, osName)` 区分大小写。另外 PS1 `Invoke-LenovoApi` 对非 `200` statusCode 抛错（`install_lenovo_drivers.ps1` 158-162），Go 的 OS 列表解析只检查 `OSList` 非空，不校验 web `statusCode`。
- 证据：`client.go` 226-233；PS1 252-261。
- 影响：API 返回大小写不同的 OS 名称或带错误 statusCode 的正文时，Go 可能解析到错误 OS 入口。
- 建议：`findOSEntry` 首分支改为 `EqualFold`/`ContainsFold`，web 响应解析先校验 statusCode。

### Medium-6：WPF 仍未切换到 Go 主路径

- 位置：`lenovo_driver_wpf.ps1` 38；`implement.md` 38-42
- 问题：WPF 的 `$script:InstallerPath` 仍指向 `install_lenovo_drivers.ps1`，没有改为 `lenovo-driver.exe`；README 64-65 也仍描述“starts the existing PowerShell installer”。
- 证据：`lenovo_driver_wpf.ps1` 38；`implement.md` Phase 3。
- 影响：验收项“WPF 默认消费 Go JSON 与 `-GuiInstallCodes`”未完成，PS1 仍是实际业务入口。
- 建议：完成 WPF 子进程切换，并以 `-WorkerSmoke` 或等价 Go JSON smoke 验证。

### Low-1：未知/位置参数行为不同

- 位置：`internal/app/app.go` 85-90
- 问题：Go 对未知 flag 和位置参数返回退出码 `2`；PS1 脚本实测会忽略未知命名参数并继续运行（本次审计中因此误触网络）。
- 影响：不在 PRD 明确契约内，但 CLI 行为并非逐字等价；Go 更严格通常更安全。
- 建议：记录为有意差异，或在 README 中说明。

### Low-2：安装器/计划输出的旁路信息不同

- 位置：`internal/install/install.go`；`internal/app/view.go` 89-101
- 问题：Go 没有输出 PS1 的 BIOS/EC 跳过、非可安装文件跳过、pnputil stdout/stderr 日志、表格颜色、状态摘要颜色等信息；`Write-HashCompanion`/历史 CSV 格式也不同。
- 证据：PS1 515-526, 918-923, 453-476；Go `filterDriverRows`、`RunPnPUtilWithTimeout`、plan 输出。
- 影响：用户可见输出与日志审计文本不等价，但不影响选择/安装核心结果。
- 建议：按验收需要决定保留哪些文本契约，并为 pnputil 日志补写。

### Low-3：结构收口项未完成

- 位置：`internal/compare/compare.go` 695 行；`internal/app/evidence.go`、`internal/audit/audit.go`、`internal/api/parse.go` 中重复 path 工具；`.gitignore`；`scripts/verify.ps1`
- 问题：`implement.md` Phase 1/4 要求 `scripts/verify.ps1`、`.gitignore` 覆盖 `bin/*.exe`、`sessionlogs/`、`nul`、Go 构建产物，并拆分 `compare.go` 与合并 path 工具；当前均未完成。
- 证据：`scripts` 目录不存在；`.gitignore` 只有 9 行；`compare.go` 仍 695 行；仓库中存在 `bin\lenovo-driver.exe`、`sessionlogs`、`nul`。
- 影响：验收脚本无法单命令执行，构建产物会被误提交，维护性收口未达成。
- 建议：按 implement.md 完成结构收口，但不改变本次审计发现的行为逻辑。

## 测试覆盖差距清单

PS core 共 86 个断言，按区域对照 Go 现有测试：

| PS 区域 | PS 断言数 | Go 现有测试 | 差距 |
|---|---|---|---|
| 版本解析/状态比较 | 6 | `TestParseVersionString`、`TestVersionCompare`、`TestCompareDriverStatus` | 缺少 `Remote=''`、`Provisioned`、真实 vendor 组件匹配、版本片段提取、不同 part 数比较 |
| 硬件匹配/适用性 | 5 | `TestHardwareMatchFunction`、`TestDriverApplicableFunction` | 缺少 Realtek 适用、缺卡/缺音频等分支 |
| 本地设备/软件版本解析 | 8 | 无 | 缺少 `GetMatchingLocalDevices` 数量、软件套件忽略 Wi-Fi、AMD Power Provisioned、Fn service version 等 |
| 来源标签 | 5 | `TestGetSourceMapMatch`、`TestResolveExternalDriverSourceLabel` | 缺少 history/verified/same OS/unchanged 四种 `ResolveDriverSourceLabel` 场景 |
| setupapi 日志解析 | 30 | `TestConvertFromImportLogText`（1 个场景、部分字段） | 缺少 offline、dev、AMD CopyINF、NVIDIA CopyINF、Device Install、完整字段/命令断言 |
| 来源证据 | 5 | `TestResolveDriverSourceEvidence` | 缺少 offline category/summary/import evidence、history beats import |
| 计划/表格/汇总/选择 | 11 | `TestBuildPlanText`、`TestFormatDriverTableLines`、`TestFormatStatusSummaryLines`、`TestParseDriverSelectionTokens` | 缺少 plan source audit、md5 空值、summary Local newer note |
| Target OS | 5 | `TestResolveTargetOsEntry` | 缺少大小写、无匹配、空输入 |
| SelectLatestDrivers | 5 | `TestSelectLatestDrivers` | 缺少 current OS 保留、跨 OS 最新、version beats issue date |
| 下载/安装纯助手 | 5 | `TestConvertToBytes`、`TestFileSizeMatchFunction` | 缺少 reboot exit code 3010/1641 等 |
| 驱动列表解析 | 4 | `TestParseQuickFix`、`TestParseWeb` | 已基本覆盖，但缺少 FileName pathBase、InstallParameter fallback 等边界 |

Go 还缺少以下迁移行为测试：
- CLI：三个非法组合、Help 优先级、GUI mismatch 退出码 3、未知参数。
- JSON：`ExportGUIView` 字段与 PS1 `Export-LenovoDriverViewJson` 逐字段 fixture。
- 历史：CSV BOM、读旧 PS1 文件、写后回读。
- 下载：403 刷新、MD5 失败后重试、缓存校验、SkipHashCheck。
- 安装：MSI/EXE/INF/ZIP/CAB 分发、超时与进程树 kill、3010/1641、EXE timeout fallback、fallback 目录定位。
- 交互：y/a/s/t/n、s 解析、t 重载、零候选。
- LatestAcrossOS 合并。
- 提权子进程退出码。

## 迁移风险排序

1. `-LatestAcrossOS` 未实现：PRD 明确验收项，直接阻塞迁移。
2. 403/URL 刷新未接线：PRD 明确验收项，影响下载可靠性。
3. EXE 超时 fallback 缺失：README 安全契约与安装成功率的关键行为。
4. EXE fallback 目录/内部安装器算法不等价：有安装错误驱动风险。
5. 历史 CSV BOM 兼容风险：跨 PS1/Go 历史证据可能丢失。
6. 提权子进程退出码丢失：错误可被掩盖为成功。
7. EXE 失败交互式 `r` 缺失：CLI 交互契约不完整。
8. 交互预览/零候选/toggle 文案差异：用户可见行为不一致。
9. JSON `SourceAudit` 值差异：协议字段兼容但值不等价。
10. OS 解析大小写/statusCode 校验：真实 API 边界风险。
11. WPF 未切 Go、README/verify/.gitignore/结构收口未完成：迁移收口未达验收状态。

## 与现有 implement.md 的差异/补充

- `implement.md` Phase 2 第 2/3 项声称“修 LatestAcrossOS”和“接 403 URL 刷新”，但代码中两项均未完成。
- Phase 2 第 4 项 CLI 契约：三种非法组合和 Help 已实测通过，但 Go 测试只覆盖其中一种；未知 flag 行为与 PS1 不同，需记录为差异。
- Phase 2 第 5 项 JSON 协议契约测试：未实现，且 `SourceAudit` 值已发现不兼容。
- Phase 3 WPF 切换：未实现，WPF 仍调用 PS1。
- Phase 4 结构收口：`compare.go` 仍 695 行，path 工具重复，未拆分/合并。
- Phase 5 文档与规范：README 仍以 PS1 为主路径，`scripts/verify.ps1` 不存在。
- Phase 6 验收：`go build/test/vet` 因工具链缺失无法执行；`.gitignore` 未覆盖构建产物。
- 补充风险：历史 CSV BOM、提权退出码、EXE fallback 安全性、OS 名称大小写、alternate map 加载时机均未在 implement.md 中显式列出。
