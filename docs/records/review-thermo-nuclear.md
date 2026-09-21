# 热核代码质量评审 — 全轮次归一汇总（单一权威报告）

> 状态：已归档。本报告是 1–8 轮评审的历史汇总，不再追加新轮次；后续评审应新起文档或取代本文件。现行结构规范见 `docs/spec/`。

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

各轮累计发现与状态一览：

| 轮次 | 发现 | 高优先级 | 状态 |
|------|------|---------|------|
| 首轮 | `#1–#7` | `#1/#2/#3` | 全部 [已落地]，`#2` 部分后续方向保留 |
| 第二轮 | `V1–V8` | `V1/V2/V3` | `V1–V5/V7/V8` 已落地；`V4` 受限落地；`V6` 延后 |
| 第三轮 | `R1–R3` | `R1/R2` | `R1–R3` 全部落地；`V6` 复确认延后 |
| 第四轮（原生迁移） | `N1–N12` | `N1/N2/N3` | `N1–N12` 全部已落地（`N3` 顺带修 `N2`） |
| 第五轮（复审施工） | `S1–S6` | `S1/S2` | `S1–S6` 全部已落地 |
| 第六轮（独立全量复审） | `F6.1–F6.5` | `F6.1/F6.2` | 见 §9；`F6.1`/`F6.2` 已落地（RFC3339 统一 + 单测），`F6.3–F6.5` 记录为不阻塞 |

- **放行**：第三轮最终**无阻塞项**；前两轮阻塞（`#1` 契约、`V1` 启动失败压码）均已闭环。
  第四轮（原生迁移分支 `19a5137` + `396c716`）原评审**不放行**，`N1–N12` 已全部落地：
  `N1` 恢复 `legacyps` 编译（常量移入 legacy 文件）；`N2`/`N3` 合并为单一 SetupAPI 枚举
  （`enumerateDevices()`）并恢复证据字段（Name/Class/InstallDate 不再抹空）；
  `N4`–`N8` 清理与契约收敛（含死常量 `regClassRoot/regEnumRoot`、reboot 映射 3010）；
  `N9`–`N12` 低项收口（包级绑定 / `fileversion_windows.go` / `isStringType` 纯函数）。
  复验全绿，含 `go build -tags legacyps`。
- **遗留**：`V4`（`Driver` 三职模型彻底拆分，受限落地）、`V6`（每驱动一次 PowerShell 子进程，
  已随原生迁移实质解决）、以及「`DriverView` 携带 `driverEntry{Driver, Assess}`」的可选建模
  方向——均不阻塞正确性，见 §7。

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
| 最大文件 | `app/view.go` 501 行（第五轮实测 466；第六轮越 500 上限，见 `F6.3`） |
| 其余大文件 | `audit/audit.go` 405、`compare/matching.go` 366、`app/app.go` 343、`app/install.go` 309 |
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

### 3.4 第四轮·原生迁移（`N1–N12`）

| ID | 严重度 | 主题 | 一句话 | 状态 |
| --- | --- | --- | --- | --- |
| `N1` | 高·构建断 | 契约 | `legacyps` oracle 引用已被删的 `PowerShellExe`，等价测试不可编译、从未运行 | 已落地（常量移入 legacy 文件） |
| `N2` | 高·行为回归 | 契约 | `GetDeviceEvidence` 用恒空 Name/Class/InstallDate 覆盖输入行，审计证据丢设备名/安装日期 | 已落地（随 `N3` 单一枚举恢复） |
| `N3` | 高·柔术 | 重复 | 两次完整 SetupAPI 枚举（`enumeratePresentDevices` + `GetNativeDeviceEvidence`）合一 | 已落地（`enumerateDevices()` 单遍） |
| `N4` | 中 | 死代码 | `var _ = filepath.Separator` 压未用 import；`fileExists` 包内零调用；`fileVersion` 恒等转发壳 | 已落地（含死常量 `regClassRoot/regEnumRoot`） |
| `N5` | 中 | 重复 | `decodeFirstMultiString` ≡ `decodeUTF16`（UTF16ToString 遇首 NUL 即停） | 已落地（并入 `decodeUTF16`） |
| `N6` | 中 | 魔数 | `IsAdministrator` 裸常量 + 残留思考注释（`TokenRead? use TOKEN_QUERY=0x0008`） | 已落地（命名常量 + 陈述注释） |
| `N7` | 中 | 抽象 | `NativeInstallEnabled` 恒真 + `NativeInstallAvailable` 预检与内检重复 + 同义反复测试 | 已落地（三件套删除） |
| `N8` | 中 | 契约 | 原生安装路径丢弃 `rebootRequired`，pnputil 兜底传播 3010，两路径语义分叉 | 已落地（映射 `errorSuccessRebootRequired`） |
| `N9` | 低 | 一致性 | 三种 DLL 绑定风格并存；`relaunchElevatedNative` 每次调用重绑 | 已落地（包级绑定） |
| `N10` | 低 | 正确性 | `GetOSInfo` caption 拼接启发式产出 "Windows 10 Pro Professional" | 已落地（删 EditionID 拼接） |
| `N11` | 低 | 拆分 | `native_full_windows.go` 440 行装约 7 职责 | 已落地（440→311；版本读取独立成 `fileversion_windows.go`） |
| `N12` | 低 | 测试 | `TestNativeReadStringRejectsNonStringType` 用非法句柄，未触达声称的类型分支 | 已落地（抽 `isStringType` 纯函数 + 真测试） |

### 3.5 第五轮·复审施工（`S1–S6`）

| ID | 严重度 | 主题 | 一句话 | 状态 |
| --- | --- | --- | --- | --- |
| `S1` | 高·柔术 | 性能/结构 | 单遍枚举只是函数级单遍；快照通道从轻量变重，流程级仍是 N+2 遍重枚举 | 已落地（`enumerateDevices` 拆身份轻遍 + `loadDriverEvidence` 显式加载；app 层版本索引每遍一次） |
| `S2` | 中高·契约 | 一致性 | `N8` 的收敛声明不成立：pnputil 把重启码 1 抹成 0，原生路径返回 3010 | 已落地（`normalizePnPUtilExitCode`：1→3010，两路径真实收敛 + 真测试） |
| `S3` | 中 | 重复 | `GetNativeDeviceEvidence` 与 `GetDeviceEvidence` 两张字段映射表；两条 `map[...]` 构建循环 | 已落地（`applyTo` + `indexRows` 单一查找源） |
| `S4` | 低 | API 面 | `GetNativeDeviceEvidence` 生产零调用、仅供 smoke 测试 | 保留为规范投影（经 `applyTo` 收敛），并在 `native_other.go` 维持 stub |
| `S5` | 低 | 魔数/编码 | `SPDRP_*` 内联魔数；installDate 手写小端循环 + 20 字节裸 DEVPROPKEY | 已落地（命名常量；`devPropKey` 结构体 + `binary.LittleEndian` + 纯函数 `formatFileTime` + 测试） |
| `S6` | 低 | API 面 | `PowerShellExe` 导出仅同包使用 | 已落地（小写 `powershellExe`） |

