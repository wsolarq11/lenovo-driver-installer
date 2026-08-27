# 可维护性审计报告：Lenovo Driver Installer

## 摘要

总体结论：当前仓库处于“PS1 可运行、Go 半成品”的双轨状态，代码结构、文档和验收标准尚未收口。按可维护性评级为 **D（较差，急需收口）**。Go 包边界方向基本正确（`api/model/inventory/compare/audit/plan/download/install/app`），但关键功能未接通、大量错误被吞掉、重复实现明显、测试覆盖不足；PS1 仍是实际可运行路径且 WPF 仍调用 PS1，与 PRD 的“Go 拥有业务逻辑”目标差距较大。这不是纯“代码风格”问题，而是迁移任务的核心完成度问题。

审计为只读审计。`go` 命令在本机不存在（`go: The term 'go' is not recognized...`），未运行 `go test`；未修改除本报告外的任何文件。

## 事实清单

- Go 工程文件全部为 untracked：`go.mod`、`cmd/`、`internal/`、`.trellis/tasks/08-26-go-migration/*` 均未提交（`git status --porcelain`）。
- `bin/lenovo-driver.exe`（约 9.95 MB）、`sessionlogs/2026-08-26.md`、根目录 `nul`（67 字节）均为未跟踪文件。
- `.gitignore` 共 9 行，仅覆盖 `*.log`、`lenovo_driver_plan.txt`、`install-state.json` 等，未覆盖 `bin/*.exe`、`sessionlogs/`、`nul`、Go 构建产物。
- 行数（按 `Get-Content` 统计）：
  - `internal/compare/compare.go` 695 行；
  - `internal/install/install.go` 343 行；
  - `internal/audit/audit.go` 338 行；
  - `internal/app/view.go` 292 行；
  - `internal/app/app.go` 281 行；
  - `install_lenovo_drivers.ps1` 1791 行；
  - `lenovo_driver_core.ps1` 909 行；
  - `lenovo_driver_wpf.ps1` 565 行；
  - `lenovo_driver_core.tests.ps1` 454 行。
- 函数体行数明显超标的示例（按源码函数体近似统计）：
  - `App.Run` 135 行（`internal/app/app.go:113-248`）；
  - `ConvertFromImportLogText` 103 行（`internal/audit/audit.go:63-166`）；
  - `TestDriverApplicable` 97 行（`internal/compare/compare.go:173-270`）；
  - `InstallDriverFile` 71 行（`internal/install/install.go:98-169`）；
  - `ExtractedDriverFallback` 66 行（`internal/install/install.go:245-311`）。
- `lenovo_driver_core.tests.ps1` 顶层 `Assert-*` 调用共 86 处，与 `implement.md:32` “PS core 86 个测试”一致。
- `scripts/verify.ps1` 不存在，`scripts/` 目录不存在。
- `README.md` 全文未出现 `Go`、`lenovo-driver`、`scripts/verify`、迁移等词，仍把 `install_lenovo_drivers.bat` 描述为唯一入口（`README.md:7`）。
- `.trellis/spec/backend/*` 仍只描述 PowerShell 三文件结构，声称“There are no packages, workspaces, or build artifacts”（`directory-structure.md:11-12`），未更新 Go 包结构。
- `lenovo_driver_wpf.ps1:38` 仍设置 `InstallerPath = install_lenovo_drivers.ps1`，并在 `Start-LenovoDriverJob`（`lenovo_driver_wpf.ps1:268-343`）启动 PowerShell 子进程，未消费 Go。
- 安装器回滚基线的既有缺陷：`lenovo_driver_core.ps1:238` 的 `Get-MatchingRemoteComponent` 参数是 `-Remote/-Vendor`，而 `install_lenovo_drivers.ps1:1763` 以 `-Driver/-VersionText` 调用；在真实后置校验路径中该调用不会按预期解析包版本。Go 中对应逻辑为 `compare.GetMatchingRemoteComponent`（`internal/compare/compare.go:317`）。
- `internal/api/client.go:245-273` 定义了 `GetRefreshedDriverURL`，但全库无调用点；Go 下载路径只调用 `DownloadWithRetry`（`internal/app/install.go:137`）。
- Go 主流程中没有跨 OS 合并逻辑：`internal/app/view.go:42-50` 只拉取 `listOsID` 一个 OS 的列表，随后 `SelectLatestDrivers` 在该单一列表内选最新；`-LatestAcrossOS` 仅影响交互开关（`internal/app/interactive.go:38`）和帮助文本（`internal/app/help.go:16`）。
- 路径工具重复实现：
  - `internal/api/parse.go:182-188` `pathBase`；
  - `internal/app/evidence.go:137-153` `pathBase`/`pathParent`；
  - `internal/audit/audit.go:301-316` `pathParent`/`pathBase`。
