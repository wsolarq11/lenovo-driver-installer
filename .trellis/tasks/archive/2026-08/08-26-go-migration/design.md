# 迁移 Lenovo Driver Installer 到 Go：设计

## 目标架构

```text
WPF（表层，保留）
  ↓ JSON / 进程调用
Go CLI 引擎（核心层）
  ↓ 系统命令
pnputil.exe / msiexec.exe / expand.exe / PowerShell(查询兜底)
```

Go 拥有全部业务逻辑。PS1/bat 不再是长期维护层，迁移完成前保留为对照与回滚基线。

本设计已结合三方独立审计与交叉会审结论，审计原文见 `research/`。

## 边界

- Go 核心包不得依赖 PowerShell 业务逻辑；只在必要时通过 PowerShell 查询 CIM/PnP 数据。
- WPF 仍可调用 PowerShell，但 PowerShell 只做 UI 和子进程启动，不重复驱动逻辑。
- Go CLI 与原 PS1 CLI 参数、退出码、交互选择保持一致。
- WPF JSON 协议保持兼容，先让 Go 能生成 WPF 已能解析的 JSON；本次将 WPF 默认入口从 PS1 切到 Go，PS1 冻结为回滚基线。
- 冻结不等于删除：PS1/bat 保留在仓库中，验收前只用于对照和回滚，不继续加功能。

## 目录结构

```text
cmd/lenovo-driver/main.go        # CLI 入口
internal/api/                    # Lenovo 官方 API 客户端
internal/model/                  # 驱动、设备、OS、历史等数据模型
internal/inventory/              # 机器、OS、PnP、应用、软件快照
internal/compare/                # 版本比较、硬件匹配、适用性
internal/audit/                  # setupapi 解析、来源证据、来源标签
internal/plan/                   # 计划文本、表格、状态汇总
internal/download/               # 下载、MD5/SHA-256、缓存、URL 刷新
internal/install/                # pnputil/MSI/EXE/zip/cab 调度
internal/app/                    # 编排、交互、历史记录、JSON 导出
go.mod
README-go.md（或并入 README）
```

## 数据模型

`Driver`：

- PartID、PartName、DriverName、DriverCode、DriverEditionId
- Version、FileName、FilePath、FileSize、FileType
- InstallCode、InstallParameter、Bootfile、HardwareId、OfficialMd5
- Status、IsEnable、IssuedDate、OSID、OsName、SourceApi
- LocalVersion、LocalVendor、CompareStatus、CompareSource、SourceAudit

`Device`：Name、Class、DeviceId、PnpDeviceId、DriverVersion、DriverDate、InfName、ProviderName、InstallDate、PackageDir、ImportSource 等证据字段。

`HistoryRecord`：Timestamp、DriverCode、OSID、OSName、DriverName、Version、VerifiedVersion、BeforeVersion、FileName、MD5、Source、Result、Message。

## 核心行为映射

| PS1 区域 | Go 包 | 关键点 |
|---|---|---|
| Lenovo API | `internal/api` | QuickFix 优先，Web 回退，URL 刷新，OS 解析 |
| 本地探测 | `internal/inventory` | CIM/WMI、PnP、注册表、服务文件版本 |
| 版本比较/匹配 | `internal/compare` | 与 `lenovo_driver_core.ps1` 保持等价 |
| setupapi 解析/来源审计 | `internal/audit` | 保留 `cmd:`、DriverStore、offline/dev 证据 |
| 计划/表格 | `internal/plan` | 保持文本格式和 JSON 字段 |
| 下载/校验 | `internal/download` | MD5/SHA-256、companion、403 刷新 |
| 安装器 | `internal/install` | EXE/MSI/INF/ZIP/CAB、超时、fallback |
| 主流程 | `internal/app` | 参数、选择、历史、退出码 |

## CLI 参数

与现有 PS1 参数保持一致，参数名沿用 `-DryRun` 等（Go flag 可自定义别名），具体值类型与互斥规则一致。

## JSON 协议

- 字段名与 PS1 `Export-LenovoDriverViewJson` 保持兼容，WPF 可按字符串直接渲染。
- `SourceAudit` 以 Go 输出 `Category: Summary` 为规范；不再复刻 PS1 的对象字符串化格式。
- 用 fixture 固化 JSON 协议，避免 WPF 切换后逐字段漂移。

核心结构：

```json
{
  "GeneratedAt": "...",
  "MachineModel": "...",
  "SerialNumber": "...",
  "SystemCaption": "...",
  "CurrentOsId": "...",
  "CurrentOsName": "...",
  "ListOsId": "...",
  "ListOsName": "...",
  "DataSource": "...",
  "OsList": [{"OSID":"...","OSName":"..."}],
  "Drivers": [
    {
      "Selected": false,
      "DriverCode": "...",
      "DriverName": "...",
      "Version": "...",
      "LocalVersion": "...",
      "CompareStatus": "...",
      "SourceAudit": "...",
      "CompareSource": "...",
      "FileName": "...",
      "FilePath": "...",
      "FileSize": "...",
      "MD5": "...",
      "IsApplicable": true,
      "IsUpdate": false
    }
  ]
}
```

WPF 现有解析逻辑可直接读取该结构。

## 关键实现决策

- `LatestAcrossOS` 在 `CompareOSDriverView` 中加载并合并全部 OS 列表，过滤后统一交给 `SelectLatestDrivers`；不能只影响交互开关。
- 403/URL 刷新由 `downloadVerified` 调用 `GetRefreshedDriverURL`，更新 `driver.FilePath` 后有限重试一次；刷新函数返回 `(string, error)`。
- EXE fallback 严格按 PS1 日志优先算法：先读 `DriverCode.log` 定位本次提取目录；无日志证据时不得扫描任意 `is-*.tmp` 安装 INF；仅 NVIDIA 且无日志目录时按 PS1 限制扫描 `Display.Driver`。
- `RelaunchElevated` 必须把 PowerShell 提权子进程的真实退出码传回外层 `Run`，不得固定返回成功。
- `ReadHistory` 必须剥离 UTF-8 BOM，兼容 PS1 生成的旧 CSV。
- `scripts/verify.ps1` 是唯一离线验收入口，只运行 Help、非法组合、离线测试和静态检查，不进入真实 API/安装路径。

## 迁移阶段

1. Go 工程骨架 + 模型 + 纯逻辑单元测试。
2. API 客户端与数据解析。
3. 本地探测与版本比较。
4. setupapi 审计与来源标签。
5. CLI 主流程、计划/表格、历史记录。
6. 下载/安装器副作用。
7. JSON 导出与 WPF smoke。
8. 文档、验收、回滚基线确认。

## 风险与对策

- Go 未安装：Go 工具链是 Phase 0 硬性前置条件；通过官方安装方式安装，无法安装则停在 Phase 0 并报告。
- WPF 切换需要真实桌面/真实 API：用 JSON fixture 与 `-WorkerSmoke` 验证，WPF 切换是本任务收口范围，不能作为永久已知边界。
- EXE fallback 有安装错误 INF 风险：按日志优先算法实现，并对 fallback 加回归测试。
- 行为差异：以现有 PS1 和 core 测试为行为基准，逐项迁移并跑 Go 单测；保留 PS1 对照。
- Windows 系统查询差异：优先 CIM/PnP 属性，Go 可用 `github.com/yusufpapurcu/wmi` 或调用 PowerShell 子进程；不引入未经验证的重依赖。
- 验证环境可能误触真实 API：`scripts/verify.ps1` 明确只跑安全命令；审计阶段已记录一次 PS1 未知参数误触 API 的教训。