---

## 4. 逐轮台账（同模板展开）

> 每一轮严格复用同一模板：**结论速览 → 发现清单（按严重度）→ 实施记录 → 复验**。
> 各轮分别见 4.1 / 4.2 / 4.3 / 4.5 / 4.6，结构完全一致，只替换内容——这本身就是"举一反三"。

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

### 4.5 第四轮 — 原生迁移（`19a5137` + `396c716`）独立复审

> 对象：把 PowerShell 运行时整体迁移为 SetupAPI / CfgMgr32 / 注册表 / SMBIOS /
> ShellExecuteExW / DiInstallDriverW 的分支（`19a5137`，+1908/−240），及 WPF 修复
> （`396c716`）。方法同前轮：全量通读 + grep 佐证 + 实机 `go build/vet/test/gofmt` 复验 +
> `go build -tags legacyps` 编译校验。

**结论**：默认构建链全绿（build / vet / test / gofmt 干净），运行时 PowerShell 全删是**真实
简化**（`execPowershell` / `runJSON` 整块删除，子进程风暴收敛）；`GetDeviceDriverVersions`
原生版按输入对位返回 `""` 占位，优于旧 PS 版"缺项即移位"的数组错位；`396c716`（`WaitForExit`
保 ExitCode + XAML BOM）最小且正确。原评审因等价性验证故事不可执行（`N1`）与证据管线静默
回归（`N2`）**不放行**；以下 `N1–N12` 全部落地后复验全绿（含 `-tags legacyps`），放行。

#### `N1` [高·构建断 · 已落地] `legacyps` oracle 无法编译

`windows.go` 随迁移删除了 `const PowerShellExe`，`windows_legacy_ps.go:21` 仍引用：

```
$ go build -tags legacyps ./...
internal\inventory\windows_legacy_ps.go:21:34: undefined: PowerShellExe
```

`native_equivalence_smoke_test.go` 同样要求 `legacyps` tag。因此本次迁移自带的验收故事——
"native vs 旧 PS 等价性对比"——**从未编译通过、从未运行过**。修复：常量移入
`windows_legacy_ps.go`（legacy 专用），并把 `go build -tags legacyps` 纳入复验清单。
已落地：`windows_legacy_ps.go` 恢复 `const PowerShellExe`，`go build/vet -tags legacyps` 通过。

#### `N2` [高·行为回归 · 已落地] `GetDeviceEvidence` 抹空 Name/Class/InstallDate

`readDriverClassProperties`（`native_windows.go:159-169`）返回的 Device 行 Name / Class /
DeviceID / InstallDate **恒为空**；`GetDeviceEvidence`（`native_full_windows.go:425-434`）
无条件用 `row.Name/row.Class/row.InstallDate` 覆盖输入行 → 每条设备的名称、类别、安装日期被
写空。用户可见：`audit.go:300` 证据行退化为 `Device: ; version=…; install-date=`，
`audit.go:251` 规则名退化为 `device:`。旧 PS 实现以 `FriendlyName/Class/InstallDate` 填充。
另外，无 class driver 的设备（`Driver` 键或版本缺失）被静默丢弃，证据行数少于输入。修复随
`N3`：单一枚举遍同时采集 name/class 与驱动属性。已落地：证据行恢复 name/class/installDate；
`InstallDate` 经 `SetupDiGetDevicePropertyW` 读 `DEVPKEY_Device_InstallDate`（FILETIME→UTC），
无驱动设备按输入保留（与旧 PS 版一致，不再静默丢弃）。

#### `N3` [高·柔术 · 已落地] 两次完整 SetupAPI 枚举合并为一次

`enumeratePresentDevices`（`native_full_windows.go:57`）与 `GetNativeDeviceEvidence`
（`native_windows.go:81`）各自执行 `SetupDiGetClassDevs` + `SetupDiEnumDeviceInfo` +
每设备属性读取——同一棵 PnP 树被完整扫两遍。柔术动作：抽 `enumerateDevices()` 一次返回全量
行（instanceID / name / class / hardwareID / 驱动属性 / installDate），
`GetLocalDeviceSnapshot` / `GetDeviceEvidence` / `GetDeviceDriverVersions` /
`GetNativeDeviceEvidence` 全部降为纯投影。既删一遍枚举，又顺带修复 `N2`（name/class 同遍
采集），`N11` 的职责拆分也在此收敛。已落地：`enumerateDevices()`（`native_windows.go`），
`native_full_windows.go` 440→311 行。

#### `N4` [中·死代码 · 已落地] 未用 import 压制 + 零调用函数 + 转发壳

- `native_full_windows.go:440` `var _ = filepath.Separator` 是未用 import 的压制行——删
  import 与压制行；
- `native_full_windows.go:316` `fileExists` 包内零调用（`install/extraction.go:137` 另有
  独立一份）；
- `native_full_windows.go:309` `fileVersion` 是 `nativeFileVersion` 的恒等转发壳（唯一调用
  在 `:279`），注释在自我辩护——直接调 `nativeFileVersion`。

已落地：三处全删；另删死常量 `regClassRoot` / `regEnumRoot`（定义后从未使用）。

#### `N5` [中·重复 · 已落地] `decodeFirstMultiString` ≡ `decodeUTF16`

`syscall.UTF16ToString` 遇首 NUL 即停，因此 `decodeFirstMultiString`（`:101`）与
`decodeUTF16`（`native_windows.go:211`）行为逐字节相同。同包两份 byte→UTF16 解码合并为一；
同包三份解码器（另 `decodeASCII`）收为两份。已落地：并入 `decodeUTF16`；
`nativeDeviceRegistryString` 的 REG_MULTI_SZ / REG_SZ 分支统一走 `decodeUTF16`，
测试改为 `TestDecodeUTF16MultiString`。

#### `N6` [中·魔数/注释 · 已落地] `IsAdministrator` 裸常量 + 思考残留

`native_full_windows.go:369-386`：`0xffffffffffffffff`（GetCurrentProcess 伪句柄）、`0x0008`
（TOKEN_QUERY）、`20`（TokenElevation）均为裸数，注释还残留决策痕迹
`// PROCESS_QUERY_INFORMATION, TokenRead? use TOKEN_QUERY=0x0008`。改为命名常量，注释陈述事实。
已落地：`getCurrentProcess` / `tokenQuery` / `tokenElevation` / `tokenElevationEnabled` 命名常量。

#### `N7` [中·死抽象 · 已落地] `NativeInstallEnabled` / `NativeInstallAvailable` 三件套

`NativeInstallEnabled` 恒真（`install/native_windows.go:33`，`native_other.go` 同步），
`NativeInstallAvailable` 的 DLL 预检与 `InstallNativeINF` 内部 `Load()` 重复，
`TestNativeInstallAlwaysEnabled` 测恒真函数（同义反复）。柔术动作：删除 Enabled / Available
与该测试，`installNativeOrPnPUtil` 直接"先试 `InstallNativeINF`，失败落 pnputil"——行为不变，
少两个 API 面。`TestNativeInstallSmoke` 已直接走 `installNativeOrPnPUtil` 验兜底，不受影响。
已落地：三件套删除；`diInstallDriver` 为包级 `LazyProc`，DLL 不可用经
`InstallNativeINF` 的 `DiInstallDriver failed` 错误落入 pnputil 兜底（`LazyProc` 首调时
`Load`，错误语义等价于原预检）。

