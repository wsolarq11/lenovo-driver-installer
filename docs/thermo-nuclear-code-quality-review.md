# 热核代码质量评审 — 全轮次归一汇总（单一权威报告）

- 评审对象：`lenovo-driver-installer` Go 引擎（`cmd/` + `internal/` 全部源码）+ WPF 层（`wpf/`）
- 评审方式：热核可维护性评审（极端严格的抽象 / 大文件 / 意大利面条分支审查）
- 里程碑：2026-08-30，共三轮独立全量复审
  - **首轮** `#1–#7`（最终报告，见原 `final` 文档）
  - **第二轮** `V1–V8`（独立新鲜全量复审，见原 `v2` 文档）
  - **第三轮** `R1–R3`（独立复审 + 落地，见原 `round3` 文档）
- 报告性质：**归一终稿**。以一份统一模板把三轮逐一整理（举一反三），合并交叉发现、
  收敛为一册；取代 `docs/` 下此前各轮次的独立文档（`final` / `v2` / `round3` 已废弃替换）。

> 归一说明：三轮评审**方法论完全相同**（独立全量重读 → grep 佐证 → 实机
> `go build/vet/test/gofmt` 复验 → 发现按严重度分级 → 落地 + 回归单测 + 复验）。因此本文件
> 用**同一套结构模板**逐份整理每一轮（§4 逐轮叙事全部同构），并把跨轮「同主题」发现归并到
> §5（举一反三：一份发现推广到多份），使读者既能看到每轮独立结论，也能在一张台账里纵览
> 整个演进。

---

## 1. 结论速览

代码库**整体健康**：包边界清晰（`api` / `app` / `audit` / `compare` / `download` / `install` /
`inventory` / `plan` / `model` / `pathutil`），领域规则走**声明式表**（`vendorRules`、
`driverMatchRules`、`softwareRules`、`sourceEvidenceRules`、`fieldExtractRules`）而非条件链——
这是对的。**没有任何文件接近 1000 行**（最大 `app/view.go` 466 行、`audit/audit.go` 405 行、
`compare/matching.go` 366 行），因此技能预设的两条阻塞性气味（文件超 1k、朴素意大利面）
**不成立**。

三轮累计发现与状态一览：

| 轮次 | 发现 | 高优先级 | 状态 |
|------|------|---------|------|
| 首轮 | `#1–#7` | `#1/#2/#3` | 全部 [已落地]，`#2` 部分后续方向保留 |
| 第二轮 | `V1–V8` | `V1/V2/V3` | `V1–V5/V7/V8` 已落地；`V4` 受限落地；`V6` 延后 |
| 第三轮 | `R1–R3` | `R1/R2` | `R1–R3` 全部落地；`V6` 复确认延后 |

- **放行**：第三轮最终**无阻塞项**；前两轮阻塞（`#1` 契约、`V1` 启动失败压码）均已闭环。
- **遗留**：`V4`（`Driver` 三职模型彻底拆分，受限落地）、`V6`（每驱动一次 PowerShell 子进程，
  延后）、以及「`DriverView` 携带 `driverEntry{Driver, Assess}`」的可选建模方向——均不阻塞
  正确性，见 §7。

---

## 2. 评审范围与方法（归一模板）

> 本模板是「举一反三」的基准：三轮每一轮都**严格套用同一份**方法 / 证据 / 放行标准，
> 只报告当轮相对前轮的**新增**或**复燃**问题。下表因此对三轮通用。

### 2.1 范围与证据

- **范围**：整个 `cmd/` + `internal/` Go 树（每份源码均逐文件通读），以及 `wpf/` 层。
- **方法**：逐文件通读 + 结构化定位；所有发现均以 `grep`/包含行号佐证，非"查过再说"。
- **验证**：每轮实机执行 `go build ./...`、`go vet ./...`、`go test ./...`、`gofmt -l`，
  第三轮额外跑 `scripts/verify.ps1`（14 步全 `VERIFY_OK`）。

### 2.2 规模事实（实测）

| 指标 | 数值 |
|------|------|
| 最大文件 | `app/view.go` 466 行 |
| 其余大文件 | `audit/audit.go` 405、`compare/matching.go` 366、`app/app.go` 352、`app/install.go` 309 |
| go build / vet / test / gofmt | OK / OK / 全包 ok / 干净 |

