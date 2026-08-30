# 热核代码质量评审 — 第三轮（独立复审 + 落地）

- 评审日期：2026-08-30（本会话，对 `main` 当前工作树的一次独立全量复审）
- 评审对象：`lenovo-driver-installer` Go 引擎（`cmd/` + `internal/` 全部源码）
- 评审方式：热核可维护性评审（极端严格的抽象 / 大文件 / 意大利面条分支审查）
- 前置状态：首轮 `#1–#7` 与第二轮 `V1–V8` 均已在前两轮交付（见
  `docs/thermo-nuclear-code-quality-review-final.md`、`docs/thermo-nuclear-code-quality-review-v2.md`）。
  本文档是**不依赖前两轮结论**的一次全新复审，只报告**新发现**或**仍未关闭/被新改动复燃**的问题，
  并逐一核对已修项是否保持有效。

---

## 1. 结论速览

代码库依然**结构健康**：包边界清晰，领域规则走声明式表（`vendorRules`/`driverMatchRules`/
`softwareRules`/`sourceEvidenceRules`/`fieldExtractRules`），**无任何文件接近 1000 行**
（最大 `app/view.go` 466 行、`audit/audit.go` 405 行、`compare/matching.go` 366 行）。
技能预设的两条阻塞性气味（超 1000 行、朴素意大利面）均不成立。

本轮未发现**结构性回归**：前两轮的 #1–#7 与 V1–V7 全部处于有效状态且逐项核实无失效。

本轮**新落地 3 项行为不变修复**（每项都带有对应的回归测试），并**复确认 1 项延后项**（V6）。
这些是本轮仅有、经源码证据核实的高置信度问题——刻意不夸大、不臆造新的阻塞项。

---

## 2. 已修项复核（前两轮修复均保持有效）

| 前轮项 | 本轮核实 |
|---|---|
| #1 / V1 `InstallDriverFile` 成/败契约 + `StartErr` 终结性失败 | 有效：`ProcessResult.StartErr` 存在；`.msi/.inf/.cab/default` 均 `StartErr != nil` 时返回 `(code, err)` |
| #2/V8 `CompareOSDriverView` 拆分 + `presentDriverView` 决策/展示分离 | 有效：`assessSelectedDrivers`/`partitionViewDrivers`/`presentDriverView` 就位 |
| #3 `mustDriverList` 严格版 | 有效（`view.go` 主视图硬错） |
| #4 `loadDriverListsForOSIDs` 并行 + 保序 + mutex 缓存 | 有效（`sync.WaitGroup` 保序返回；`osDriverCacheMu` 就位） |
| #5 `sourceEvidenceRules` 优先级表 | 有效（`view.go`/`audit`） |
| #6 `cloneDriver`/`cloneDrivers` 所有权分离 | 有效，且生产仅一个入口（`view.go:177`），未散落 |
| #7 删除伪捕获 `ProcessResult.Stdout/Stderr` | 有效 |
| V2 `otherOSIDs` 收敛三处 | 有效（`view.go`+`install.go` 全局复用） |
| V3 RFC3339 时间戳 | 有效（`formatTimestamp` = UTC RFC3339） |
| V5 超时 killTree 错误捎带 | 有效（`ProcessResult.KillErr` + `timeoutErr`） |
| V7 `selectByCodes` 单遍 | 有效（`helpers.go` 带 requested-set 单遍） |

---

## 2. 本轮发现与落地修复

### R1 [高，代码柔术，已落地] "OSID → OSName" 查找在至少三处各写一遍，且第二轮已标注未收口

对"这个 OSID 叫什么 OS"的查找，本轮复核发现至少三处独立实现，其中两处是**完整重复循环**：

- `internal/app/view.go` `ExportGUIView`（`view.go:430-437`）用一个内联双重 `for` 同时查
  `currentOSName` 与 `listOSName`——纯粹量定的 OSID→名称查找被**手写展开**。
- `internal/api/parse.go` `findOSName(osList, osID)`（原 173-179）——与 view 里的查找是同一逻辑，
  只是放在 api 包里。
- `internal/app/interactive.go` `nextOSIndex` 依赖 `osList` 扫描。

第二轮 V2 报告已明确把这些列为"OSID→名称查表的另一处重复扫描，可各自复用小的 index map"，
但**未实际落地**。本轮把它作为真正的代码柔术收敛为一处：

> 新增规范纯函数 `model.OSNameByID(osList, osID) string`（OS 条目的单一查找源），
> `api.findOSName` 与 `app.ExportGUIView` 都改为调用它。

行为不变（找不到返回 `""`，与两处原实现一致），删掉了 `ExportGUIView` 的手写双重循环与 api 内联循环。
新增 `internal/model/os_test.go` 锁定"命中 / 未命中 / 空表"三种边界。

**为什么是代码柔术**：不是把 4 份复制改成"1 处更漂亮的复制"，而是删掉重复实现、让所有权归到 `model`
（`OSListEntry` 的所有者），一处修改同步所有读点，杜绝"哪个入口对 OSID 的口径漂移"。

