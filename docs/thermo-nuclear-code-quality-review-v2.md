# 热核代码质量评审 — 第二轮（独立新鲜全量复审）

- 评审日期：2026-08-30（在本会话对 `main` 当前工作树做的一次**独立**全量复审）
- 评审对象：`lenovo-driver-installer` Go 引擎（`cmd/` + `internal/` 全部源码）+ WPF 层
- 评审方式：热核可维护性评审（极端严格的抽象 / 大文件 / 意大利面条分支审查）
- 前置状态：首轮 `#1–#7` 已落地（前一轮交付，见
  `docs/thermo-nuclear-code-quality-review-final.md`）。本文档为在此基础上的一次**全新、
  不依赖首轮结论**的复审，只报告**新发现**或**未关闭/被新改动复燃**的问题；不复述已修项，
  除非发现其契约出现回归。
- 本会话实测验证：`go build ./...`=BUILD_OK、`go vet ./...`=VET_OK、`go test ./...` 全
  包 ok、`gofmt -l cmd/ internal/` 空（干净）。当前 `main` 树健康、已修项保持有效。

> **🌋 本章跟进（评审后的实施落地记录）**：用户在评审后选择「一并处理」。本文档第 3 节各
> 发现的**实施状态**已更新为 `[已落地]` / `[受限落地]` / `[延后]`，第 6 节给出每个改动的
> 文件、行为保证与新增单测，并在第 7 节给出**全量 `go build/vet/test/gofmt` 复验结果**。
> 只有 V6（幂等批次化 PowerShell 版本查询）与 V4（Driver 三职模型彻底拆分）因触及
> 无测试兜底的运行语义/JSON 线契约而被**谨慎延后**并说明原因；其余全部落地且行为不变。

---

## 1. 结论速览

代码库**整体健康**：包边界清晰（`api/audit/compare/install/inventory/plan/model/download/
pathutil`），领域规则走声明式表（`vendorRules`、`driverMatchRules`、`softwareRules`、
`sourceEvidenceRules`、`fieldExtractRules`）而非条件链——这是对的。**没有任何文件接近
1000 行**（最大 `app/view.go` 466 行、`audit/audit.go` 405 行、`compare/matching.go`
366 行），因此技能预设的两个阻塞性气味（文件超 1k、朴素意大利面）**不成立**；第二个阻塞
气味（新改动把已有流程弄成意大利面）需逐一核查——见下方发现。

本轮最终结论：**未达到放行条**。存在两个高置信度问题——V1 是「启动失败无错误、被
`InstallDriverFile` 契约『错当』成真实退出码」的契约回归（直接牵连首轮 #1 的契约统一）；
V2 是「同一段『收集非当前 OSID』编排在三个文件各写一遍」的可柔术删除复制。其余为中低危
的结构/边界/编排观察。

---

## 2. 复审方法（独立、非复述）

- 重新通读 `cmd/` + `internal/` 全部 Go 源码（逐文件），WPF 层与 `scripts/verify.ps1` 通读。
- 关键事实全程用 `grep`/包含行号佐证，非"查过再说"。
- 本会话实机运行 `go build/vet/test/gofmt` 以锁定当前树的真实状态（结果见上）。

**规模事实（实测）**：

| 指标 | 数值 |
|------|------|
| 最大文件 | `app/view.go` 466 行 |
| 其余大文件 | `audit/audit.go` 405、`compare/matching.go` 366、`app/app.go` 352、`app/install.go` 309 |
| go build / vet / test / gofmt | OK / OK / 全包 ok / 干净 |

> 刻意不臆造 1k 行或朴素 if 链这两条阻塞项——文献级证据表明它们不存在。

---

## 3. 发现清单（按严重度排序）

> 以下为本轮**新**发现。标注 `[已提议 / 未落地]` 的项建议在下一提交落地；本评审交付
> 的是**审查结论**（含明确修复路线），不擅自改码。

### V1 [高] [已落地] `runProcess` 启动失败被压成 `-2` 且不带错误——直接翻开 #1 的统一契约

- 位置：`internal/install/install.go:57-59`
  ```go
  if err := cmd.Start(); err != nil {
      return ProcessResult{ExitCode: -2}
  }
  ```
- 问题：当**可执行文件根本起不来**（缺失、被占用、无权限、路径错、`.exe` 不在）时，
  `cmd.Start()` 返回非 nil 错误，这里却丢弃错误并返回 `ExitCode:-2, TimedOut:false`。
  于是 `InstallDriverFile` 走 `.msi`/`default` 分支时，把"启动失败"**当成**"安装器
  正常运行并返回退出码 -2"，`InstallSelected` 打印 `Install exit code -2`（install.go:92）。
