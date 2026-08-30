# 热核代码质量评审 — 最终报告

- 评审日期：2026-08-30（首轮）＋ 后续会话（第二轮独立全量复审与 #4/#5/#6/#7 落地）
- 评审对象：`lenovo-driver-installer` Go 引擎（`cmd/` + `internal/` 全部源码）+ WPF 层
- 评审方式：热核可维护性评审（极端严格的抽象 / 大文件 / 意大利面条分支审查）
- 报告性质：**汇总终稿**，整合首轮审查发现、随后的 3 项修复（#1–#3），以及第二轮的独立全量复审与本轮落地的 #4/#5/#6/#7
- 状态：首轮及第二轮全部审查项（#1–#7）均已落地并验证；`go build / vet / test / gofmt` 全部通过，`scripts/verify.ps1` 全程 `VERIFY_OK`

> 本报告取代 `docs/` 下此前各轮次的中间审查文档（Round 1/2/3、go、go-zh、current、followup），
> 为唯一权威的审查结论。

---

## 1. 结论速览

这是一个**分层清晰、结构健康的代码库**——不是处于危机中的烂摊子。包划分
（`api` / `compare` / `audit` / `install` / `inventory` / `plan` / `model`）方向正确，
领域规则表（厂商规则、驱动匹配规则、软件规则、setupapi 日志规则）是真正的"代码柔术"——
用表替代条件链。**没有任何文件接近 1000 行**（最大为 `matching.go`，366 行），因此
技能所预设的两个阻塞性气味（文件超 1000 行、朴素意大利面）**基本不成立**——我不臆造。

真正的问题更隐蔽：**契约不一致、一个 god-function、以及非原子的编排**。这三个已在本轮
修复。

审查按技能的严重度阶梯输出 **8 项发现**，其中前 3 项（#1-#3）已实施修复，
其余（#4-#8）为明确、行为可保留的后续优化。

---

## 2. 审查范围与证据

- **范围**：整个 `cmd/` + `internal/` Go 树（每份源码均已通读），以及 `wpf/` 层。
- **方法**：逐文件通读 + 结构化定位；对所有权证均在源码上核实，非"查过再说"。
- **验证**：`go build ./...`、`go vet ./...`、`go test ./...`、`gofmt -l`。

| 指标 | 数值 |
|------|------|
| 最大文件行数 | `matching.go` 约 366 行 |
| 全部源码 gofmt | 干净 |
| go build ./... | OK |
| go vet ./... | OK |
| go test ./... | 全部包通过 |

---

## 3. 发现清单（按严重度排序）

> 以下为评审结论。标注 **[已修复]** 的是首轮（#1–#3）或第二轮复审（#4/#5/#7）已实施并
> 验证的项；标注 **[待办]** 的是有明确路线但本报告撰写时尚未落地的后续项。

### #1 [已修复] `InstallDriverFile` 的成/败契约不一致 → `InstallSelected` 用令人困惑的三路分支兜底

`internal/install/install.go` 依据扩展名返回 `(int, error)`，却有两种约定：
- `.msi` / `.inf` / default → 失败时 `(非零码, nil)`（nil error + 非零码）
- `.exe` / `.cab` → 失败时 `(code, fmt.Errorf(...))`（非 nil error）

于是 `InstallSelected`（`app/install.go`）不得不同时分支判断 `err` 与 `code`，
读者无法不经推演各扩展名的返回形状就理解分支流向。更进一步，`.exe` 先跑**内部**提取
兜底（`installEXE` → `finishEXEFallback` → `ExtractedDriverFallback`），随后又在应用
层跑**第二层**交互式兜底（`tryInteractiveExeFallback`）——同一失败在两处被重复恢复，
且被糟糕的契约隔开。

**修复**：统一契约为（见 `install.go` 的文档注释）——
- `err == nil && code == 0`：干净成功；
- `err == nil && code != 0`：可执行程序**已运行**并返回非零退出码（可交互重试）；
- `err != nil`：无可用退出码的**终结性失败**（nil driver / 超时 / 解压错误 / 空 INF 包）。