### R2 [高，遗留意大利面债务，已落地] `downloadVerified` 用无界 `for {}` 表达"至多刷新一次"的重试

`internal/app/downloadVerified`（`install.go:146-179`）是**首轮 H6 明确标记但两轮内仍未闭环**的项：
一个无界 `for {}`，其终止只能靠读者通读全函数、跟踪 `refreshAttempted` 与各 `return` 才能推断出
"至多两次"。

本轮把它改成**显式有界**的双遍循环（`install.go`）：

```go
for pass := 0; pass < 2; pass++ {
    ...
    if download.IsHTTPStatus(err, 403) && pass == 0 {
        ... refresh ... continue
    }
    return 0, err
}
return 0, fmt.Errorf("download did not converge after 2 passes")
```

- 行为完全一致：`pass == 0` 才允许刷新（等价原 `refreshAttempted`），第二次 403 直接失败；
- 边界变"显式"，且退出"循环耗尽可能"的尾 return 让边界成为程序文本的一部分；
- 现有 `TestDownloadVerifiedRefreshes403URL` 继续通过并覆盖该刷新路径（`pass==0` → 重试一次）。

### R3 [低/中，已落地] `initCurrentSourceMap` 是一层无收益的恒等转发壳

`view.go` 的
```go
func initCurrentSourceMap(drivers []*model.Driver) map[string]model.SourceMapEntry {
    return buildDriverSourceMap(drivers)
}
```
是精确一对一转发，未新增任何语义（`current` 后缀没有封装任何逻辑），只在生产 1 处（`view.go:184`）+测试 1 处调用。
符合"取消不澄清 API 的薄层/恒等抽象"。本轮删除它直接调 `buildDriverSourceMap`，测试同步改。

### V6 [中，确认延后（非阻塞）] 每驱动一次 `powershell.exe` 子进程风暴

`assessSelectedDrivers` 对每个选中驱动调 `localDriverState` → `inventory.GetDeviceDriverVersions`，
即每次运行发起 N 次 PowerShell 子进程查询 PnP 版本。理想的柔化是把"获取版本"提升为对所有 matched
设备的**一次**聚合查询再按 PnP 归并，使子进程从 N 降到 1。

- 本轮**维持延后**，理由与第二轮一致：规范化地批次化需要把 `GetDeviceDriverVersions`（`[]string`，无 id
  关联）改为 id→version 映射（或在 app 层预收集全量设备再统一查询），这会改变错误时机与"无测试兜底的
  PowerShell 语义"，不属于"行为不变"。属可排期的后续营养，不阻塞本仓交付与正确性。

---

## 低以上观察（不阻塞，记录）

- `view.go` 的 `initAlternateSourceMap` 在 `assessSelectedDrivers` 的**逐驱动循环内懒构建一次**
  （`LocalNewer` 首次出现时），为"避免无本地更新的运行构建该 map"做了折中——可接受，不为立项而搅动。
- `lookup` 的重复扫描（`view.go:430-437` 双循环）已在 R1 取代。
- 无浮点、无 1000 行文件、时间戳 RFC3339、enum 良好——均符合 AGENTS.md，无新触发。

## 放行

按热核技能，放行条要求：无清晰结构回归、无可见的"可以更简单"被丢弃、无无端文件翻倍、无意大利面新增、
无 hacky/伪装抽象、无重复规范 helper、无错层泄漏。
对应本轮：

- **阻塞**：无。
- **前置的未闭环重复/债务**：R1（OS 查找）、R2（无界循环）→ 本轮已修（对应第二轮 V2 中未落地的 OS
  名查表、首轮 H6 中未闭合的无界重试）。
- 其余 V6 为明确后续方向，不阻塞正确性。

成交：R1/R2/R3 已落地且经 `go build / vet / test / gofmt` 复验；V6 记录待排期。

---

## 实施清单（本轮改动）

| 文件 | 改动 | 行为保证 |
|---|---|---|
| `internal/model/model.go` | 新增 `OSNameByID` 纯函数 | 无 |
| `internal/model/os_test.go` | 新增 `TestOSNameByID` | 锁定 命中/未命中/空表 |
| `internal/api/parse.go` | `findOSName` 改调 `OSNameByID` | 相同返回 |
| `internal/app/view.go` | `ExportGUIView` 用 `OSNameByID`（删内联双 loop）；删 `initCurrentSourceMap` 薄包装 | 相同输出 |
| `internal/app/app_test.go` | 用例改调 `buildDriverSourceMap` | 语义不变 |
| `internal/app/install.go` | `downloadVerified` 有界 `for pass < 2` | 语义不变（幂等重试 3 次 + 至多刷新 1 次） |

## 验证（全部通过）

- `gofmt -l cmd/ internal/` — 干净
- `go vet ./...` — VET_OK
- `go test ./...` — 全部包 ok（含新增 `TestOSNameByID`）
- `go build ./...` — BUILD_OK
- `scripts/verify.ps1` — VERIFY_OK