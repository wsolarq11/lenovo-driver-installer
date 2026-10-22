# 目录与包职责

> 仓库怎么组织，每个包负责什么。

## 目录布局

```text
cmd/lenovo-driver                 CLI 入口：flag 解析后交给 app
cmd/gatecheck                     按名字跑一条门禁源码规则（gate -> verify.ps1）
internal/api                      Lenovo API 客户端与 JSON 归一化（联网）
internal/app                      编排：CLI、elevation、选择分发、安装流程、GUI 导出
internal/audit                    setupapi 解析与来源证据（纯函数）
internal/compare                  版本解析、匹配、选择、表格（纯函数）
internal/download                 HTTP 下载、重试、完整性（联网 + 文件）
internal/gate                     门禁规则本体：go/parser + go/types，只用标准库
internal/install                  安装器分发与原生 INF 安装（副作用）
internal/inventory                Windows 机器/OS/PnP/应用快照（副作用）
internal/ledger                   WORM 账本：列定义、哈希链、读写
internal/model                    共享纯数据类型
internal/pathutil                 Windows 路径助手
internal/plan                     计划文本、表格、账本行构建
internal/trust                    Authenticode 签名校验
wpf/                              WPF 表现层（ui/worker/actions/xaml）
scripts/                          门禁与开发脚本
docs/spec/                        产品与架构规范（人读单一源）
docs/spec/coding/                 编码规范（本目录）
docs/howto/                       任务导向手册
docs/decisions/                   冻结的决策记录
docs/records/                     追加式历史与证据
```

## 包职责

**分类的唯一权威是 `docs/spec/architecture.md` §2.1。** 本文件不维护第二份分类表；那里说哪个包属于确定性/联网/副作用，门禁按那里的声明与实际 import 图做集合比对。

`internal/app` 是命令式外壳：flag 解析、elevation、API 调用、系统快照、选择分发、安装编排、GUI 导出。它不定义账本列、不做版本比较、不解析 setupapi。

`internal/ledger` 拥有 WORM 账本的全部结构：列顺序、哈希链、读写成对出现。它的方法只接收路径与记录，不持有 orchestrator 状态，因此可以在任何入口被复用且行为一致。

`lenovo_driver_wpf.ps1` 是桌面表现层：不得含驱动匹配、来源归因或安装逻辑。它启动 Go 子进程、读 JSON 视图、渲染表格、用 `-GuiInstallCodes` 回传勾选码。

`internal/gate` 是门禁规则集，用源码分析而非文本匹配。它只用 Go 标准库（`go/parser`、`go/ast`、`go/types`），从不启动进程；唯一需要的进程边界——依赖包的导出数据——在 `cmd/gatecheck`，由 `scripts/verify.ps1` 每条具名步骤调用一次。判断依赖“名字指向什么”或“声明形态”的规则属于这里；只读字面文本的规则可以留在门禁脚本。

## 模块组织

- 生产 Go 文件的行数上限只有一个值，由 `scripts/verify.ps1` 的 `Go source file size` 步骤声明并执行。靠近上限时，把一个完整关注点抽成独立文件或独立包，而不是让一个文件承载第二个关注点。
- 一个包只承载一个关注点。出现“顺手也放这里”的第二个关注点时，先问它是否值得独立成包。
- `install_lenovo_drivers.bat` 保持薄：需要时构建引擎、调用它、转发退出码。
- 不新增第二个 PowerShell 运行时入口。WPF 文件是唯一的 PowerShell 运行时外壳。

## 命名

- 用 Go 惯例：导出名表达公开契约，未导出助手保持包内私有。
- 用显式错误返回，不用静默回退值。
- 包名具体：`pathutil`、`inventory`、`plan`、`ledger`；避免 `util`、`manager`、`data`。

## 例子

- CLI 契约：`cmd/lenovo-driver/main.go` 只做转发，装配在 `internal/app`。
- 薄启动器：`install_lenovo_drivers.bat` 仅在缺失时构建 `bin\lenovo-driver.exe`，随后转发全部参数。