- PowerShell 可执行文件路径重复：`internal/inventory/windows.go:14` 定义 `powershellExe`，`internal/app/helpers.go:43` 又硬编码同一路径。
- `internal/compare/compare.go` 有 60+ 处函数内 `regexp.MustCompile`，其中 `TestDriverApplicable`、`GetDeviceVendor`、`GetRemoteComponentVendor` 等循环/高频路径每次调用都重新编译。
- `internal/app/install.go:223-225` 的 `normalizeSourceString`、`internal/compare/compare.go:694-695` 的 `EarliestTime` 均无调用点。
- `internal/app/app_test.go` 只有 3 个测试，仅覆盖 `-Help`、1 个非法参数组合和 `selectByCodes`；`implement.md:61` 要求 3 个非法组合均 exit 2。
- 高风险纯逻辑缺少 Go 测试：`install` 包无测试；`inventory` 的 PowerShell 包装无测试；`CompareOSDriverView`/跨 OS、URL 刷新、源映射、GUI JSON 导出无测试。
- 本机未安装 Go，无法执行 `go test ./...`、`go vet ./...`、`go build ./...`。

## 发现列表

### Critical

#### C1. `-LatestAcrossOS` 未实现，PRD 核心验收项缺失
- 位置：`internal/app/view.go:42-50`、`internal/app/app.go:24/74/188`、`internal/app/interactive.go:38`
- 问题：参数被解析和校验，但主流程从未加载其他 OS 列表，也从未把其他 OS 的 driver 行合并后再 `SelectLatestDrivers`。
- 证据：`CompareOSDriverView` 只调用一次 `GetDriverObjects(ctx, categoryID, listOsID, "QuickFix")`；`view.go:79` 的 `initAlternateSourceMap` 拉取其他 OS 列表仅用于来源审计，不参与选择；PS1 基线在 `install_lenovo_drivers.ps1:1429-1470` 会显式拉取每个 alt OS 并合并后选择。
- 影响：`-LatestAcrossOS` 是 PRD 验收条件（`prd.md:34`）和 README/帮助中宣称的实验模式，当前行为与文档不符；跨 OS 驱动选择不会发生。
- 建议：在 `CompareOSDriverView` 增加明确的数据合并层：按模式合并 `sysID + 全部 alt OS` 的 rows，过滤后统一调用 `SelectLatestDrivers`；为合并与选择写 fixture 测试；删除或重命名只做审计的 `initAlternateSourceMap`，避免与选择逻辑混淆。

#### C2. 403/URL 刷新逻辑是死代码，下载重试契约未接通
- 位置：`internal/api/client.go:245-273`、`internal/app/install.go:103-162`
- 问题：`GetRefreshedDriverURL` 存在但从未被调用；`downloadVerified` 的循环没有 URL 刷新分支，也没有对 403/过期 URL 的专项处理。
- 证据：全库 grep `GetRefreshedDriverURL` 只有定义；`install.go:137` 直接 `DownloadWithRetry(ctx, driver.FilePath, ...)`；PS1 基线在 `install_lenovo_drivers.ps1:1695-1714` 对 `403|Forbidden` 调用 `Get-RefreshedDriverUrl` 后重试。
- 影响：PRD 验收项“下载遇到 403/URL 过期时使用 `GetRefreshedDriverURL` 刷新后重试”（`prd.md:35`）未满足；运行中遇到 CDN 过期只能失败。
- 建议：把“检查刷新 URL 并重试”接到 `downloadVerified` 的循环中；`GetRefreshedDriverURL` 改为返回 `(string, error)` 并记录失败原因；补 403 fixture 测试。