> 刻意不臆造「1k 行」或「朴素 if 链」两条阻塞项——文献级证据表明它们不存在。

### 2.3 放行条（每轮同一标准）

按热核技能：无清晰结构回归、无可见「可以更简单」被丢弃、无无端文件翻倍、无意大利面新增、
无 hacky / 伪装抽象、无重复规范 helper、无错层泄漏。对应三轮执行，逐轮给出阻塞项结论
（见 §4 每轮小结）。

---

## 3. 全量发现台账（跨轮统一编号）

> 三条：`#x`（首轮）、`Vx`（第二轮）、`Rx`（第三轮）。表格合并每轮结论，供纵览。
> 状态口径：**[已落地]** = 行为不变 + 回归单测 + 复验通过；**[受限落地]** = 收口为单一入口
> 但未做全量改型；**[延后]** = 不阻塞，列入后续。

### 3.1 首轮 `#1–#7`

| 编号 | 严重度 | 主题 | 一句话 | 状态 |
| --- | --- | --- | --- | --- |
| `#1` | 高 | 契约 | `InstallDriverFile` 成/败契约不一致，`InstallSelected` 三路分支兜底 | 已落地 |
| `#2` | 高 | 编排/god-function | `CompareOSDriverView` 约 95 行混五职 | 已落地（内核拆分） |
| `#3` | 高 | 复制 | 三处"加载驱动列表并容忍失败"各卷一份 | 已落地（`mustDriverList`） |
| `#4` | 中 | 编排 | 独立 OS 读被串行化 | 已落地（并行 `loadDriverListsForOSIDs`） |
| `#5` | 中 | 抽象 | 来源审计优先级用命令式 if 链 | 已落地（`sourceEvidenceRules` 表） |
| `#6` | 中 | 模型 | `Driver` 三职 + 污染共享 DTO | 已落地（`cloneDrivers` 所有权分离） |
| `#7` | 中 | 抽象 | `runProcess` 伪输出捕获接口 | 已落地（整块删除） |

### 3.2 第二轮（`V1–V8`）

| 编号 | 严重度 | 主题 | 一句话 | 状态 |
| --- | --- | --- | --- | --- |
| `V1` | 高 | 契约 | `runProcess` 启动失败被压成 `-2` 且不报错，撕开 `#1` 的统一契约 | 已落地（`StartErr`） |
| `V2` | 高·柔术 | 复制 | "除当前 OS 外全部 OSID" 编排三处复制 | 已落地（`otherOSIDs`） |
| `V3` | 高/中 | 时间 | `formatTimestamp` 仍"UTC 但非 RFC3339" | 已落地（RFC3339） |
| `V4` | 中/高 | 模型 | `Driver` 三职 + `cloneDrivers` 只是"贴创可贴" | 受限落地 |
| `V5` | 中 | 进程 | 超时路径吞掉 `killTree` 错误、"射后即望"协程 | 已落地（`KillErr`） |
| `V6` | 中 | 编排 | 每驱动一次 `powershell.exe` 子进程风暴 | 延后 |
| `V7` | 中 | 复杂度 | `selectByCodes` 双重 O(N·M) 扫描 | 已落地（单遍） |
| `V8` | 低/中 | 编排 | `CompareOSDriverView` 仍是"决策+副作用"混合门 | 已落地（`presentDriverView`） |

### 3.3 第三轮（`R1–R3`）

| ID | 严重度 | 主题 | 一句话 | 状态 |
| --- | --- | --- | --- | --- |
| `R1` | 高·柔术 | 代码 | "OSID→OSName" 查找三处各写一遍，第二轮已标注未收口 | 已落地（`model.OSNameByID`） |
| `R2` | 高 | 债务 | `downloadVerified` 用无界 `for {}` 表达"至多刷新一次" | 已落地（有界双遍） |
| `R3` | 低/中 | 抽象 | `initCurrentSourceMap` 是无收益恒等转发壳 | 已落地（删除） |
| `V6` | 中 | 编排 | PowerShell 子进程风暴（复确认） | 维持延后 |

---

## 4. 逐轮台账（同模板展开）

