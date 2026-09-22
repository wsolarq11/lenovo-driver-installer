# 规格单一入口

本目录是**人读规范**的单一权威层：回答“系统怎么分层、外部契约是什么、运行时行为语义是什么、驱动决策以谁为准”。人与 agent 读这里，不看 `internal/` 代码也能理解边界；但凡是能由代码表述的事实，代码才是机器源，本文档只做语义展开或留指针。

## 单一权威原则

1. **每一条事实只在一处定义**。其它文档引用，不复制。复制一处，就多一处漂移面。
2. **机器源（代码）优先**。退出码、参数、产物路径、账本列、API 端点这类可枚举事实的权威是代码，不是文档。文档中的同义副本必须被门禁锁死；锁不住的，删掉副本只留指针。
3. **spec 写语义，不写实现**。分层、边界、数据流向、契约、行为语义属于 spec；函数调用顺序、循环细节、内部优化属于代码。实现一变 spec 就漂移的内容，不允许出现在 spec 里。
4. **事实分级贯穿**：spec 中的论断标注 实测 / 推断 / 待取证；实现性描述以 `internal/` 代码为准。

## 主题地图

| 我要回答的问题 | 权威文件 | 机器源（代码） | 门禁 |
| --- | --- | --- | --- |
| 驱动该不该更新、要不要跨 OS、以谁为准 | `fact-standard.md` | 决策标准（无机器源） | 逐驱动证据见 `docs/records/evidence-82jq.md` |
| 六条不可回退约束是什么、在哪强制 | `invariants.md` | 语义展开；清单权威在 `AGENTS.md` | `scripts/verify.ps1`（零网络导入等） |
| 系统怎么分层、数据按什么顺序流 | `architecture.md` | `internal/` 包结构 | 开发边界（不新增运行时引擎） |
| API / GUI JSON / 退出码 / 产物 / 账本列 | `contracts.md` | `internal/app/help.go`、`internal/app/history.go`、`internal/app/export.go`、`internal/api/` | `verify.ps1` 退出码比对、`history_test.go` golden |
| 比较选择 / 下载完整性 / 安装分发 / 回退语义 | `behavior.md` | `internal/compare`、`internal/download`、`internal/install`、`internal/audit` | golden 测试 |

## 手册与证据的位置

- 怎么做（构建 / 运行 / 开发 / 回退验收）：`docs/howto/`。
- 为什么这样定（决策记录，冻结）：`docs/decisions/`。
- 历史证据与日志（append-only，非规范）：`docs/records/`。
- 怎么写代码（agent 注入的编码规范）：`.trellis/spec/`。
