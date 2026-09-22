# 架构与数据流

本文是“系统怎么分层、谁负责什么、数据按什么顺序流动”的权威描述。任何行为变更都必须能对照本文件走一遍，确认没有在既有边界之外新增副作用。

本文件写**边界与数据流向**，不写函数调用顺序与内部优化——那些的权威是 `internal/` 代码。代码实现变了而本文件不需要变，是本文件的维护性目标。

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

### 2.1 确定性边界（架构级约束）

- **确定性包**（`api`、`compare`、`audit`、`plan`、`model`）：不碰网络、注册表、PnP、控制台、进程或文件 API。同输入同输出。
- **副作用包**（`app`、`download`、`install`、`inventory`）：系统副作用集中于此，是“确定性”与“外部世界”之间的唯一接口。
- 运行时只派生到原生 Windows API 与联想官方接口，从不派生 PowerShell。

## 3. 仓库布局

```text
cmd/lenovo-driver            CLI 入口
internal/api                 Lenovo API 客户端与响应归一化
internal/app                 编排、CLI、GUI 导出、账本、提示
internal/audit               setupapi 解析与来源证据审计
internal/compare             版本解析、匹配、选择、格式化
internal/download            HTTP 下载、重试、SHA-256 伴生文件
internal/install             安装器分发与 EXE 回退；原生 INF 安装（pnputil 回退）
internal/inventory           Windows 机器/OS/PnP/应用快照（原生 SetupAPI/CfgMgr32/注册表）
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

## 4. 前置条件

- Windows 10 / Windows 11
- Go 1.27 或更新（官方工具链）
- Windows PowerShell 5.1（仅 WPF 层）
- 驱动安装需要管理员权限
- 需要访问联想官方 API 主机的网络

## 5. 数据流（阶段模型）

一次运行按五个阶段流动，阶段间只通过只读视图传递，不共享可变状态：

```text
解析 → 比较 → 选择 → 下载安装 → 记账
```

| 阶段 | 职责 | 副作用 | 不变量落点 | 代码锚点 |
| --- | --- | --- | --- | --- |
| 解析 | 收集机型/OS/分类/OS 列表/本地盘点/历史账本，构建只读视图 | 读系统、读 API、读账本 | 硬失败即停；快照失败降级为空 | `internal/app/app.go`、`internal/inventory`、`internal/api` |
| 比较 | 加载官方列表，选出最新适用行，评估版本/状态/来源 | 无（纯函数 + 只读输入） | 事实分级、默认不动 | `internal/app/view.go`、`internal/compare`、`internal/audit` |
| 选择 | 按导出/试运行/交互/回传码分发下一步 | 写 GUI JSON（可选） | 选择先于副作用可见 | `internal/app/interactive.go`、`internal/app/export.go` |
| 下载安装 | 下载校验、按文件类型分发安装、装后复核 | 网络下载、写文件、装驱动 | 审计先于状态变更、设备级回退 | `internal/download`、`internal/install` |
| 记账 | 写计划、日志、WORM 账本、回退 offer | 追加写文件 | 审计先于状态变更（写失败不提交） | `internal/plan`、`internal/app/history.go`、`internal/app/rollback.go` |

阶段语义要点：

- **解析**：机器/OS/分类/OS 条目硬失败即停（没有正确输入就没有正确输出）；本地快照失败降级为空盘点，仍可安全继续。
- **比较**：比较阶段深拷贝官方列表的行，评估的写操作不碰共享列表缓存或 API 传输 DTO；备用 OS 列表为空/失败是软缺失（`tolerated`），不阻断主流程。`Local newer` 由来源审计标记，不当错误。
- **选择**：任何下载发生前，`y` / `a` 驱动集已精确打印，选择先于副作用可见。
- **下载安装**：每个文件先校验后使用；安装按文件类型走原生路径，EXE 静默失败后可交互重跑。
- **记账**：安装/回退动作前先写意图行（`Install` / `Rollback`），写入失败则跳过动作；账本每行带 SHA-256 链哈希，可证伪篡改/乱序/删除。

## 6. 开发边界

- 确定性逻辑放 `internal/compare`、`internal/audit`、`internal/plan`、`internal/api`；副作用放 `internal/app`、`internal/install`、`internal/download`、`internal/inventory`。
- 保留公开 CLI 参数、交互选择、退出码、缓存规则、安装器回退。
- 不新增第二个运行时语言入口点。
- 任何行为变更必须更新测试并通过 `scripts/verify.ps1`。
- 不复活已删除的旧 PowerShell 引擎文件。