> 每一轮严格复用同一模板：**结论速览 → 发现清单（按严重度）→ 实施记录 → 复验**。
> 三轮分别见 4.1 / 4.2 / 4.3，结构完全一致，只替换内容——这本身就是"举一反三"。

### 4.1 首轮 — 发现与落地（`#1–#7`）

**结论**：不是烂摊子，包划分方向正确，领域规则表是"代码柔术"。真正的问题是契约不一致、
一个 god-function、非原子编排——均已修复。放行。

#### 首轮发现清单（按严重度）

**`#1` [高·已修复] `InstallDriverFile` 契约不一致，`InstallSelected` 三路分支兜底**

`internal/install/install.go` 依扩展名返回 `(int, error)`，却有两种约定：`.msi/.inf/default`
失败时 `(非零码, nil)`；`.exe/.cab` 失败时 `(code, err)`。于是 `InstallSelected` 需同时分支判断
`err` 与 `code`；且 `.exe` 先跑内部兜底（`installEXE → finishEXEFallback`），应用层又跑第二层
交互式兜底，同一失败两处重复恢复，被糟糕契约隔开。

修复：统一契约为——
- `err==nil && code==0`：干净成功；
- `err==nil && code!=0`：可执行程序**已运行**并返回非零退出码（可交互重试）；
- `err!=nil`：无可用退出码的**终结性失败**。

`InstallSelected` 改为按**扩展名**的显式 `switch`；为各包类型补契约定死的单测。

**`#2` [高·已修复] `CompareOSDriverView` god-function**

约 95 行的单函数混五职（抓取/合并/过滤/比较/写 plan/打印/分区），与 `App`（缓存+日志）纠缠。
修复：收敛为薄编排（fetch → merge → select → 评估 → 持久化/打印 → 分区 → return），逐驱动
比较抽 `assessSelectedDrivers`（早返回替代嵌套 if），分区抽纯函数 `partitionViewDrivers`。

**`#3` [已修复] 三处"加载驱动列表并容忍失败"包裹几乎相同**

修复：新增严格版 `mustDriverList`（主视图必须成功），主视图改用并删除内联重写；备用合并复用
`loadDriverList`。新增 `mustDriverList` 单测。

**`#4` [已修复] 可分离却串行 — 独立读被串行化**

`LatestAcrossOS` 合并、`initAlternateSourceMap`、`refreshDriverURL` 逐个串行抓取。
修复：`loadDriverListsForOSIDs`/`driverListLoad` 零依赖 `sync.WaitGroup` 并行抓取并按 OSID 顺序
返回，`App.osDriverCache` 加 `sync.Mutex`；合并、备用源、URL 刷新确定性输出顺序不变；安装与
逐驱动评估仍串行。新增保序与容忍策略单测。

**`#5` [已修复] 来源审计优先级 if 链**

`audit.go` 的 `ResolveDriverSourceEvidence` 用"只设一次"闭包接长顺序 if。
修复：`sourceCategoryRule` 表 + `sourceEvidenceRules`，优先级变为**表顺序**；证据行收集与类别判定
解耦。新增 4 个优先级单测。

**`#6` [已修复] `Driver` 身兼三职**

同清单 §3.1 `#6`。修复：极简 `cloneDriver`/`cloneDrivers` 把合并行深拷贝为视图私有记录，
评估只写私有副本，共享缓存/API transport 行保持只读；`SourceAudit` 嵌套深拷贝。新增
`TestCloneDriversIsOwnedCopy`。

**`#7` [已修复] 假接口**

`ProcessResult.Stdout/Stderr` 永远同字节或空、生产从不读取。修复：整块**删除**捕获机制（优于
"合并为单一 Output"），`runProcess` 一律 `io.Discard`；更新 `TestRunProcessTimeoutKillsTree`。

**边界/规范观察（不阻塞）**：无浮点（`big.Rat` 尺寸避免）、无 1k 文件、enum 良好。
时间戳为 `UTC()` 但 `2006-01-02 15:04:05`（非 RFC3339）——**首轮仅记为低优先，第二轮 V3 才关闭**。

#### 首轮复验

| 检查 | 结果 |
| --- | --- |
| go build / vet / gofmt | OK / OK / 干净 |
| go test ./... | 全部包 ok，含新增单测 |