#### `N8` [中·契约分叉 · 已落地] 原生路径丢弃 reboot 标志

`install/install.go:255-256`：原生成功路径 `if _, err := InstallNativeINF(...); err == nil
{ return 0, nil }` 丢弃 `rebootRequired`；pnputil 兜底则传播 3010（`InstallSucceeded` 视为
"成功+需重启"）。同一次安装走两条路径，对"是否需重启"语义不同。至少把原生路径的
`rebootRequired` 映射为 3010 使两路径收敛。已落地：新增常量
`errorSuccessRebootRequired = 3010`，原生需重启返回 3010，两路径收敛
（`compare.InstallSucceeded(3010)` 为真，语义不变）。

#### `N9–N12` [低·观察 · 全部已落地]

- `N9`：三种 DLL 绑定风格并存——inventory 包级 procSet（`native_windows.go:53`）、install
  包级结构体（`native_windows.go:22`）、app 每次调用重绑（`native_elevate_windows.go:36`）。
  统一为包级绑定，或抽最小 `winapi` 共享包。已落地：app 侧提为包级
  `var` 块（shell32 / kernel32 / 四个 proc），install 侧收敛为单个包级 `LazyProc`。
- `N10`：`GetOSInfo` 的 caption 拼接启发式（`:157-160`）在 Pro 等 SKU 产出 "Windows 10 Pro
  Professional"（Contains 对 "Professional" vs "Pro" 不等价）；等价测试只比 `OSName`
  （kind+bits），掩盖 Caption 差异。建议直接只用 `ProductName`，删拼接。已落地：删除
  EditionID 拼接，caption 即 `ProductName`（默认 "Windows"）。
- `N11`：`native_full_windows.go` 440 行装约 7 个职责（注册表封装 / 机器身份 / OS 身份 /
  应用 / 供应证据 / 文件版本 / 令牌提升 / 证据归并），未破 500 但已到拆分线；`IsAdministrator`
  可随 app 提升代码就近，版本读取可独立成文件。已落地：枚举与注册表封装归入
  `native_windows.go`（309 行），版本读取独立成 `fileversion_windows.go`（58 行），
  `native_full_windows.go` 440→311 行。
- `N12`：`TestNativeReadStringRejectsNonStringType`（`native_full_test.go:62`）以 h=0 调用，
  `RegQueryValueExW` 先以 ERROR_INVALID_HANDLE 失败，**从未触达**注释声称的 REG_SZ 类型分支——
  测试因错误的理由通过。已落地：抽纯函数 `isStringType`，`nativeReadString` 与测试
  （`TestIsStringType`，覆盖 0/1/2/3/4/7 六型）均以它为准。

#### 第四轮复验（落地后）

| 检查 | 结果 |
| --- | --- |
| go build ./... | BUILD_OK |
| go vet ./... | VET_OK |
| go test ./... | 全部 ok（含重写的 `TestIsStringType` / `TestDecodeUTF16MultiString`） |
| gofmt -l | 干净 |
| go build -tags legacyps ./... | BUILD_OK（`N1` 修复后） |
| go vet -tags legacyps ./... | VET_OK |

### 4.6 第五轮 — 复审施工（`S1–S6`）

> 对象：第四轮落地后的未提交工作区（`N1–N12` 修复本身）。独立复审放行前两处声明失实与
> 一处回归：`S1`（快照变重 + 流程级仍多遍枚举）、`S2`（`N8` 收敛声明不成立）、`S3`（字段映射
> 表重复）。`S1–S6` 全部落地后复验全绿。

#### `S1` [高·柔术 · 已落地] 快照通道回归 + 流程级多遍枚举

`enumerateDevices()` 把原本轻量的快照通道（纯 SetupAPI 身份字段）升级成每设备 2 次
`RegOpenKeyEx` + 5 次 `RegQueryValueEx` 的重遍历，`GetLocalDeviceSnapshot` 只用 4/9 个字段。
且 `assessSelectedDrivers` 按驱动逐个调 `GetDeviceDriverVersions`（`view.go:235`），每次重扫
整棵 PnP 树——视图流程实际是 N+2 遍重枚举。柔术动作：`enumerateDevices` 拆为身份轻遍
（identity + installDate），注册表重的 class-key 证据收敛为 `loadDriverEvidence(rows)`，
仅证据入口点调用；app 层新增 `deviceVersionIndex`（去重 pnpIDs，单次 `GetDeviceDriverVersions`），
`assessSelectedDrivers` / `verifyInstalled` 每遍一次索引，`localDriverState` 降为纯对齐纯函数。
快照恢复轻量，主流程 N 遍枚举压为 1 遍。

#### `S2` [中高·契约 · 已落地] `N8` 收敛声明不成立

`RunPnPUtilWithTimeout` 把 pnputil 的重启码 1 抹成 0（信号丢失），原生路径返回 3010——两条
路径对同一结果产出不同退出码，第四轮台账的「两路径收敛」声明与代码不符。落地：
`normalizePnPUtilExitCode`（1→`errorSuccessRebootRequired`）+ `TestNormalizePnPUtilExitCode`；
`install.go` 三处注释改为与代码一致的事实陈述。

#### `S3` [中·重复 · 已落地] 字段映射表与索引循环双重复

`GetNativeDeviceEvidence`（9 字段）与 `GetDeviceEvidence`（7 字段）两张手写映射表——正是 `N2`
那类字段表漂移的温床；`GetDeviceEvidence` / `GetDeviceDriverVersions` 两条近同 `map[...]`
构建循环。落地：`nativeDeviceRow.applyTo(*model.Device)`（映射唯一化）+
`indexRows(rows)`（索引唯一化），`GetNativeDeviceEvidence` 保留为规范「全量驱动证据」投影
（smoke 测试仍使用，`native_other.go` stub 同步保留）。

#### `S4` [低 · 已落地] `GetNativeDeviceEvidence` 仅供 smoke 测试

维持导出为规范投影，不删：smoke 测试（`native_smoke_test.go`）合法地需要「全量驱动证据」
入口，删除只会逼测试走两步合成。经 `S3` 的 `applyTo` 收敛后字段表不再漂移。

#### `S5` [低 · 已落地] 魔数与手写编码

`SPDRP_DEVICEDESC/HARDWAREID/CLASS`（0x0/0x1/0x7）命名常量；`DEVPROPKEY` 类型化为
`devPropKey{fmtid [16]byte; pid uint32}` 结构体；FILETIME 解码改 `binary.LittleEndian.Uint64`
并抽纯函数 `formatFileTime`（含 `TestFormatFileTime`：零值→空串、2020-01-01 定点校验）。