#### C3. WPF 仍调用 PS1，Go 不是实际运行路径
- 位置：`lenovo_driver_wpf.ps1:38`、`lenovo_driver_wpf.ps1:268-343`
- 问题：WPF 后台 worker 仍启动 `install_lenovo_drivers.ps1`，与 `implement.md` Phase 3 “把 WPF 子进程从 install_lenovo_drivers.ps1 改为 lenovo-driver.exe”相反。
- 证据：`$script:InstallerPath = Join-Path $PSScriptRoot 'install_lenovo_drivers.ps1'`；`Start-LenovoDriverJob` 构造 `& '$escapedScript' ...` 的 powershell 命令。
- 影响：用户默认 GUI 路径仍是 PS1 业务逻辑；Go 即使完成也处于未消费状态，双轨负担无法消除。
- 建议：按 implement.md Phase 3 切换 worker 为 `lenovo-driver.exe -GuiExportPath / -GuiInstallCodes`；保留 PS1 仅作回滚，并在 README 标注实际默认路径。

#### C4. README 与 Trellis spec 仍是旧 PowerShell 架构，无法指导维护
- 位置：`README.md:3-7/209-239`、`.trellis/spec/backend/index.md:9-13`、`.trellis/spec/backend/directory-structure.md:11-12`
- 问题：README 称“A PowerShell installer”“the `.bat` wrapper remains the only user entry point”；spec 声称仓库“no packages, workspaces, or build artifacts”，与当前 `go.mod`、11 个 Go 包、`bin/lenovo-driver.exe` 直接矛盾。
- 证据：`README.md` grep 无 `Go`/`lenovo-driver`；`directory-structure.md:11-12` 原文“There are no packages, workspaces, or build artifacts”。
- 影响：维护者按文档会继续改 PS1、不会运行 Go 验证；迁移任务的“单一路径”目标不可持续。
- 建议：README 标注 Go 为主路径、PS1 冻结基线、`scripts/verify.ps1` 验证入口；spec 新增/更新 Go 层目录结构、验证命令、冻结策略；删除与事实不符的“no packages”表述。

### High

#### H1. 文件与函数规模系统性违反仓库约束
- 位置：`internal/compare/compare.go`（695 行）、`install_lenovo_drivers.ps1`（1791 行）、`lenovo_driver_core.ps1`（909 行）、`lenovo_driver_wpf.ps1`（565 行）；函数 `App.Run`（135 行）、`ConvertFromImportLogText`（103 行）、`TestDriverApplicable`（97 行）、`InstallDriverFile`（71 行）等。
- 问题：多个文件超过 500 行、多个函数超过 20 行，违反 `AGENTS.md` 的“Functions: 4-20 lines / Files: under 500 lines”和 implement.md 的拆分要求。
- 证据：见“事实清单”行数与函数统计。
- 影响：单函数承担“解析 + 决策 + 输出 + 副作用”多职责，改动风险高；`App.Run` 已有 12 个连续分支，新增参数/退出码时极易遗漏。
- 建议：按 implement.md Phase 4 先拆 `compare.go`；把 `App.Run` 拆为 `resolveMachine/Category/OS`、`resolveInventory`、`runComparison`、`runSelection`、`runInstall` 等 10-30 行步骤；冻结期不重写 PS1，但要为 Go 拆分建立等价测试。

#### H2. 历史记录/日志/清理错误被静默吞掉，审计链不可信
- 位置：`internal/app/install.go:57/66/70/74/219`、`internal/app/helpers.go:25`、`internal/app/history.go:78-80`、`internal/app/evidence.go:25-27`
- 问题：所有 `WriteHistoryRecord` 调用都忽略返回错误；`Log` 忽略 `appendLine` 错误；CSV 头写入/关闭错误被丢弃；setupapi 日志读取失败直接 `continue`。
- 证据：`install.go` 5 处 `_ = a.WriteHistoryRecord(...)`；`history.go:78-80` `_ = writer.Write(...)`、`_ = f.Close()`；`helpers.go:25` `_ = appendLine(...)`。
- 影响：驱动器安装/校验的审计记录可能缺失，日志文件可能写失败但用户仍看到“成功”；违反“失败显式化、副作用受控”。
- 建议：`InstallSelected`/`verifyInstalled` 将历史写入错误显式记录为 WARN 并继续；`Log` 在文件写失败时至少向 stderr 报一次；setupapi 日志读取失败记 WARN 并统计缺失日志。

