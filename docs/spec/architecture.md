# 架构与数据流

本文是“系统怎么分层、谁负责什么、数据按什么顺序流动”的权威描述。任何行为变更都必须能对照本文件走一遍，确认没有在既有边界之外新增副作用。

## 1. 目标

Lenovo 驱动安装器是面向联想中国大陆机型的 Windows 桌面工具：解析机型与主机编号 → 查询联想官方驱动接口 → 与本地驱动状态对比 → 让用户选择下载或安装适用驱动。

运行时引擎是 Go CLI（`cmd/lenovo-driver` → `internal/app`）。PowerShell WPF 是唯一桌面前端，通过 JSON 导出文件与 `DriverCode` 选择与 CLI 通信。仓库里没有第二个运行时引擎。

## 2. 分层

```text
User
  |
  | install_lenovo_drivers.bat / install_lenovo_drivers_wpf.bat
  v
WPF 层 (lenovo_driver_wpf.ps1 -> wpf/ui.ps1, worker.ps1, actions.ps1)
  |
  | Start-Process bin\lenovo-driver.exe
  | -GuiExportPath <json> / -GuiInstallCodes <codes>
  v
Go CLI (cmd/lenovo-driver -> internal/app)
  |
  | 系统调用
  v
联想 API + Windows 盘点 + 安装器 + 文件
```

Go CLI 拥有全部驱动业务逻辑：API 访问与归一化、本机/OS/PnP/应用/软件快照、适用性与版本比较、来源审计、下载重试与 URL 刷新、MD5/SHA-256 完整性、MSI/INF/ZIP/CAB/EXE 安装分发、计划/日志/账本/JSON 导出产物。

WPF 只是表现壳：渲染 JSON 导出、启动后台 Go 进程、回传 `DriverCode` 选择。它不实现驱动匹配或安装逻辑。

## 3. 仓库布局

```text
cmd/lenovo-driver            CLI 入口
internal/api                 Lenovo API 客户端与响应归一化
internal/app                 编排、CLI、GUI 导出、账本、提示
internal/audit               setupapi 解析与来源证据审计
internal/compare             版本解析、匹配、选择、格式化
internal/download            HTTP 下载、重试、SHA-256 伴生文件
internal/install             安装器分发与 EXE 回退；原生 DiInstallDriverW INF 路径（pnputil 回退）
internal/inventory           Windows 机器/OS/PnP/应用快照（原生 SetupAPI/CfgMgr32/注册表；运行时无 PowerShell）
internal/model               共享纯数据类型
internal/pathutil            Windows 路径助手
internal/plan                计划文本、表格、账本行构建
internal/trust               WinVerifyTrust Authenticode 签名校验
wpf/window.xaml              WPF 窗口布局
wpf/ui.ps1                   WPF 窗口构造与 UI 助手
wpf/worker.ps1               WPF 后台 worker 状态机
wpf/actions.ps1              WPF 安装动作确认与分发
scripts/verify.ps1           离线验收门禁
scripts/dev.ps1              快速内循环
scripts/lib/go-toolchain.ps1 go 工具链发现（单一源）
lenovo_driver_wpf.ps1        WPF 表现入口
install_lenovo_drivers.bat   薄 CLI 启动器
install_lenovo_drivers_wpf.bat 薄 WPF 启动器
docs/                        本文档树
```

确定性包（`api`、`compare`、`audit`、`plan`、`model`）不碰网络、注册表、PnP、控制台、进程或文件 API。副作用集中在 `app`、`download`、`install`、`inventory`。

## 4. 前置条件

- Windows 10 / Windows 11
- Go 1.27 或更新（官方工具链）
- Windows PowerShell 5.1（仅 WPF 层）
- 驱动安装需要管理员权限
- 需要访问联想官方 API 主机的网络

## 5. 端到端数据流

```text
cmd/lenovo-driver (main)  ->  internal/app.App.Run
                                   |
                                   v
        +---------------- resolveRuntime ----------------+
        |  machine -> OS -> category -> OS list -> local |
        |  device/app/software snapshot -> driver history|
        +-----------------------------------------------+
                                   |
                                   v
        +------------- CompareOSDriverView --------------+
        |  load list -> (可选跨 OS merge) -> row 所有权  |
        |  -> filter -> latest-select -> assess          |
        |  (apply / local-version / status / source audit)|
        |  -> plan file + console table -> partition      |
        +-----------------------------------------------+
                                   |
                                   v
                  runSelection  -> export | dry-run |
                                   select | interactive
                                    |
                                    v
          InstallSelected -> download-verify ->
          install-dispatch (msi/inf/zip/cab/exe + fallbacks)
                              -> post-install verify -> CSV 账本
```

### 5.1 入口与参数契约（`internal/app/app.go`）

`cmd/lenovo-driver/main.go` 只调用一次 `app.New(...).Run(args)`。