#### `S6` [低 · 已落地] `PowerShellExe` 导出无谓

同包唯一使用，降级为 `powershellExe`。

#### 第五轮复验（落地后）

| 检查 | 结果 |
| --- | --- |
| go build ./... | BUILD_OK |
| go vet ./... | VET_OK |
| go test ./... | 全部 ok（含新增 `TestNormalizePnPUtilExitCode` / `TestFormatFileTime`） |
| gofmt -l | 干净 |
| go build -tags legacyps ./... | BUILD_OK |
| go vet -tags legacyps ./... | VET_OK |

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

## 6. 验证矩阵（各轮汇总）

| 检查 | 首轮 | 第二轮 | 第三轮 | 第四轮 | 第五轮 | 第六轮 |
| --- | --- | --- | --- | --- | --- | --- |
| `go build ./...` | OK | BUILD_OK | BUILD_OK | BUILD_OK | BUILD_OK | BUILD_OK |
| `go vet ./...` | OK | VET_OK | VET_OK | VET_OK | VET_OK | VET_OK |
| `go test ./...` | 全部 ok | 全部 ok | 全部 ok | 全部 ok | 全部 ok | 全部 ok |
| `gofmt -l cmd/ internal/` | 干净 | 干净 | 干净 | 干净 | 干净 | 干净 |
| `scripts/verify.ps1` | — | — | VERIFY_OK（14 步） | — | — | — |
| `go build -tags legacyps ./...` | — | — | — | **FAIL（评审时）→ BUILD_OK（`N1` 落地后）** | BUILD_OK | BUILD_OK |
| `go vet -tags legacyps ./...` | — | — | — | VET_OK（`N1` 落地后） | VET_OK | VET_OK |

非页面型交付：无视觉/页面 smoke 需求。

---

## 7. 遗留 / 延后与后续方向

- **第四轮 `N1–N12`（已全部落地）**：`N1` 常量移入 `windows_legacy_ps.go` 并将
  `go build -tags legacyps` 纳入复验；`N2`+`N3` 合并为单一枚举（`enumerateDevices()`）并恢复
  Name/Class/InstallDate 证据；`N4`–`N12` 的清理与契约收敛见 §4.5。落地后逐项复核完成。
  后续方向（不阻塞）：原生枚举按需缓存（`view.go` 每驱动一次全枚举），等价性 smoke 在
  实机上跑一遍 `LENOVO_NATIVE_EQUIV_SMOKE=1`。
- **`V6`（随原生迁移实质解决）**：运行时 PowerShell 子进程风暴已随 `19a5137` 整块移除；
  仅 `legacyps` 测试 oracle 保留 PS，与运行时无关。
- **`V4` 受限落地 → 可选全量建模**：让 `DriverView` 携带 `driverEntry{Driver, Assess}`，`Driver` 彻底
  回归纯 transport。这超出当前"行为不变"约束（`guiExportPayload`/plan JSON 契约），作为后续排期方向。
- 其余三轮已落地项的剩余表达均为**不阻塞**的低观察，可归档。

---

## 8. 交付说明

本文件**取代** `docs/` 下此前全部热核评审轮次文档（`final`、`v2`、`round3`）。原三个独立文件
的内容已全部归一合并到本文件，且三个旧文件已替换为「已废弃 / Superseded」占位说明、指向本唯一权威
报告（物理删除可由具备删除能力的工具或 `rm`/git 完成；历史内容保留在 git 中）。`docs/` 现以本文件
为**唯一权威**审查结论。`WORKLOG.md`、`TECHNICAL.md` 为工程文档，予以保留。

> 交付物：本归一报告（前三轮全部发现 `#1–#7 / V1–V8 / R1–R3` + 状态 + 验证矩阵）+ 既有源码改动与
> 单测 + 全程 `go build/vet/test/gofmt` 通过 + `scripts/verify.ps1` `VERIFY_OK`，均已在上文三轮
> 验证矩阵录入。

---

## 9. 第六轮 — 独立全量复审（Round 6，2026）

> 方法（与前五轮完全同构）：**独立全量重读** `cmd/` + `internal/` + `wpf/` 每一份源码；逐项核实
> 前五轮全部已落地 findings 在当前 `main` 仍有效（不复燃，见 §9.3 复核矩阵）；再按热核技能
> （抽象 / 大文件 / 意大利面 / 时间与契约边界 / 复制）**新猎**新增问题，均以 `grep`/行号佐证；
> 末了跑 `go build/vet/test/gofmt`（+ `-tags legacyps`）复验。

### 9.1 结论速览

放行条中「>1k 文件 / 朴素 if 链」两条阻塞气味依然不成立：最大文件为 `app/view.go` 501 行，
其次 `audit/audit.go` 405、`compare/matching.go` 366、`app/install.go` 309。**没有阻塞项。**

前五轮结构修复在当前树**全部有效**（§9.3）。本轮没有「可柔术整体删除一整类复杂度」的信号——
包边界、声明式规则表、单一查找源都已到位。真正的问题集中在**时间统一边界在原生证据路径上的
一处置漏 + 一处回潮**（`F6.1`/`F6.2`，与 `V3` 同族，只是 `V3` 当年只覆盖了 `formatTimestamp`，
没有覆盖第四轮原生迁移新增的证据格式化），以及文件规模、无界循环一致性、跨层复制三处低危观察。

| 序号 | 严重度 | 主题 | 一句话 |
| --- | --- | --- | --- |
| `F6.1` | 高 | 时间 | 原生 `InstallDate` 非 RFC3339 —— [已落地] 改 RFC3339 + 单测 |
| `F6.2` | 中/高 | 时间 | `PackageCreationTime` 本地时区 + 非 RFC3339 —— [已落地] `UTC().Format(RFC3339)` |
| `F6.3` | 低 | 文件规模 | `app/view.go` 501 行首次越过 AGENTS 500 行上限（此前 466） |
| `F6.4` | 低 | 一致性 | `expandWindowsEnv` 又是无界 `for`（R2 已把同类收口为有界双遍） |
| `F6.5` | 低 | 跨层复制 | Windows 命令行引号算法在 `helpers.go` 与 `wpf/worker.ps1` 各一份 |

落实进度：`F6.1`/`F6.2` 已随本轮**整体落地**（行为不变，RFC3339 统一 + 单测，见 §9.2）；
`F6.3` 拆 `export.go`、`F6.4` 显式有界、`F6.5` WPF 侧备注为后续非阻塞加工方向。

### 9.2 发现清单（按严重度）

#### `F6.1` [高 · 时间边界 · 回潮] 原生 `InstallDate` 非 RFC3339

- 位置：`internal/inventory/native_windows.go:236` —— `formatFileTime` 末尾
  `return time.Unix(secs, 0).UTC().Format("2006-01-02 15:04:05")`。