- 这与首轮 #1 明确定义的契约直接冲突：#1 说 *`err != nil` = 无可用退出码的终结性失败*，
  而"启动失败"正属于"无可用退出码"一类，却被压成 `err==nil && code!=0`（"可执行程序已
  运行并返回非零"）。**`#1` 的契约统一被这个遗留返回点复开了一个洞。** 实际效果是用户看到
  "exit code -2"，而真实原因（文件缺失/拒绝访问）从未出现在任何错误面上。
- 柔术：给 `ProcessResult` 加 `StartErr error`（或 `runProcess` 返回 `(ProcessResult, error)`），
  把启动失败归一为 `(code, err)` 终结性失败；`processExitCode` 只对真正的 `exec.ExitError`
  归一，其余一律带错。这样`.msi`/`default` 才能走回 `err != nil` 分支。
- 证据链：`install.go:57-59`、`install.go:146-151`（default 仅测 TimedOut 即 `return code,nil`，
  掉了启动错误）。

### V2 [高，代码柔术] [已落地] "收集除当前 OS 外全部 OSID" 编排三处复制

`app` 包内三处以几乎相同的循环"遍历 `OsList`、跳过当前 OSID、收拢其余 OSID"：

- `internal/app/view.go:160-167`（`CompareOSDriverView` 的 `LatestAcrossOS` 合并）
- `internal/app/view.go:342-347`（`initAlternateSourceMap`）
- `internal/app/install.go:228-234`（`refreshDriverURL`）

三处对"哪些 OS 属于'当前'、哪些是'备选'"的定义各自内联重写一遍。若一处对
"`sysID` 是否算当前"的口径漂移，合并、来源审计、URL 刷新三者对"备选 OS"的理解就会不一致
——这正是该抽成规范化 helper、收敛为一处的典型复制。
- 柔化：抽一个纯函数 `otherOSIDs(osList []model.OSListEntry, excludeID string) []string`
  放在 `app`（或 `compare`），三处调用同一实现；顺带消掉 `ExportGUIView` 里
  `view.go:419-425` 的另一处 OSID→名称查表类似的重复扫描（这两、三处可各自复用一个小
  的 index 映射）。

### V3 [高/中] [已落地] `formatTimestamp` 仍是"UTC 但非 RFC3339"

`internal/app/helpers.go:19-21`
```go
func formatTimestamp(t time.Time) string {
    return t.UTC().Format("2006-01-02 15:04:05")
}
```
`AGENTS.md`（时间统一边界）要求核心日志/DB/接口强制 **UTC+0 且严格 RFC3339（含毫秒/微秒）**，
且禁止自定义字符串。当前格式无 `Z` 后缀、无小数秒，且历史 CSV、plan "Generated"、日志行
全部沿用此格式。首轮把它只标为低优先边界观察，**实际未关闭**。本复审升为要落地项：
改为 `t.UTC().Format(time.RFC3339)`（或 RFC3339Nano），并同步 `history`/`plan`/`Log`
读取端对该格式的解析一致性确认。

### V4 [中/高] [受限落地] `Driver` 仍是三职（transport + 领域 + 每跑 scratch），`cloneDrivers` 是"贴创可贴"

首轮 :#6 通过 `cloneDrivers` 让评估只写视图私有副本，**共享 DTO 不再被污染**——
这一步是对的。但 `Driver` 类型**本身**仍同时是：(a) API/wire 对像（全部 JSON 标签）、
(b) 领域值、且 (c) 承载 `LocalVersion/LocalVendor/CompareStatus/CompareSource/SourceAudit`
这些**派生评估结果**的可变字段。每个下行消费（plan.go、`ExportGUIView`、
`PromptSelection`/interactive）仍一律从同一个 `Driver` 上读"评估结果"，于是 `cloneDrivers`
不得不在每个入口处重复"先深拷贝以免污染共享行"。这是**给症状打补丁**，未把模型做干净。
- 首轮报告把"让 `DriverView` 携带 `driverEntry{Driver, Assess}`"列为可选遗留方向；本轮
  复审维持：这是唯一能真正删除 `clone` 散点（`view.go:36-55` 缓存、`api.SourceDrivers`
  行只读约束）的结构性柔化。若不重构，至少应把 `cloneDrivers` 收敛成一个唯一入口并在
  `DriverView` 上做，而不要在未来的新读点上再新增 `clone`。
- 已评估：当前行为正确、测试锁定（`TestCloneDriversIsOwnedCopy`）；本项是**结构倾向**，
  不做也会因为继续追加 Reader 而让补丁面扩大。

### V5 [中] [已落地] `runProcess` 超时路径吞掉 `killTree` 错误并"射后即望"协程