#### H3. Go 版本与 PS1 基线存在可验证的行为偏差，且缺少对照测试
- 位置：`internal/app/install.go:63-76` vs `install_lenovo_drivers.ps1:1732-1748`；`internal/app/view.go:58-87` vs `install_lenovo_drivers.ps1:533-552`
- 问题：PS1 对 EXE 失败会提示交互式 `r`/`s` 回退，Go 没有该交互回退；PS1 主流程对 `LatestAcrossOS` 有独立大分支，Go 没有；PS1 的 URL 刷新、MD5 mismatch 重试，Go 没有。
- 证据：`install_lenovo_drivers.ps1:1144-1153` 提供 `Type r to run ... interactively, or s to skip`；Go `InstallSelected` 仅记录失败并继续。
- 影响：迁移承诺“保持现有 CLI 契约和安装器处理”（`prd.md:10-15`），当前存在用户可见行为差异。
- 建议：先把每个差异写成 fixture/CLI 冒烟断言（交互回退、403 刷新、跨 OS 合并），再决定“等价实现”或“显式变更契约”；不要在无测试时声称行为等价。

#### H4. `.gitignore` 未覆盖迁移产物，仓库已出现未跟踪构建产物
- 位置：`.gitignore:1-9`
- 问题：PRD 验收要求 `.gitignore` 覆盖 `bin/*.exe`、`sessionlogs/`、`nul`、Go 构建产物（`prd.md:38`），当前未覆盖。
- 证据：`git status --porcelain` 显示 `bin/lenovo-driver.exe`、`sessionlogs/2026-08-26.md`、`nul` 均为 `??`。
- 影响：后续提交会带入构建产物/临时文件；`nul` 是 Windows 保留文件名，还可能破坏 grep 等工具（本审计中 rg 因 `nul` 返回 `函数不正确`）。
- 建议：补齐 ignore 规则；清理或归档 `nul`、`sessionlogs` 的归属后加入 ignore；把“无未跟踪构建产物”加入 verify 脚本。

#### H5. 安装器进程抽象存在隐患：启动失败无错误、超时竞态、输出丢失
- 位置：`internal/install/install.go:26-52`、`65-90`、`92-95`
- 问题：`RunProcessWithTimeout` 在 `Start` 失败时只返回 `ExitCode=-2`，调用方无法区分“权限/路径问题”和“退出码 -2”；`runWithOutput` 使用 `CommandContext` 但超时后从 `cmd.Process.Pid` 杀树，进程可能已退出或 `Process` 未赋值；`RunProcessWithTimeout` 不捕获 stdout/stderr，而 PS1 基线会记录 pnputil 日志。
- 证据：`install.go:31-33` 返回无错误信息的 `-2`；`install.go:74-75` 在 `ctx.Err()` 后直接 `cmd.Process.Pid`；`ProcessResult` 有 `Stdout/Stderr` 字段但 `RunProcessWithTimeout` 从不填充。
- 影响：安装失败诊断能力弱于 PS1，超时路径可能 panic 或误判；维护者很难从日志判断安装器为什么失败。
- 建议：让 `RunProcessWithTimeout` 返回错误；超时/退出统一使用 `context` + 捕获输出；`killTree` 失败要记录；为超时与启动失败写可注入命令的测试。