- 问题：值是 `UTC` 的，但**线格式是自定义字符串、非 RFC3339**。这正是 `V3`（第二轮）关闭的同一类
  —— 但 `V3` 只把 `formatTimestamp`（`helpers.go:20`）统一为 `time.RFC3339`；第四轮原生迁移
  （round 4 落地的 `formatFileTime`）**在 `V3` 之后新增**了这第二处格式，前几轮从未覆盖到。
  产物流向 `model.Device.InstallDate` → 审计证据行（`audit.go:300` `install-date=`）→ plan 落盘，
  均属 `AGENTS.md` 第三/七节「全系统接口强制 UTC+0 + RFC3339、禁自定义字符串」边界。且
  `latestSourceHistory` 的 Timestamp 排序依赖 RFC3339 字典序——本处非 RFC3339，一致性存疑。
- 建议：改 `...Format(time.RFC3339)`（行为不变，仍 UTC 且字典序正确）；对 `formatFileTime`
  补定点单测，锁定 `2020-01-01T00:00:00Z` 形式。
- **已落地（行为不变）**：`native_windows.go` 改 `Format(time.RFC3339)`；
  `TestFormatFileTime` 期望更新为 `"2020-01-01T00:00:00Z"`；`go test ./internal/inventory/` ok。

#### `F6.2` [中/高 · 时间边界 · 漏网] `PackageCreationTime` 本地时区 + 非 RFC3339

- 位置：`internal/app/evidence.go:111` 与 `:117` —— `packageCreation = info.ModTime().String()`。
- 问题：`FileInfo.ModTime()` 返回**本地时区**时刻，`Time.String()` 是 Go 默认格式
  （`2006-01-02 15:04:05.999… -0700 MST`）——既非 UTC 也非 RFC3339。该值经
  `packageEvidence.PackageCreationTime` → `audit.go:302` `created=` 证据行 → plan 落盘。与
  `F6.1`/`V3` 同族。
- 建议：改 `info.ModTime().UTC().Format(time.RFC3339)`。
- **已落地（行为不变）**：`evidence.go` 两处 `ModTime().String()` 改
  `ModTime().UTC().Format(time.RFC3339)`（新增 `time` import）；`go test ./internal/app/` ok。

#### `F6.3` [低 · 文件规模] `app/view.go` 501 行越过 500 行上限

- 位置：`internal/app/view.go`（501 行）。
- 问题：`AGENTS.md` 第七节「文件 ≤ 500 行」；第五轮台账实测为 466 行，现 501 行，**首次越过仓库
  自身上限**（仍远未到热核技能预设的 1k 阻塞线，故不阻塞）。
- 建议：把 `ExportGUIView` + `guiExportPayload`/`guiDriverRow`（`view.go` 约尾 40 行）抽到
  `internal/app/export.go`；行为不变。

#### `F6.4` [低 · 一致性] `expandWindowsEnv` 用无界 `for 循环`

- 位置：`internal/inventory/native_full_windows.go:209/212`（`expandWindowsEnv`）。
- 问题：第 2 轮 `R2` 已把 `downloadVerified` 的无界 `for {}`（终止需通读全函数）收口为**显式有界**
  双遍；本函数又是同样无界、靠逐字符 `%` 消耗在末尾隐性收敛。实际会收敛（每次至少消掉一个变量
  占位），但违背既定「显式有界 / 边界可由读者直接确认」的惯例。
- 建议：改为前置「无 `%` 即 return」的有界显式结构（`for` + 明确不变量）。

#### `F6.5` [低 · 跨层复制] Windows 命令行引号算法两份

- 位置：`internal/app/helpers.go:59` `quoteWindowsArgument` 与 `wpf/worker.ps1:97`
  `ConvertTo-CommandLineArgument`。
- 问题：同一 CMD 命令行引号算法在 Go 与 WPF(PowerShell) 各写一份，后续改一边漏一边会喂出漂移。
  （跨 Go/WPF 层无法直接复用同一函数，属已知边界，仅记录不阻塞——与 §7 的跨层注意一致。）

### 9.3 前五轮落实复检矩阵（第六轮逐一核实）

> 对每一已落地项在当前工作树核实，无失效。

| 前轮项 | 第六轮核实 |
| --- | --- |
| `#1`/`V1` 成/败三态契约 + `StartErr` | 有效：`install/install.go` 各包型先判 `StartErr`/`TimedOut` 再返回，契约注释一致 |
| `#2`/`V8` `CompareOSDriverView` + `presentDriverView` 分离 | 有效：`view.go` 决策管道与副作用单点分离 |
| `#3` `mustDriverList` 严格版 + `loadDriverList` 软漏 | 有效：`view.go:173/105` |
| `#4` `loadDriverListsForOSIDs` 并行 + 保序 + mutex 缓存 | 有效：`view.go:84` + `app.go` `osDriverCacheMu` |
| `#5` `sourceEvidenceRules` 优先级表 | 有效：`audit.go:223` |
| `#6`/`V4` `cloneDrivers` 单一所有权入口 | 有效：`view.go` 合并后一处深拷 |
| `#7` 删伪捕获 `runProcess` | 有效：`install.go` `io.Discard` |
| `V2` `otherOSIDs` 收敛三处 | 有效：`view.go:362`（比较/源映射/URL 刷新共用） |
| `V3` `formatTimestamp` RFC3339 | 有效：`helpers.go:21`（唯一；`F6.1`/`F6.2` 为漏网新点） |
| `V5` `KillErr`/`timeoutErr` | 有效：`install.go:54/96` |
| `V7` `selectByCodes` 单遍 | 有效：`helpers.go:89` |
| `R1` `model.OSNameByID` 单一查表 | 有效：`model.go:116` + `view.go:460/461`/`parse.go:174` |
| `R2` `downloadVerified` 有界双遍 | 有效：`install.go:149` |
| `R3` 删除恒等转发壳 | 有效 |
| `N1` `legacyps` 恢复 `powershellExe` 可编译 | 有效：`windows_legacy_ps.go:22` |
| `N2/N3` 单一枚举 `enumerateDevices()` | 有效：`native_windows.go:106` |
| `N7` 删除 `NativeInstallEnabled`/`Available` 三件套 | 有效：仅 `diInstallDriver` 包级 `LazyProc` |
| `N8/S2` `normalizePnPUtilExitCode` 3010 | 有效：`install.go:123` |
| `S1` `deviceVersionIndex` 每遍一次枚举 | 有效：`view.go:141` |
| `S3` `applyTo`/`indexRows` 单一映射 | 有效：`native_windows.go:158/148` |

> 一处备注（非失效）：`V4` 的「`Driver` 三职全量拆分」仍按 §7 维持**延后**，本次核实克隆所有权
> 单点仍唯一，不构成回归并保持不变。

### 9.4 验证矩阵（第六轮）

| 检查 | 结果 |
| --- | --- |
| `go build ./...` | BUILD_OK |
| `go vet ./...` | VET_OK |
| `gofmt -l cmd/ internal/` | 干净 |
| `go test ./...` | 全部包 ok（`api/app/audit/compare/download/install/inventory/model/pathutil/plan`） |
| `go build -tags legacyps ./...` | BUILD_OK |
| `go vet -tags legacyps ./...` | VET_OK |