### 4.2 第二轮 — 独立全量复审 + 落地（`V1–V8`）

**结论**：整体健康，包边界清晰，领域规则走表。前两轮阻塞气味（>1k、朴素 if）不成立；
V1、V2 为两个高置信原则问题（V1 契约回归直接牵连 #1、V2 可柔术删除的复制），其余为中低危观察。
**未达放行条**（V1 需修）。随后的用户「一并处理」已落地。

#### `V1` [高·已落地] 启动失败被压成 `-2`

`install.go:57-59` 中 `cmd.Start()` 失败时 `return ProcessResult{ExitCode:-2}`，丢弃错误，
把"启动失败"错当"已运行返回退出码 -2"——直接撕开 `#1` 的统一契约。修复：`ProcessResult` 新增
`StartErr error`；`.msi/.inf/.cab/default/installINFPaths/RunPnPUtilWithTimeout` 在 `StartErr!=nil`
时返回 `(code, err)`；`RunPnPUtilWithTimeout` 不再把启动失败改为 exit 1。补
`TestInstallDriverFileStartFailureIsTerminal`、`TestRunProcessStartFailureCarriesError`。

#### `V2` [高·柔术 · 已落地] "除当前 OS 外全部 OSID" 三处复制

`view.go` 两处 + `install.go` 一处重复写"遍历 OsList → 跳当前 → 收其余"。修复：抽纯函数
`otherOSIDs(osList, excludeID) []string`，三处统一调用；顺带收敛 `ExportGUIView` 的 OSID→名称
查表重复扫描。新增 `TestOtherOSIDsExcludesCurrentAndPreservesOrder`。

#### `V3` [高/中 · 已落地] `formatTimestamp` 仍为"UTC 但非 RFC3339"

`helpers.go:19-21` 用 `t.UTC().Format("2006-01-02 15:04:05")`，违反 `AGENTS.md`（需 UTC+0 且严格
RFC3339、禁自定义字符串），且首轮仅标低优先未关闭。修复：改 `t.UTC().Format(time.RFC3339)`，历史
CSV/plan/Log 同源收敛；RFC3339 字典序正确。新增 `TestFormatTimestampIsRFC3339UTC`。

#### `V4` [中/高 · 受限落地] `Driver` 三职 / `cloneDrivers` 是"贴创可贴"

首轮 `#6` 用 `cloneDrivers` 已消除共享 DTO 污染，但 `Driver` 仍承载传输 + 领域 + 派生评估可变量，
`clone` 在每个入口重复"先深拷以免污染"。修复（受限）：本轮核实 `cloneDrivers` 已是**唯一**所有权
入口（无散落每例 clone），维持该单点并强化注释；**未**做 `Driver` 去评估字段的全量改型，因其会改
`guiExportPayload`/plan 依赖的 `Driver` JSON 线契约（不属"行为不变"）。全量建模作为后续非阻塞方向。

#### `V5` [中 · 已落地] 超时路径吞掉 killTree 错误

`install.go:66-71` `_ = killTree(...)` 静默吞错；等待 `done` 的协程可能未回收。修复：`ProcessResult`
新增 `KillErr error`（仅超时 + 杀失败非 nil）；`timeoutErr(prefix, result)` 在超时错误里捎带杀树
失败详情，替换各处 `"... timed out"` 直写。

#### `V6` [中 · 延后] 每驱动一次 PowerShell 子进程风暴

`assessSelectedDrivers` 对每个驱动调 `inventory.GetDeviceDriverVersions`，N 个驱动 → N 次
`powershell.exe` 子进程。理想柔术：全设备**一次**聚合查询再按 PnP 归并，子进程 N→1。延后理由：把
`GetDeviceDriverVersions` 从 `[]string` 改为 id→version 会改变错误时机与"无测试兜底的 PowerShell
语义"，不属"行为不变"。可排期后续。

#### `V7` [中 · 已落地] `selectByCodes` 双重 O(N·M)

第一遍滤出命中（O(N)），再每 code 内层扫描判 Missing（O(K·N)）。改为"请求 set + 一遍扫描"：
Selected/NotApplicable 顺序按驱动表，Missing 用 set 剩项判定，去内部二次全扫。既有 2 则测试通过。