#### H6. 幂等/重试逻辑存在 `for{}` 无界循环和丢失的重试语义
- 位置：`internal/app/install.go:104-162`
- 问题：`downloadVerified` 使用 `for {}` 且只有 `return` 可退出；函数名为 verified，但若下载成功且哈希通过后没有 `return` 分支之外的重试条件，循环可读性差；代码里也没有“refresh URL 后再试一次”的实际步骤。
- 证据：`install.go:104` `for {`、`install.go:136-160` 成功路径在末尾 `return downloadedSize, nil`；未出现 URL 刷新调用。
- 影响：后续维护者在循环中加错误分支时容易制造无限重试；当前行为无法满足 403 刷新要求。
- 建议：把“缓存验证 → 下载 → 完整性校验 → 刷新重试”改成有限步骤状态机（如 `attempt := 0; for attempt < max && refreshable { ... }`），并为“缓存通过/哈希失败/403 刷新”分别写测试。

### Medium

#### M1. `pathBase`/`pathParent` 三处重复实现
- 位置：`internal/api/parse.go:182-188`、`internal/app/evidence.go:137-153`、`internal/audit/audit.go:301-316`
- 问题：三个包各自实现路径 basename/parent，行为细节略有差异（`api` 的版本用正则切分，`evidence/audit` 用 `LastIndexAny`；`audit.pathParent` 未先 TrimRight）。
- 证据：grep 命中三组函数；`implement.md:47` 明确要求“合并 `pathBase`/`pathParent` 重复实现”。
- 影响：后续修改一个实现不会同步其他实现，Windows 路径边界行为可能漂移。
- 建议：新建 `internal/winpath` 或 `internal/pathutil` 小包，提供 `Base/Parent`，并补充带尾分隔符、无分隔符、UNC 路径测试。

#### M2. 正则每调用/每行重新编译，且规则散落
- 位置：`internal/compare/compare.go:123-648`（60+ 处 `regexp.MustCompile`）、`internal/api/client.go:153/235`、`internal/app/view.go:128`
- 问题：大量静态正则被放在函数内每次执行编译；同类模式（`Intel|Realtek|MediaTek...`、`Direct|Virtual`、厂商名）在多函数重复书写。
- 证据：`compare.go:213-215` 在同一个分支内编译 3 次；`GetDeviceVendor` 与 `GetRemoteComponentVendor` 是重复厂商规则。
- 影响：每个 driver 都要重编译数十个正则，性能与可读性差；厂商/匹配规则两处维护极易漂移。
- 建议：把静态模式提升为包级 `var`；把“厂商规则”抽成一个 `vendorPatterns` 数据表；为名称模式增加数据驱动测试。

#### M3. 未使用的导出/非导出符号增加理解成本
- 位置：`internal/app/install.go:223-225`、`internal/compare/compare.go:694-695`、`internal/inventory/windows.go:214-215`
- 问题：`normalizeSourceString` 无调用点；`compare.EarliestTime` 无调用点（API 解析仍硬编码 1900 日期，`internal/api/parse.go:131`）；`inventory.Timeout` 无调用点。
- 证据：grep 仅命中定义；`EarliestTime` 未用于统一默认时间。
- 影响：读者无法判断哪些是已规划 API、哪些是遗留；重复常量会继续扩散。
- 建议：删除无调用符号，或在接口文档中说明用途并让 API/audit 共用；用 `EarliestTime` 替换 `parse.go:131` 的硬编码。

#### M4. 命名偏泛，且 PowerShell 风格命名残留
- 位置：`internal/app/app.go:38/53`（`App`/`New`）、`internal/api/client.go:27/32`（`Client`/`NewClient`）、`internal/plan/plan.go:13` 等；`internal/app/helpers.go:47-49` `resolveTarget` 只是 `compare.ResolveTargetOsEntry` 的转发壳。
- 问题：`App`、`Client`、`New` 是通用名，在该域中不表达“Lenovo driver installer CLI/API client”；`resolveTarget` 包装层没有新增语义。
- 证据：`helpers.go:47-49` 只调用 `compare.ResolveTargetOsEntry`；同一包内还有 `selectByCodes`、`resolveTarget` 等多个 5-10 行助手。
- 影响：难以 grep 和推理职责，也违反“名字具体且唯一、避免无意义包装层”。
- 建议：至少将 `resolveTarget`/`selectByCodes` 归入明确命名的小文件；新增 Go API 时避免 `App/Handler/Manager`，采用 `CLI`、`LenovoAPIClient`、`DriverViewBuilder` 等具体名称。