> 非页面型交付：无视觉/页面 smoke 需求。

---

## 10. 第七轮 — 独立全量复审（Round 7，仅看代码）

> 方法（与前六轮同构，但本次按用户要求**不参考前六轮报告 / 不看代码注释**，仅以
> `cmd/ + internal/ + wpf/ + 脚本` 的源码为准独立重读）：逐文件通读 → 实测文件规模 →
> 以热核技能维度（抽象 / 大文件 / 意大利面条 / 边界 / 时间与进程 / 复制）新猎问题 →
> 实机 `go build/vet/test/gofmt`（+ `-tags legacyps`）复验 → 落地行为不变修复 + 单测。

### 10.1 结论速览

代码库整体健康：包边界清晰（`api/app/audit/compare/download/install/inventory/model/
plan/pathutil`），领域规则走声明式表（`vendorRules` / `driverMatchRules` /
`softwareRules` / `sectionStartRules` / `fieldExtractRules` / `sourceEvidenceRules`
/ `sourceCategoryRule`），并行取数与缓存集中（`cachedDriverObjects` +
`loadDriverListsForOSIDs`）。**没有接近 1000 行的文件**，因此热核技能预设的两条
阻塞性气味（文件超 1k、朴素意大利面链）**不成立，本轮无阻塞项**。

| ID | 严重度 | 主题 | 一句话 | 状态 |
| --- | --- | --- | --- | --- |
| `R7-1` | 中·文件规模 | `app/view.go` 501 行越过 500 上限 | 抽 `ExportGUIView` + `guiExportPayload/guiDriverRow` 入 `export.go`，`view.go` 425 行 | 已落地 |
| `R7-2` | 中·复制 | 原生证据两条入口各枚举一遍 + 重建索引 | 抽 `evidenceRows()` 单一「枚举+证据+索引」| 已落地 |
| `R7-3` | 低/中·复制 | `HistoryRecord` 列序在 header/写/读三处各写一遍 | 列序单一来源（后续方向，不阻塞） | 记录 |
| `R7-4` | 低·一致性 | `expandWindowsEnv` 无界 `for {}`（终止靠 `%` 消耗推断） | 改为显式有界循环（64 次 + 不变量注释）| 已落地 |
| `R7-5` | 低·跨层复制 | Windows 命令行引号算法在 Go 与 WPF 各一份 | 跨层不可复用，双端均有测试，记录漂移风险 | 记录 |

### 10.2 规模事实（实测）

| 指标 | 数值 |
|------|------|
| 最大文件（本轮落地后） | `compare/matching.go` 366、`inventory/native_windows.go` ≈347、`app/app.go` 343、`install/install.go` 358 |
| `app/view.go` | 501 → **425**（`R7-1` 落地后） |
| `app/export.go`（新增） | ≈97 |
| `go build / vet / test / gofmt` | 见 §10.4 |

> 上一标的 `view.go` 恰为 501 行、唯一越 500 上限的 Go 文件；本轮将其对齐关注拆出后，
> 全仓 Go 文件均回到 500 行以内。

### 10.3 发现清单（按严重度）

#### `R7-1` [中 · 文件规模 · 已落地] `app/view.go` 越过 500 行上限

`internal/app/view.go` 实测 501 行，是**全仓唯一越过 AGENTS.md 第七节「文件 ≤ 500 行」**的
Go 文件。其尾部约 76 行（`guiExportPayload` / `guiDriverRow` / `ExportGUIView`）是「GUI 导出」这一
独立关注点，与同文件前半的比较/合并/分区管道职责无关。落地：把这三段整体抽入新的
`internal/app/export.go`（承同包，`ExportGUIView` 仍由 `app.Run` 调用，线契约与行为不变）；
`view.go` 501 → 425 行，并顺带移除为此释放的 `encoding/json` 依赖。

#### `R7-2` [中 · 复制 · 已落地] 两个原生证据入口各枚举一遍

`GetDeviceEvidence`（`native_full_windows.go`）与 `GetDeviceDriverVersions`
（`native_full_windows.go`）都执行同一三段前缀：`enumerateDevices()` → `loadDriverEvidence(rows)` →
`indexRows(rows)`。落地：抽 `evidenceRows() (map[string]nativeDeviceRow, error)` 为这两处的单一枚举
+ 证据 + 索引来源（不动 `GetNativeDeviceEvidence`，它保留枚举顺序的纯投影无需索引）。行为不变。

#### `R7-3` [低 · 复制 · 记录，不落地] `HistoryRecord` 列序三处手工编排

`historyHeader` 定义列序，`WriteHistoryRecord` 再按字面写一次这 14 个字段，`ReadHistory` 又按列名
读一遍。若增/删/改一列，三处需同步，否则历史 CSV 静默漂移。非阻塞（当前一致且由
`ReadHistoryStripsBOM` 兜底），可作为后续「以单一列序切片构造读与写」的方向，本次不改全量以保持
行为与 CSV 线格式严格不变。

#### `R7-4` [低 · 一致性 · 已落地] `expandWindowsEnv` 无界循环

`native_full_windows.go` 的 `expandWindowsEnv` 用无界 `for {}`，终止只能靠「每次至少消一个 `%`
对」推断（与 `downloadVerified` 早已收口的有界双遍一致，但本函数当时仍是同一类特例）。落地：改显式
有界 `for expansion := 0; expansion < maxExpansions; expansion++`（64），每轮消除至少一个
`%VAR%`；正常路径（无自我注入的 `%`）行为逐字节不变，病理性地「展开值自身含 `%`」时保证终止
（上限用尽即返回剩余串）。既有 `TestExpandWindowsEnv` 覆盖不变。

#### `R7-5` [低 · 跨层复制 · 记录] 命令行引号两处

`internal/app/helpers.go` 的 `quoteWindowsArgument` 与 `wpf/worker.ps1` 的
`ConvertTo-CommandLineArgument` 是同一 CMD 引号算法（Go 与 PowerShell 各一份）。跨 Go/WPF 层无法
直接复用一个函数，两处各有单测；记录为应保持同步的漂移风险（改一边需在 `verify.ps1` 的
`CLI invalid combination` + `WPF argument quoting` 两处都验证），不阻塞。

### 10.4 验证矩阵（第七轮，落地后）

| 检查 | 结果 |
| --- | --- |
| `go build ./...` | BUILD_OK |
| `go vet ./...` | VET_OK |
| `go test ./...` | 全部包 ok（`api/app/audit/compare/download/install/inventory/model/pathutil/plan`） |
| `gofmt -l cmd/ internal/` | 干净（`export.go`/`view.go` 已 `gofmt -w` 后复查） |
| `go build -tags legacyps ./...` | BUILD_OK |
| `go vet -tags legacyps ./...` | VET_OK |

> 非页面型交付，无页面/视觉 smoke 需求。
---

## 11. 第八轮 — 未提交工作区复审（Round 8，2026）