#### `V8` [低/中 · 已落地] `CompareOSDriverView` 仍"决策+副作用"混合

`#2` 已拆比较内核，但 `CompareOSDriverView` 仍把 plan 持久化（:193）、表格打印（:198-205）、`Log`
与比较混在 64 行内。修复：抽 `presentDriverView(ctx, selected)` 为唯一副作用点；`CompareOSDriverView`
收敛为"构建+评估+分区"决策管道。行为不变。

#### 第二轮复验（第二轮第 7 节）

| 检查 | 结果 |
| --- | --- |
| go build / vet / test / gofmt | BUILD_OK / VET_OK / 全部 ok（含 V1/V2/V3） / 干净 |

### 4.3 第三轮 — 独立复审 + 落地（`R1–R3`）

**结论**：无阻塞；前两轮修复逐项核实有效（见 §4.4 复核表）。本轮落地 3 项行为不变修复，复确认
`V6` 延后。

#### `R1` [高·柔术 · 已落地] "OSID→OSName" 至少三处各写一遍

- `ExportGUIView`（`view.go:430-437`）内联双重 `for` 查 `currentOSName`/`listOSName`；
- `api.parse.findOSName`（原 173-179）同一逻辑放 api 包；
- `app.interactive.nextOSIndex` 依赖 `osList` 扫描。

第二轮 V2 已把其列为"OSID→名称查表的另一重复扫描，可复用 index map"但**未落地**。修复：新增规范纯
函数 `model.OSNameByID(osList, osID) string`（OS 单一查找源），`api.findOSName` 与 `ExportGUIView`
改调；行为 `""` 不变。新增 `internal/model/os_test.go` 锁定 命中/未命中/空表。

#### `R2` [高 · 已落地] `downloadVerified` 把"至多刷新一次"写成无界 `for {}`

首轮 H6 标记的两轮内仍未闭环，终止只能通读全函数。改为**显式有界**双遍：
`for pass := 0; pass < 2; pass++ { … 403 && pass==0 刷新 …; return }`；尾 return 明确"2 遍未收敛"。
行为一致（`pass==0` 允许刷新同原 `refreshAttempted`）。既有 `TestDownloadVerifiedRefreshes403URL` 通过。

#### `R3` [低/中 · 已落地] 删除无收益恒等转发壳 `initCurrentSourceMap`

`view.go` 的 `initCurrentSourceMap` = `buildDriverSourceMap` 一对一转发、未新增语义、仅在
生产 1 处+测试调用。删除后直接调 `buildDriverSourceMap`，测试同步改。

#### `V6` 复确认延后

同 §4.2 理由，维持延后（不阻塞正确性），记入后续。

#### 第三轮复验

| 检查 | 结果 |
| --- | --- |
| gofmt -l | 干净 |
| go vet ./... | VET_OK |
| go test ./... | 全部 ok（含新增 `TestOSNameByID`） |
| go build ./... | BUILD_OK |
| scripts/verify.ps1 | VERIFY_OK（14 步，0 失败） |

### 4.4 跨轮修复复核矩阵

> 第三轮对前两轮所有已修项逐一核实「在当前工作树仍有效」，无失效。

| 前轮项 | 第三轮核实 |
| --- | --- |
| `#1`/`V1` 成/败契约 + `StartErr` 终结性失败 | 有效：`.msi/.inf/.cab/default` 均 `StartErr != nil` 时返 `(code, err)` |
| `#2`/`V8` `CompareOSDriverView` + `presentDriverView` 分离 | 有效 |
| `#3` `mustDriverList` 严格版 | 有效 |
| `#4` `loadDriverListsForOSIDs` 并行 + 保序 + mutex 缓存 | 有效 |
| `#5` `sourceEvidenceRules` 优先级表 | 有效 |
| `#6` `clone` 所有权分离 | 有效，生产仅单一入口（`view.go:177`） |
| `#7` 删伪捕获 | 有效 |
| `V2` `otherOSIDs` 收敛三处 | 有效 |
| `V3` RFC3339 时间戳 | 有效 |
| `V5` 超时 killTree 错误捎带 | 有效（`KillErr` + `timeoutErr`） |
| `V7` `selectByCodes` 单遍 | 有效 |