`Run` 依次：`ParseOptions` 解析 PowerShell 风格参数 → `-Help` 打印用法退出 `0` → `Validate` 拒绝互斥参数组合退出 `2` → 启动单个 30 分钟 `context.WithTimeout` 约束所有网络/下载/安装步骤 → `maybeElevated` 在需要管理员时重发提权 → `resolveRuntime` 构建只读 `ViewContext` → 解析待比较的 `listOsID` → `CompareOSDriverView` 构建评估视图 → `runSelection` 分发导出/试运行/选择/安装。

`ViewContext` 刻意保持只读；每次运行的易变暂存（OS 驱动缓存）放在 `App` 上，消息代码无法改写共享输入。

### 5.2 解析阶段

`resolveRuntime` 按序收集：`inventory.GetMachineInfo`（机型+序列号）→ `inventory.GetOSInfo`（Windows 版本+归一化 OS 名）→ `api.ResolveCategoryID`（联想机型分类，自动失败回退 `-Model`）→ `api.ResolveOSEntry`（当前 OS 条目+完整支持 OS 列表）→ `inventory.GetLocalDeviceSnapshot` / `GetInstalledApps` / `GetSoftwareSnapshot`（本地盘点，失败软降级）→ `ReadHistory`（历史账本，用于来源审计）。

机器 / OS / 分类 / OS 条目解析硬失败即停；快照失败降级为空盘点（仍可安全继续）。

### 5.3 比较阶段（`view.go`、`compare`、`audit`）

1. `mustDriverList` 加载官方驱动列表（QuickFix 优先、网页回退），硬错误即停，结果视为自有数据。
2. `-LatestAcrossOS` 下并行拉取每个备用 OS 列表，按确定性的 OSID 顺序合并；空/失败的备用列表是软缺失（`tolerated`）。
3. 深拷贝拉取的行，评估写操作不碰共享列表缓存或 API 传输 DTO。
4. `filterDriverRows` 丢弃禁用 / BIOS / 不可安装行。
5. `SelectLatest` 按 code 选出最新适用行。
6. `assessSelectedDrivers` 先做单次设备-版本索引，再逐驱动：测试适用性 → 解析本地版本与厂商（`compare.ResolveLocalDriverVersion`）→ 计算 `CompareStatus` → 有本地版本时跑来源证据审计（`audit.ResolveDriverSourceEvidence`，非历史来源标 `External`，`Local newer` 由审计标记而非当错误）→ 折叠匹配设备问题码为 `DeviceProblem`。
7. `present` 写计划文件 + 打印控制台表，`partitionViewDrivers` 拆成适用 / 仅更新两集。

> 数据流注：设备-版本索引对所有适用驱动只取一次；备用来源映射懒构建并复用，比较阶段不会逐驱动重扫。

### 5.4 选择阶段（`runSelection`、`interactive.go`）

- `-GuiExportPath` → `ExportGUIView` 写 WPF JSON 后退出 `0`。
- `-DryRun` → 记日志“未下载未安装”退出 `0`。
- `-GuiInstallCodes` → `selectByCodes` 映射逗号分隔 code；缺失或不适用的 code 快速失败退出 `3`。
- 否则 → `SelectInteractive` 提示；按 `t` 重载另一个 OS 列表并重建视图。

交互流程在任何下载前打印精确的 `y` / `a` 驱动集，让选择先于副作用可见。

### 5.5 下载与安装（`install.go`、`install`、`download`）

`InstallSelected`：创建下载目录 → 逐选中驱动 `downloadVerified`（缓存命中校验后复用，否则重下；`403` 从当前/合并列表刷新一次 URL 后重试最多一次）→ `-DownloadOnly` 写 `Downloaded` 账本行即算成功 → 否则 `install.InstallDriverFile` 按文件类型分发（`.exe`/`.msi`/`.inf`/`.zip`/`.cab`，各有原生路径 + 有界超时 + 合理回退，`.exe` 静默失败后可能交互重跑）→ 每个结果写账本行（`Installed`/`Failed`/`Downloaded`/`Verified`）→ 有安装成功且非 `-DownloadOnly` 时跑装后复核（`verifyInstalled`，重读版本写 `Verified` 前后版本行）→ 任一驱动失败返回非 nil（驱动退出码 `1`）。

## 6. 开发边界

- 确定性逻辑放 `internal/compare`、`internal/audit`、`internal/plan`、`internal/api`；副作用放 `internal/app`、`internal/install`、`internal/download`、`internal/inventory`。
- 保留公开 CLI 参数、交互选择、退出码、缓存规则、安装器回退。
- 不新增第二个运行时语言入口点。
- 任何行为变更必须更新测试并通过 `scripts/verify.ps1`。
- 不复活已删除的旧 PowerShell 引擎文件。