#### M5. 测试覆盖明显不足，高风险逻辑无回归保障
- 位置：`internal/app/app_test.go:11-36`、`internal/install/` 无测试、`internal/inventory/windows.go` 无测试；`internal/compare/compare_test.go` 未覆盖 `GetNamePatterns`、`GetDeviceVendor`、`ResolveInstalledSoftwareVersion`、`ResolveLocalDriverVersion`、`GetDisplayWidth`、`ConvertToBytes` 的失败分支等。
- 问题：PRD 要求覆盖“版本解析/比较、硬件匹配、适用性、setupapi 日志解析、来源审计、选择解析”（`prd.md:24`），但 Go 侧只覆盖其中一小部分；`-LatestAcrossOS`、URL 刷新、JSON 协议、`install` 包完全没有测试。
- 证据：`app_test.go` 仅 3 个测试且只检查 1 个非法组合；`install` 包目录无 `_test.go`。
- 影响：迁移等价性无法证明；实现中已出现的 PS1/Go 行为差异没有回归网兜住。
- 建议：按 implement.md Phase 2 以 PS1 86 条断言为清单补 Go 测试；补 `scripts/verify.ps1` 之前至少让 `go test ./...` 覆盖上述高风险函数。

#### M6. README 行数与文档宣称的“保持核心/壳分离”不完全一致
- 位置：`README.md:214-218`、`lenovo_driver_core.ps1:3-5`
- 问题：README 宣称 core“must not call network, registry, PnP, file, console, or process APIs”，但这是文档声明而非可执行校验；且实现中 `install_lenovo_drivers.ps1` 的 `Get-LocalSoftwareSnapshot`、`Get-DriverPackageEvidence` 等大量文件/注册表副作用集中在 shell 中，核心/壳边界仍靠人工维护。
- 证据：`lenovo_driver_core.ps1` 无禁止 API 的测试；`implement.md:13` 说“不改动冻结 PS1 的风格”，但 PS1 仍在被 README 描述为运行时主路径。
- 影响：双轨状态下边界约束无法自动验证，迁移后的 Go 版本也没有对应测试。
- 建议：新增静态检查/测试：扫描 `lenovo_driver_core.ps1` 是否出现 `Invoke-RestMethod/Get-CimInstance/Get-Item/Write-Host`；Go 侧用接口注入保证纯逻辑与 I/O 分离。

### Low

#### L1. `lenovo_driver_core.tests.ps1` 的“86 个测试”是顶层断言数，不是测试用例粒度
- 位置：`lenovo_driver_core.tests.ps1:45-449`
- 问题：86 次 `Assert-*` 调用与 implement.md 的“86 个测试”对齐，但脚本只有 2 个函数（`Assert-True`/`Assert-Equal`），失败时只能看到断言名，没有按场景隔离和详细错误信息。
- 证据：文件总行数 454，顶层断言 86 条；`Assert-Equal` 用字符串比较，无法给出完整 diff。
- 影响：迁移对照时难以定位单条失败；Go 侧若按 86 条“断言”而非场景迁移，覆盖会碎片化。
- 建议：报告/迁移清单中区分“86 断言”和“约 30-40 个场景”；Go 测试按行为场景命名。

#### L2. 代码格式未统一（gofmt 对齐、raw literal 缩进）
- 位置：`internal/model/model.go:29-33`、`internal/api/parse.go:16-20`、`internal/api/parse_test.go:10-38`
- 问题：`model.Driver` 的 `SourceAudit` 行与上方字段对齐不一致；`parse.go` 的 `Data` struct 中 `OSList`/`DriverList` 对齐不齐；`parse_test.go` 的 JSON raw literal 用 2 空格缩进，而 Go 代码本身用 tab。
- 证据：`model.go:33` `SourceAudit   *SourceAudit \`json...\`` 与上方列宽不一致；`parse_test.go` 存在 48 行空格缩进行。
- 影响：`gofmt -l` 会报未格式化；编辑器/CI 判据不明确。
- 建议：在 `scripts/verify.ps1` 中加 `gofmt -l .` 检查；合并 raw JSON 的缩进策略（建议 2 空格但统一说明，或使用 `json.MarshalIndent` 生成 fixture）。