`InstallSelected` 改为显式 `switch`，按**扩展名**而非偶然的 `err != nil` 来决定是否
交互式重试 `.exe`，并保留 msi/inf/other 的 "exit code N" 提示语义。为各包类型新增了
契约定死的单测。

### #2 [已修复] `CompareOSDriverView`（`app/view.go`）是一个约 95 行的 god-function，混五职

单函数负责：抓取驱动列表、合并跨 OS 列表、过滤、最新选择、逐驱动循环（可适用 → 本机
状态 → 状态 → 来源审计 → 来源标签，就地改 `driver.*`）、写 plan 文件、打印表格与
汇总、构造 `applicable` / `updates`。即业务比较、I/O 与展示全粘在一起，且与 `App`
（缓存+日志）纠缠——这是"下一个功能只会再往里面塞一块"的典型。

**修复**：`CompareOSDriverView` 收敛为薄编排（fetch → merge → select → 评估 →
持久化/打印 → 分区 → return），把逐驱动比较循环抽成 `assessSelectedDrivers`（并
用早返回替代嵌套 `if`），把分区抽成纯函数 `partitionViewDrivers`。

### #3 [已修复] 三处"加载驱动列表并容忍失败"的包裹几乎相同；`loadDriverList` 是规范的，但两个调用点各卷一份

`app/view.go`：`loadDriverList`（容忍+WARN）、`CompareOSDriverView`（主视图，重新内联
err/empty 包裹）、`initAlternateSourceMap` 各自 roll 一遍"抓取+容忍策略"。容忍策略
（"什么算不可用"以及如何记录）出现三份，若漂移会导致合并与主视图对"好"的定义不一致。

**修复**：新增 `mustDriverList` 作为严格版（主视图必须成功，空/失败为硬错），并使用到
主视图，删除其内联重写；`initAlternateSourceMap` 与跨域合并继续复用 `loadDriverList`，
且新增 `mustDriverList` 单测。

### #4 [已修复] 可分离却串行的网络编排 — 独立读被串行化

`LatestAcrossOS` 合并、`initAlternateSourceMap`、`refreshDriverURL` 等都是逐个串行抓取
的循环。这些 OS 读均**独立**，串行只因把编排写成平铺 for。

**修复（本轮）**：新增 `loadDriverListsForOSIDs`/`driverListLoad`，以零依赖 `sync.WaitGroup`
并行抓取一组独立 OS 列表，同时按**输入 OSID 顺序**返回——合并、备用源表与 URL 刷新的确定
性输出顺序（含「首个命中」语义）保持不变。`App.osDriverCache` 加 `sync.Mutex` 守卫，
并发抓取无竞态。安装仍保持串行（`reboot` 语义下不并行）；`assessSelectedDrivers` 逐驱动
循环亦维持串行（惰性共享 `alternateSourceMap` + 交互输出确定性）。新增
`TestLoadDriverListsForOSIDsPreservesOrder`、`TestToleratedDriverListMissPolicy`。

### #5 [已修复] 来源审计优先级以 `setCategory` 闭包与命令式 if 链实现

`internal/audit/audit.go` 的 `ResolveDriverSourceEvidence` 用"只设一次"闭包接长顺序
`if` 链（history → 每设备循环 → 当前 OS 表 → 备用 OS）把优先级编码在控制流里。

**修复（本轮）**：引入 `sourceCategoryRule` 表 + `sourceEvidenceRules`，按文档顺序组装
（history → 设备 → 当前 OS → 备用 OS），优先级成为**表的顺序**而非条件链；证据行收集
（`driverEvidenceLines`）与类别判定解耦。新增 4 个单测锁定优先级（history 压倒设备、
offline 图片、设备压倒当前 OS、无命中回落 Unknown）。

### #6 [已修复] `Driver` 身兼三职：DTO + 领域对象 + 可变每跑 scratch

`Driver` 同时是（a）wire 格式（含全部 JSON 标签）、（b）领域对象、（c）被比较循环原地
改写的 scratch——`CompareOSDriverView` 拿到的 `viewDrivers` 元素与 `osDriverCache`/API 返回
的 transport 对象**同一身份**，评估一旦净改写就会污染共享 DTO。