> 对象：`main` 之上**未提交工作区**（Round 7 之后新增的改动），共 6 文件（+208/−53）：
> `go.mod`（1.24.4→1.27.0）、`internal/model/model.go`（`DriverAssessment` 嵌入 `Driver`）、
> `internal/app/app.go`（`resolveListOS`/`runSelection` 拆 `runExport`/`runInstallFlow`/`acquireSelection`）、
> `internal/app/evidence.go`（`enrichDeviceEvidence` → `buildDeviceEvidenceMap`+`projectMatchedEvidence`）、
> `internal/app/view.go`（`assessSelectedDrivers` 证据批处理 + `resolveDriverSourceAudit` 签名变更）、
> `internal/app/app_test.go`（2 组新单测）。方法与前七轮同构：独立通读差异 + 既有上下文 → 以热核技能
> 维度（抽象 / 简化 / 意大利面 / 边界 / 复制）新猎 → 实机 `go build/vet/test/gofmt` 复核。

### 11.1 结论速览

本轮改动**质量整体中上**：`app.go` 的 `runSelection` → `runExport`/`runInstallFlow`/`acquireSelection`
提取是干净、行为不变的简化（降低嵌套、职责被命名）；证据批处理（`buildDeviceEvidenceMap` +
`projectMatchedEvidence`）是**本轮最有价值的部分**——把「每驱动一次全 PnP 枚举 + setupapi 日志重读」
降为**每 pass 一次**，补齐了 `V6`→`S1` 家族里仅剩的「证据审计」N+1 缺口，且正确降级（枚举失败回
原始匹配行），两个新单测锁住了正确契约。

但有两处**阻塞级**的结构问题，按技能放行条曾须先修正；**本轮已一并落地**（行为不变，回归红线全绿）：

| ID | 严重度 | 主题 | 一句话 | 状态 |
| --- | --- | --- | --- | --- |
| `R8-1` | 高·边界/柔术 | `go.mod` 语言级与嵌入的关系 | 提升字段复合字面量确实需要 go1.27+；最终决定：锁定 **`go 1.27`（Latest stable，不带 patch）**为既定契约，测试字面量用提升缩写，后续不再为此回退 | 已定案（接受） |
| `R8-2` | 高·复制 | 匹配遍历在 `deviceVersionIndex` 与 `assessSelectedDrivers` 各写一遍 | 抽 `matchedByDriver` 单一遍历源，版本索引与证据审计均消费其结果，评估流对同一 set 只匹配一遍 | 已落地 |

`R8-2` 属「重复逻辑必须抽公共函数」，已柔术落地；`R8-1` 经评审与被裁决后**定案为接受的既定基线**
（`go 1.27`），不再作为阻塞项。落地细节见 §11.3，复验见 §11.4。

放行条中其余预设气味（>1k 文件、朴素 if 链、伪装抽象、恒等转发壳）**不成立**：最大文件
`app/view.go` 456 行、`app/app.go` 372 行，均远未到上限；`app.go` 的拆解整体是对轮次的干净推进，
无新意大利面。

### 11.2 规模事实（实测，本轮范围）

| 指标 | 数值 |
|------|------|
| 改动文件 / 行数 | 评审对象 6 文件（+208/−53）；落地后累计 8 文件（本轮新增落地改动 `install.go`/`audit_test.go`/`plan_test.go`/`go.mod`） |
| 最大文件 | `app/view.go` 456、`app/app.go` 372、`app/app_test.go` 446（均 < 500） |
| `go build / vet / test / gofmt` | 全绿（见 §11.5） |
| `go 1.27.0` 语言级 | `go test ./internal/audit|plan` 在 `-lang go1.24` 下**编译失败**——确认依赖提升字段复合字面量 |

### 11.3 发现清单（按严重度）

#### `R8-1` [高 · 边界 · 已定案（接受）] `go.mod` 语言级与嵌入的关系

`go.mod` 由 `go 1.24.4` → `go 1.27.0`。`model.go:43-48` 注释自辩称「嵌入的 assessment struct
在复合字面量里用提升字段需要 go1.2x+」，并以此为本轮 go.mod 升版背书。

**证据核查：该 go 版本提升只被测试文件需要，生产代码一次都不需要。**
- 生产侧唯一构造 `model.Driver` 的复合字面量在 `api/parse.go:148`，只用到本结构自有字段（`PartID`
  等），**从不**用到提升字段（`LocalVersion`/`CompareStatus`）。
- 用提升字段的复合字面量全在 `*_test.go`：`internal/audit/audit_test.go`（8 处）、`internal/plan/
  plan_test.go`（~8 处）、`internal/app/app_test.go`、`internal/install/install_test.go`、`internal/
  compare/compare_test.go`。这些 `&model.Driver{… LocalVersion: "x", CompareStatus: "Update"}` 是该
  语言版本提升触发的**唯一**原因——只有编译器报「use of promoted field … requires go1.27 or later」。

**最终裁决（被采纳）**：评审判定曾被给出（go1.27 语言级是单向门，仅为测试便利不值得，建议回
1.24.4）。经用户复核后该建议**未被采纳**；用户决定**锁定 `go 1.27`（Latest stable, 不带 patch）**——即
「本仓储建立在最新稳定语言级上」是接受的既定契约，不回退。落地取舍：
- `go.mod` 定为 `go 1.27`（不带 patch；Go 自动解析为当前最新 patch，1.28 发布时只在升级窗口跑一次
  `go get go@latest`，其余时间不动）；
- 测试字面量**保留提升字段缩写**（`LocalVersion:`/`CompareStatus:`/`SourceAudit:` 直接写），贴合嵌入
  设计、更简洁；
- `model.go` 的 `Driver` 注释按「既定契约」改述事实（见下段）：读取靠提升字段访问、复合字面量靠提升
  缩写，均需 go1.27+——这是刻意锁定、不回退的基础；
- 复验：`go build/vet/test ./...` 与 `gofmt -l` 在 `go 1.27` 下全绿（含 `-tags legacyps`），见 §11.4。

**（历史评审建议，未采纳）**：见上方「最终裁决」对应的回退方案——保留嵌入、测试改显式嵌套、go.mod 回 1.24。经用户复核未采纳，最终定案为锁 go 1.27。

**（同段历史落地草稿，未采用）**：
- 全部 16 处测试里用提升字段的复合字面量改显式嵌套：
  `internal/audit/audit_test.go`（7 处 `LocalVersion`）、`internal/plan/plan_test.go`
  （`LocalVersion`/`CompareStatus` 若干）、`internal/app/app_test.go`
  （`LocalVersion`/`CompareStatus`/`SourceAudit` 若干），形如
  `&model.Driver{DriverCode: "d1", OSID: "42", DriverAssessment: model.DriverAssessment{LocalVersion: "..."}}`；
- `go.mod` 回 `go 1.24.4`；
- `model.go` 的 `Driver` 注释改为陈述「嵌入使 encoding/json 扁平化、线契约不变 + 字段按类型分组」，
  删除「requires go1.2x+ / pins a recent stable language level」的自辩；