#### L3. `inventory` 中 PowerShell 脚本字符串不可静态校验，错误细节丢失
- 位置：`internal/inventory/windows.go:45/58/94/116/134/154/181`
- 问题：内嵌 PS1 脚本是字符串，无 parse/test；多个 `catch {}` 吞掉注册表/服务查询错误；`runJSON` 对空输出返回 `nil` 而不报“脚本无输出”。
- 证据：`windows.go:116` `catch {}`；`windows.go:134` 两个 `catch {}`；`windows.go:36-39` 空输出 `return nil`。
- 影响：Windows 版本差异下查询静默变空，来源审计被判定“无证据”；内嵌脚本无法被 PSScriptAnalyzer 校验。
- 建议：将每个脚本提取为常量并加注释说明预期输出；`runJSON` 空输出返回显式错误；对关键查询（OS/机器/设备）提供可注入命令的测试或用 fixture 回归。

## 可维护性改进优先级排序

1. **接通 Go 主流程**（Critical）：实现跨 OS 合并、URL 刷新重试、WPF 切 Go、README/spec 更新。这是迁移任务的核心，不做则其他整理无意义。
2. **建立单命令验证**：创建 `scripts/verify.ps1`，包含 `gofmt -l`、`go vet`、`go test`、PS parse/core tests、`git diff --check`、`git status` 无构建产物检查；本机补 Go 工具链。
3. **补齐行为等价测试**：以 PS1 86 断言和现有 JSON fixture 为清单，覆盖 `compare/audit/plan/app/view`；先测再拆。
4. **拆分超大文件与函数**：`compare.go` 拆为 `version/hardware/software/format/select`；`App.Run` 与安装调度拆步骤；保持 500 行/20 行约束。
5. **统一重复实现**：`pathBase/pathParent`、`powershellExe`、厂商/名称模式、默认时间常量收口到一个包或常量表。
6. **显式化错误处理**：历史、日志、下载清理、inventory、安装器进程都返回/记录错误；`GetRefreshedDriverURL` 返回 `(string, error)`。
7. **清理仓库噪音**：`.gitignore` 补 `bin/*.exe`、`sessionlogs/`、`nul`、Go 构建产物；确认 `nul` 文件归属；移除死代码。
8. **冻结 PS1 的可运行性但停止功能演进**：保留为回滚基线，README 和 spec 明确冻结状态；把 PS1 中的行为差异逐项列为“已知边界”或“待迁移”。

## 与现有 implement.md 的差异/补充

- `implement.md:33` 说“`view.go` 实际拉取并合并其他 OS 列表”，本审计发现该步骤尚未实现，应把其列为未完成项而非待办叙述。
- `implement.md:34` 说“接 403 URL 刷新”，本审计确认 `GetRefreshedDriverURL` 已存在但无调用点；补充应明确“调用点 + 错误传播 + fixture”。
- `implement.md:40-42` Phase 3 要求 WPF 切 Go，当前 WPF 仍调用 PS1；补充建议先做 JSON 协议契约测试，再做 worker 切换。
- `implement.md:46` 说 `compare.go` 695 行待拆，审计确认现状 695 行，并补充同类超标文件（`install_lenovo_drivers.ps1` 1791、`lenovo_driver_core.ps1` 909、`lenovo_driver_wpf.ps1` 565）和 `App.Run` 等函数。
- `implement.md:26-27` 要求 `.gitignore` 和 `scripts/verify.ps1`，审计确认 `scripts/` 不存在、`.gitignore` 未覆盖；补充 `nul` 已实际影响 grep 工具。
- `implement.md:32` 的“PS core 86 个测试”应注明是 86 条顶层断言，迁移清单建议按场景整理。
- 补充审计发现的 PS1 基线既有缺陷（`Get-MatchingRemoteComponent -Driver/-VersionText` 参数不匹配）和 PS1 与 Go 的行为差异（EXE 交互回退、MD5 mismatch 重试），这些未在 implement.md 中显式列出。