---

## 5. 主题归并（举一反三）：同一份发现家族

> 把跨轮对**同一设计问题族**的发现归一，展示演进脉络而非孤立条目。

### 5.1 契约一致性与错误显式化（AGENTS 异常显式化 / 重试有边界）

- `#1`（首轮 · 统一成/败三态契约）
- `V1`（启动失败被压成退出码，撕开 `#1` —— 高置信复制回归）
- `V5`（超时吞掉 killTree 失败 → `KillErr` 显式化）
- `R2`（无界 `for {}` 重试 → 显式有界双遍）

**模式**：凡是"错误被压成另一个语义 / 循环边界靠读者推断"的地方，都落为显式 `error` / 有界边界。

### 5.2 复制与单一查找源（避免漂移）

- `#3`（三份驱动加载容忍包装）
- `V2`（三份收集 OSID 编排）
- `R1`（三份"OSID→OSName"查表）
- `R3`（恒等转发壳）
- 附带 `V7`（`selectByCodes` 二次扫描）

**模式**：同一事实/编排在多处手写复现时，抽单一规范函数并收敛所有权（`model`/`app`/`compare`）。

### 5.3 模型与所有权（DTO 污染 / 三职）

- `#6` / `V4`：`Driver` 三职（传输 + 领域 + 每跑 scratch）。`#6` 落地 `cloneDrivers`（视图私有副本），
  `V4` 收敛 ownership 到单一入口；**全量"去除评估字段"因 JSON 线契约而延后**。

### 5.4 god-function / 决策与副作用分离

- `#2` 拆分 `CompareOSDriverView`（抽出 `assessSelectedDrivers`/`partitionViewDrivers`）
- `V8` 再抽 `presentDriverView` 隔离 plan/stdout 副作用

### 5.5 时间与进程（AGENTS 边界）

- `V3`：时间戳归 UTC RFC3339（`AGENTS` 时间统一边界）
- `V6`：PowerShell 子进程风暴（编排健康，延后）

---

## 6. 验证矩阵（三轮汇总）

| 检查 | 首轮 | 第二轮 | 第三轮 |
| --- | --- | --- | --- |
| `go build ./...` | OK | BUILD_OK | BUILD_OK |
| `go vet ./...` | OK | VET_OK | VET_OK |
| `go test ./...` | 全部 ok | 全部 ok | 全部 ok |
| `gofmt -l cmd/ internal/` | 干净 | 干净 | 干净 |
| `scripts/verify.ps1` | — | — | VERIFY_OK（14 步） |

非页面型交付：无视觉/页面 smoke 需求。

---

## 7. 遗留 / 延后与后续方向

- **`V6`（延后）**：每驱动一次 `powershell.exe` 子进程风暴 → 聚合查询按 PnP 归并，子进程降 N→1。
  需改 `GetDeviceDriverVersions` 从 `[]string` 到 id→version，改变错误时序与无测试兜底的 PS 语义。
- **`V4` 受限落地 → 可选全量建模**：让 `DriverView` 携带 `driverEntry{Driver, Assess}`，`Driver` 彻底
  回归纯 transport。这超出当前"行为不变"约束（`guiExportPayload`/plan JSON 契约），作为后续排期方向。
- 其余三轮已落地项的剩余表达均为**不阻塞**的低观察，可归档。

---

## 8. 交付说明

本文件**取代** `docs/` 下此前全部热核评审轮次文档（`final`、`v2`、`round3`）。原三个独立文件
的内容已全部归一合并到本文件，且三个旧文件已替换为「已废弃 / Superseded」占位说明、指向本唯一权威
报告（物理删除可由具备删除能力的工具或 `rm`/git 完成；历史内容保留在 git 中）。`docs/` 现以本文件
为**唯一权威**审查结论。`WORKLOG.md`、`TECHNICAL.md` 为工程文档，予以保留。

> 交付物：本归一报告（三轮全部发现 `#1–#7 / V1–V8 / R1–R3` + 状态 + 验证矩阵）+ 既有源码改动与
> 单测 + 全程 `go build/vet/test/gofmt` 通过 + `scripts/verify.ps1` `VERIFY_OK`，均已在上文三轮
> 验证矩阵录入。