`internal/install/install.go:66-71`
```go
case <-ctx.Done():
    _ = killTree(cmd.Process.Pid)   // 失败被无视（AGENTS 显式化/通知约定）
    select {
    case <-done:
    case <-time.After(5 * time.Second):   // 等待子进程退出；上限 5s
    }
    return ProcessResult{ExitCode: -1, TimedOut: true}
```
- `killTree` 失败（taskkill 不可用/权限异常）被 `_ =` 静默吞掉；`cmd.Wait()` 的协程若在
  `killTree` 未播完时退出，第二个 select 等到 5s 后照常返回，等待中的协程可能尚未被回收。
  建议：让 `killTree` 失败在 `TimedOut` 结果上至少捎带错误（或记入日志）；对 `done` 通道的
  等待可改为固定的小上限且不阻塞唯一失败面。此为操作边界的中低危观察。

### V6 [中] [延后] `localDriverState` 单驱动一次的 PowerShell 子进程风暴

`internal/app/view.go:128-135` 里 `assessSelectedDrivers`（view.go:222-253）对**每个
选中驱动**都调 `inventory.GetDeviceDriverVersions(ctx, pnpIDs(...))`，即每次运行对 N 个
驱动发起 **N 次 powershell.exe 子进程**（每个子进程再配前备 CIM）。同一台机器的 PnP
版本信息是**可一次聚合查询**的；把"获取版本"提升为对整个 `matchedDevices` 并集的一次
版本信息是**可一次聚合查询**的；把"获取版本"提升为对整个 `matchedDevices` 并集的一次
查询、再按 PnP 归并，可将子进程从 N 降到 1。行为/厂商匹配规则不变（仍按每驱动在归并结果
里查自己的设备 id）。此为编排/健康的中危观察，不阻塞行为；排期时可先做，不必为本轮目的
强制上线。

### V7 [中] [已落地] `selectByCodes` 双重 O(N·M) 扫描
`internal/app/helpers.go:97-131`：第一遍只滤出 `codeSet` 命中的驱动（O(N)），随后又为
每个 `codeSet` 里的 code 内层再扫全驱动以判定 Missing（O(K·N)）。用一张
`code→driver` 映射一次扫完即可同时得到 Selected/NotApplicable/Missing。轻微，但属
"已有更直接的 canonical 路径"的众点对象。

### V8 [低/中] [已落地] `CompareOSDriverView` 仍是"决策+副作用"混合门（#2 已拆内核，输出仍在）

首轮 #2 已把逐驱动比较抽成 `assessSelectedDrivers`、分区抽成 `partitionViewDrivers`。
但 `CompareOSDriverView`（view.go:152-216）本身仍把 持久化plan 文件（`:193`）、
`fmt.Fprintln` 表格输出（:198-205）、`a.Log` 与"比较/返回结构"混在一 64 行函数内。
建议把"构建 `DriverView`"（纯决策）与"写 plan + 打印"（副作用）分成两个类型的方法，
主 `Run` 里先 `buildView` 再 `present(view)`。此为整体枢纽处尚未删除的编排复杂性；
若能拆，`Run/gui-export` 等只关心 `DriverView` 的口径将不再依赖 stdout/plan 的副作用。
非阻塞但为可保留优化。

---

## 4. 边界/规范观察（不阻塞，回归确认）

- **无浮点**：`ConvertToBytes` 用 `big.Rat` → `big.Int.Quo`（截断），尺寸避免浮点；符合约定。
- **enum**：`CompareStatus`/`AuditCategory` 字符串枚举；`Driver.Disabled()` 封装 `"0"`，明确。
- **测试覆盖**：强（`api/app/audit/compare/download/install/inventory/pathutil/plan` 均有
  `_test.go`；`app` 含 `loadDriverListsForOSIDs` 顺序、`clone` 属权、宽容 miss 等）。`model` 与
  `cmd` 无测试文件（纯 DTO/入口，可接受）。
- **`inventory.GetDeviceEvidence`/`windows.go` 的 PowerShell 字符串**仍为不可静态校验的内嵌
  script；`GetLocalDeviceSnapshot` 空输出 `runJSON` 返回 nil（`windows.go:48-50`）——若
  设备查询静默变空，来源审计会被判"无证据"。维持首轮的**低**评，不新增。

---

## 5. 放行条

按热核技能，可按的放行条件是：无清晰结构回归、无可见的"让实现更简单"被丢弃、无
无端文件翻倍、无意大利面新增、无 hacky/伪装抽象、无重复规范 helper、无错层泄漏。
对应本轮：

- **阻塞**：V1（契约回归：启动失败却走"退出码"语义——直接推翻 #1）——必须修。
- **强建议（柔术）**：V2（三份 OSID 编排复制）——一抽即删两份，代价低。
- **应落地**：V3（RFC3339）——直接违反 `AGENTS.md`，首轮遗留未关。
- 其余 V4/V5/V7/V8 为明确的后续方向，不阻塞行为，但写进报告便于排期。