- 复验：`go build/vet/test ./...` 与 `gofmt -l` 在 `go 1.24.4` 下全绿（含 `-tags legacyps`），见 §11.4。

#### `R8-2` [高 · 柔术 · 已落地] 匹配遍历 duplicated：`deviceVersionIndex` 与 `assessSelectedDrivers`

`view.go` 现有两条几乎逐字相同的遍历：

- `deviceVersionIndex`（`view.go:140-167`）：
  ```go
  for _, driver := range selected {
      if driver == nil || !compare.TestDriverApplicable(driver, localDevices) { continue }
      for _, id := range pnpIDsOf(compare.GetMatchingLocalDevices(driver, localDevices)) {
          // dedup by PnpDeviceID
      }
  }
  ```
- `assessSelectedDrivers` 新增的 evidence 预备遍（`view.go:266-286`）：
  ```go
  for i, driver := range selected {
      if driver == nil || !compare.TestDriverApplicable(driver, localDevices) { continue }
      matched := compare.GetMatchingLocalDevices(driver, localDevices)
      matchedForDriver[i] = matched
      for _, device := range matched { /* dedup by PnpDeviceID */ }
  }
  ```

两者对**同一 selected set**执行同一「跳过不可用 → 取匹配设备 → 按 PnP ID 去重」遍历；而
`assessSelectedDrivers` 开头就调 `deviceVersionIndex`（它内部已完整遍历一遍），随后又自己遍历一遍去拼
`evidenceUnion`。既**复制了逻辑**，又在评估流里**对同一各匹配集跑了两次匹配**。

**柔化动作（行为不变）**：让 `deviceVersionIndex` 消费 `assessSelectedDrivers` 已算出的
`matchedForDriver`（每驱动匹配集合）+ 去重 PnP ID 的单一来源，而不是自带一遍遍历。改法（任选其一、
推荐 1）：
1. 抽单一 helper `matchedDevicesByDriver(selected, localDevices) ([][]model.Device, []model.Device)`
   ——返回每驱动匹配表 + 去重 union；`deviceVersionIndex` 改为接收该表（或 union 的 PnP id 列表），
   `assessSelectedDrivers` 用同一表，阅读即知两处同源。
2. 或让 `assessSelectedDrivers` 先拼 `matchedForDriver`，再把「全部匹配 PnP id」传给
   `deviceVersionIndex`（改其签名为接收 ids 或 matched table）。

要点是**踪迹去重/union 只出现一次**，消除「复制匹配逻辑 + 重复匹配」这一对硬伤，仍是行为不变的纯
结构简化。

**落地（已落地，行为不变）**：
- `view.go` 新增单一遍历源 `matchedByDriver(selected, localDevices) (perDriver [][]model.Device,
  union []model.Device)`——承担「跳不可用 → `GetMatchingLocalDevices` → 按 PnP ID 去重（空 ID 过滤）
  → 拼 union」的全部逻辑；
- `deviceVersionIndex` 签名简化为 `(ctx, devices []model.Device)`，内部只做 `pnpIDsOf + 单次
  GetDeviceDriverVersions + 建索引`，不再自带遍历；
- `assessSelectedDrivers` 先 `matchedForDriver, union := matchedByDriver(...)` 一次拿到每驱动匹配表与
  union，版本索引与证据审计共用同一结果，对同一 selected set 只遍历一遍；
- `install.go` 的 post-install 验证流同步改走 `matchedByDriver` 取 union 后喂 `deviceVersionIndex`；
- 语义核对：旧代码 union 会把空 PnP ID 设备去重成一条，但 `GetDeviceEvidence`/投影均按 PnpDeviceID
  查表、空 ID 永不命中，故新 helper 跳过空 ID 不改变任何输出——**行为一致**；复验全绿（§11.4）。

#### 更低危观察（不阻塞）

- `DriverAssessment` 嵌入是 `V4`「transport 与 assessment 彻底分离」的**中间态**——它只做了字段归组，
  没真分离（`Driver` 仍是三人一体的扁 JSON blob，嵌入使 plan/export 线不变）。这是接受的方向，只要
  `R8-1` 解决；但要如 §7 记下：`Driver` 真正的 transport/assess 分离仍待后续。
- 证据批改（`buildDeviceEvidenceMap` + `projectMatchedEvidence`）本身行为保持且是把图像正向工程：
  - union 去重按 PnP ID 安全（同一设备的 PnP 属性在各驱动映射内等价）；每驱动只需投影其匹配行 → 与
    旧「每驱动枚举」逐行一致；
  - `localVersion == ""` 驱动的 `continue` 前置意味着若无可用驱动带本地版本，证据永不 build，此时
    `projectMatchedEvidence(nil)` 回原始匹配行，等价于旧 per-driver 降级——**行为一致**；
  - setupapi 日志现在只读一遍（`loadDriverImportRecords` 在 union 内一次）vs 旧每驱动一遍——净益。
  无问题。
- `app.go` 三种 `switch` 拆 `acquireSelection`（返回 `([]*model.Driver, int)`）把「PS1 退出码契约
  （3=GUI 码不匹配）」命名的单测 `TestAcquireSelectionPreservesExitCodeContract` 与 `app_test` 的
  `TestSelectByCodesRejectsPartialAndNotApplicable` 一起收敛得很好。无问题。

### 11.4 验证矩阵（Round 8，落地后）

> 最终状态（`R8-1` 定为 `go 1.27` 既定契约、`R8-2` 落地）实机复核：全绿。

| 检查 | 结果 |
| --- | --- |
| `go build ./...` | BUILD_OK |
| `go vet ./...` | VET_OK |
| `go test ./...` | 全部包 ok（`api/app/audit/compare/download/install/inventory/model/pathutil/plan`） |
| `gofmt -l cmd/ internal/` | 干净 |
| `go build -tags legacyps ./...` | BUILD_OK |
| `go vet -tags legacyps ./...` | VET_OK |
| `go.mod` 语言级 | `go 1.27`（Latest stable, 不带 patch；全树在此语言级编译/测试，利用并依赖 go1.27+ 提升字段复合字面量） |

> 落地前的基线证据：`go.mod` 原为 `go 1.27.0` 时，仅在 `go test ./internal/audit ./internal/plan`
> （`-lang go1.24` 编译级）报「requires go1.27 or later」，生产 `go build` 不受影响——即 `R8-1` 的空洞。

> 非页面型交付：无视觉/页面 smoke 需求。

### 11.5 放行判定

按热核放行条，`R8-1` 与 `R8-2` 评审时为**预设阻塞项**。裁决结果：
- `R8-2` 已落地（新增 `matchedByDriver` 单一遍历源消除双份匹配与双遍遍历，行为不变）；
- `R8-1` 经用户复核**定为接受的既定契约**：go.mod 锁 `go 1.27`（Latest stable, 不带 patch），测试字面量保留提升字段缩写，`Driver` 注释陈述「依赖 go1.27+ 提升字段」为不回退基础。

最终状态 §11.4 全绿（含 `-tags legacyps`）。**本轮放行。** 遗留不阻塞方向见下。