**修复（本轮）**：把比较视图改为持有**自有副本**——新增 `cloneDriver`/`cloneDrivers`，
在过滤/选择/评估前把合并后的行深拷贝为视图私有记录；`assessSelectedDrivers` 只写这些
拥有权的副本，**共享缓存与 API transport 行保持只读**。行为不变（plan/表格/导出/安装
仍读取视图私有行），仅所有权边界被划清。新增 `TestCloneDriversIsOwnedCopy`（验证对副本的
改写不会泄漏回源行，含嵌套 `SourceAudit` 深拷贝）。

### #7 [已修复] `runProcess` 的 `captureOutput bool` 与合并 stdout/stderr 是"假接口"

`ProcessResult.Stdout` 与 `.Stderr` 永远同字节或空，加一个 mode 布尔，掩盖了并不存在的
不变量，且**生产代码从未读取**这两个字段。

**修复（本轮）**：直接**删除**整个捕获机制（优于"合并为单一 `Output`"的折中）——删掉
`ProcessResult.Stdout/Stderr`、`captureOutput` 参数与输出缓冲，`runProcess` 一律
`io.Discard`。更新 `TestRunProcessTimeoutKillsTree` 的签名调用。

### 边界/规范观察（不阻塞）

- **无浮点、无 1000 行文件、enum 良好**：`big.Rat` 版 `ConvertToBytes`（尺寸避免浮点）、
  `CompareStatus` 字符串枚举、`Driver.Disabled()` 封装 `"0"` 魔法。符合 AGENTS.md。
- **时间戳**：`formatTimestamp` 用 `UTC()` 但格式为 `2006-01-02 15:04:05`（非 RFC3339 毫秒）。
  低优先——多为面向用户的纯文本日志，但"核心日志必须是 RFC3339"的规范算轻微触发。
- **测试覆盖**：强（`api`/`app`/`audit`/`compare`/`install`/`inventory`/`plan` 均有 `_test.go`）。
  #2 抽出的纯核心正好能让更多比较逻辑落入这些测试。

---

## 3. 已实施的修复（本会话）

### 3.1 统一 `InstallDriverFile` 契约（`internal/install/install.go`、`internal/app/install.go`）

代码变更：
- 在 `InstallDriverFile` 增加统一契约文档字符串；`installEXE`/`finishEXEFallback`
  归一化为：提取的兜底成功 → `(0,nil)`；可获得该退出码 → `(code,nil)`；无退出码且兜底无效
  （超时）→ `(code, err)`。
- `InstallSelected` 改为显式 `switch` 分支：

```go
code, installErr := install.InstallDriverFile(outFile, driver, dlDir)
switch {
case code == 0 && installErr == nil: // success
case installErr != nil:             // terminal, no usable exit code
case strings.EqualFold(filepath.Ext(outFile), ".exe"): // silent EXE ran non-zero -> interactive
default:                            // non-EXE concrete exit code -> "exit N"
}
```

行为保留：msi/install/other 非零、"exit N" 消息、`安装退出码 N` 与历史 `exit=N` 不变；
EXE 常见干净失败仍走交互式重试；仅"EXE 超时且兜底无效"从"尝试交互式"改为"直接失败"
（无可用退出码，属合理契约）。

新增/更新单测：`TestInstallEXESurfacesCleanExitWhenFallbackUnused`、`TestInstallEXETimeoutWithoutFallbackIsHardError`。

### 3.2 抽取 `CompareOSDriverView` 比较核心（`internal/app/view.go`）

- `assessSelectedDrivers(...)`：每驱动 可适用 → 本地 → 状态 → 来源审计 → 标签，早返回。
- `partitionViewDrivers(...)`：`applicable`/`updates` 分区。
- `CompareOSDriverView`：fetch → merge → select → assess → 持久化/打印 → 分区 → return。

行为：与之前一致（日志、plan 文件、表格、返回结构相同）。

### 3.3 规范驱动抓取（`internal/app/view.go`）

新增 `mustDriverList`（严格版，主视图必需），替换 `CompareOSDriverView` 内联 err/empty
包裹。`loadDriverList` 继续是容忍/软 miss 的规范。新增
`TestMustDriverListHardErrorsOnEmpty`、`TestMustDriverListReturnsCachedRows`。

### 3.4 第二轮：独立全量复审 + 落地 #4/#5/#7