> 用户已选「一并处理」。落地结果见第 6、7 节。V1/V2/V3/V5/V7/V8 已实现且行为不变、
> 配回归单测；V4 受限落地（所有权边界已收敛为唯一入口点）；V6 因需改无测试兜底的
> 运行语义而**延后**（详见 6.4/6.6）。

---

## 6. 本章实施记录

> 改动均为**行为不变**（除 V3 时间戳线格式按 `AGENTS.md` 从 `"2006-01-02 15:04:05"`
> 改为 RFC3339，属既定变更）。全量 `go build / vet / test / gofmt` 复验见第 7 节。

### 6.1 V1 — 启动失败不再被压成"退出码 -2"
- `internal/install/install.go`：`ProcessResult` 新增 `StartErr error`；`runProcess` 的
  `cmd.Start()` 失败时 `StartErr` 落错，`.`msi/.inf/.cab/default/installINFPaths/RunPnPUtilWithTimeout`
  均在 `StartErr != nil` 时 `return (code, err)`（终结性失败），`RunPnPUtilWithTimeout` 也不再
  把启动失败改为 exit 1。补 `TestInstallDriverFileStartFailureIsTerminal`、
  `TestRunProcessStartFailureCarriesError`。
- 行为：此前"启动失败→ `Install exit code -2`"的误导路径消除；`InstallSelected` 的既有
  `installErr != nil` 分支正确接手，用户能看到真实错误。

### 6.2 V2 — `otherOSIDs` 收敛三处复制
- `internal/app/view.go`：新增纯函数 `otherOSIDs(osList, excludeID) []string`；`CompareOSDriverView`
  的 `LatestAcrossOS` 合并、`initAlternateSourceMap`、`install.go` 的 `refreshDriverURL` 三处改调它。
- 新增 `TestOtherOSIDsExcludesCurrentAndPreservesOrder`。

### 6.3 V3 — 时间戳改 RFC3339
- `internal/app/helpers.go`：`formatTimestamp` 改为 `t.UTC().Format(time.RFC3339)`。
  历史 CSV、plan "Generated"、日志行同源收敛；历史 `Timestamp` 仍按字符串排序（RFC3339 字典序
  正确）。新增 `TestFormatTimestampIsRFC3339UTC`。

### 6.4 V4 — 受限落地
- 经核实用例：`cloneDrivers` 目前在 `CompareOSDriverView` 中已是**唯一**所有权入口（无"每个入口各
  clone 一份"的散落）。本轮维持该单点并强化注释；**未**做完整三职模型拆分（`Driver` 去除
  Assessment 字段），因其会改动 `guiExportPayload`/plan 依赖的 `Driver` JSON 线契约——不属
  "行为不变"。全量改型作为后续非阻塞方向保留。

### 6.5 V5 — 超时吞掉 killTree 失败
- `internal/install/install.go`：`ProcessResult` 新增 `KillErr error`（仅超时且杀树失败时非 nil）；
  新增 `timeoutErr(prefix, result)` 在超时错误里捎带杀树失败详情，替换各处 `"... timed out"` 直写。

### 6.6 V6 — 延后（非阻塞）
-正确批次化需把 `inventory.GetDeviceDriverVersions`/`compare.ResolveLocalDriverVersion` 从
  `[]string`（无 id 关联）改为 id→version 映射，或在 app 层改"预收集全量设备再统一查询"——
  两者都会改变错误出现时机/无测试兜底的 PowerShell 语义。在本轮"行为不变"约束下不强行落地，
  列入后续。现有每驱动单次查询行为保持不变。

### 6.7 V7 — `selectByCodes` 单遍
- `internal/app/helpers.go`：改为"请求 set + 一遍驱动扫描"：`Selected`/`NotApplicable` 顺序
  仍按驱动表顺序，`Missing` 用 set 剩余项判定，去掉内部二次全扫描。现有两则 `selectByCodes`
  测试继续通过。

### 6.8 V8 — 决策/展示分离
- `internal/app/view.go`：把 `CompareOSDriverView` 内的"写 plan + 打印表格/汇总"抽成
  `presentDriverView(ctx, selected)`；`CompareOSDriverView` 收敛为"构建+评估+分区"的决策管道，
  `present` 为唯一把决策耦合到 stdout/plan 的地方。行为不变。

---

## 7. 全量复验

| 检查 | 结果 |
|------|------|
| `go build ./...` | BUILD_OK（exit 0） |
| `go vet ./...` | VET_OK（exit 0） |
| `go test ./...` | 全部包 ok，含新增 `V1/V2/V3` 单测 |
| `gofmt -l cmd/ internal/` | 空（干净） |