第二轮对本仓库 `cmd/` + `internal/` 全部 Go 源码 + WPF 层做了一次**独立**的全量复审
（不依赖首轮结论），再落地三项后续项：

- **#4 并行编排**（`internal/app/view.go`、`internal/app/install.go`、`internal/app/app.go`）：
  新增 `loadDriverListsForOSIDs`/`driverListLoad`（零依赖 `sync.WaitGroup`），并行抓取独立
  OS 列表并按输入顺序返回；`App.osDriverCache` 加 `sync.Mutex` 守卫无竞态。被合并的
  `CompareOSDriverView` 跨 OS 循环、`initAlternateSourceMap`、`refreshDriverURL` 均改并行，
  确定性输出顺序与「首个命中」语义不变；安装与逐驱动评估仍串行。
- **#5 来源审计规则表**（`internal/audit/audit.go`）：`sourceCategoryRule` 表 +
  `sourceEvidenceRules`，把优先级从命令式 if 链改为表的顺序；证据行与类别判定解耦。
  新增 4 个优先级单测。
- **#7 删除伪捕获机制**（`internal/install/install.go`）：直接删掉
  `ProcessResult.Stdout/Stderr`、`captureOutput` 与输出缓冲——优于原建议的「合并为单一
  Output」。更新 `TestRunProcessTimeoutKillsTree`。
- **#6 评估所有权分离**（`internal/app/view.go`）：`cloneDriver`/`cloneDrivers` 把合并行
  深拷贝为视图私有记录，`assessSelectedDrivers` 只写拥有权副本，共享缓存/API 行保持只读。

新增单测：`TestSourceEvidenceHistoryOutranksDevice`、`TestSourceEvidenceOfflineImageAvailable`、
`TestSourceEvidenceDeviceOutranksCurrentOS`、`TestSourceEvidenceNoMatchFallsBackToUnknown`
（audit）；`TestLoadDriverListsForOSIDsPreservesOrder`、`TestToleratedDriverListMissPolicy`、
`TestCloneDriversIsOwnedCopy`（app）；`internal/install` 原有用例按新签名更新。

---

## 4. 验证（全部通过）

| 检查 | 结果 |
|------|------|
| `go build ./...` | BUILD_OK（exit 0） |
| `go vet ./...` | VET_OK（exit 0） |
| `gofmt -l internal/ cmd/` | 无文件（干净） |
| `go test ./...` | 全部包 ok（`api/app/audit/compare/download/install/inventory/pathutil/plan`），含新增单测 |

未被阻止（非页面型交付）：无视觉/页面 smoke 需求。

---

## 5. 后续建议（按优先级）

首轮的后续清单 #4/#5/#6/#7 **全部在本报告落地**：

- **#4 并行编排**——`loadDriverListsForOSIDs` 并行抓取独立 OS 列表，确定性顺序保留。
- **#5 来源审计规则表**——`sourceCategoryRule`/`sourceEvidenceRules` 声明式优先级表。
- **#6 评估所有权分离**——`cloneDriver`/`cloneDrivers` 使比较评估只改写视图私有副本，
  共享缓存/API transport 行保持只读。
- **#7 删除伪捕获**——移除 `ProcessResult.Stdout/Stderr` 与 `captureOutput`。

可选的后续方向（非阻塞）：把评估结果进一步建模为独立 `assess` 值并让 `DriverView` 携带
`driverEntry{Driver, Assess}`，可让 `Driver` 彻底回归纯 transport——但这超出本轮目标，
且当前实现（owned 副本 + 收敛的单点 `assessSelectedDrivers`）已消除共享 DTO 污染。

---

## 6. 交付说明

本报告取代 `docs/` 下全部旧审查文档（旧轮次 round1/2/3、go/go-zh/current/followup），
因此经用户确认后这些文件已被删除。`WORKLOG.md`、`TECHNICAL.md` 为工程文档，予以保留。

> 最终交付物：本报告（含首轮 #1–#3 与第二轮 #4/#5/#6/#7 的修复）+ 源码与单测 + 全程
> `go build/vet/test/gofmt` 通过 + `scripts/verify.ps1` 14 步全 `VERIFY_OK`。