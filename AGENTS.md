<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — 指向 `docs/spec/coding/` 的指针（编码规范的单一权威在那里，因为 `.trellis/` 被 gitignore）
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->

## CI 触发顺序

凡需要触发 CI，一律**先转公开再跑**。禁止先以私有跑 CI、发现跑不通、再转公开重跑——那是把一次已知的失败当探测用，白烧一轮 runner，还会让失败原因在两次运行之间被覆盖。

门禁 `scripts/verify.ps1` 的 `the CI trigger is public` 强制这条：工作流里出现私有可见性设置即编译期报错。

## 驱动审计脚本不变量
> 语义展开与强制点见 `docs/spec/invariants.md`。

部署形态：**作者自用的审计工具，现已带安装器与更新通道**。原"个人单机审计脚本"定位在
`docs/decisions/distribution-frozen.md` 中冻结，由 `docs/decisions/distribution-unfrozen.md`
解冻——解冻的条件与代价写在那份记录里（无 Authenticode 证书、更新私钥成为关键资产、
D0 的问题仍未回答，因此仍不对陌生用户分发）。遥测与品牌合规**仍然冻结**。

以下性质是不可回退约束。产品能力（签名、升级）只能作为核心之外的层加入，不得违反：

1. 确定性：CLI 引擎零遥测、无隐藏联网、无后台服务。
2. 证据优先：计划文件保留原始证据行；桌面 GUI 只做薄渲染，不折叠“待定”与来源审计。
3. 默认不动：`Local newer`、未观察到设备问题、证据不足，一律不进自动安装集。无问题码只表示未观察到问题，不写成“正常”。
4. 事实分级：计划、控制台、交互提示、GUI 导出统一输出 fact/inference/undetermined。
5. 审计先于状态变更：历史账本追加写入，写入失败不提交成功结果。
6. 回退语义：设备回退是设备级操作（回退驱动程序或重装旧 INF）；`pnputil /delete-driver` 是包清理不是回退，不得作为自动回退动作。

## 规范边界与文档地图

- 产品/架构规范（人读，单一源）：`docs/spec/` — fact-standard / invariants / architecture / contracts / behavior。
- 编码规范（人读，单一源）：`docs/spec/coding/` — directory / error / logging / quality / single-source / guides。
- 任务导向手册：`docs/howto/` — run / develop / verify-rollback。
- 决策记录：`docs/decisions/`（`distribution-frozen.md` 为历史决定，`distribution-unfrozen.md` 与 `mygo-modules-declined.md` 为当前决定）。
- 历史证据与日志（append-only，非规范）：`docs/records/`。
- 入口与总地图：`README.md`。

两套规范的边界：`docs/spec/` 回答“系统怎么分层、外部契约是什么、运行时行为语义、决策依据”；`docs/spec/coding/` 回答“在某一层怎么写代码、错误/日志/目录/质量怎么管”。改前者同步代码行为，改后者同步编码约定。`.trellis/spec/` 只保留指向编码规范的指针，不再承载副本。

## 测试条款的项目级覆盖

用户级默认指令说“0 容忍单元测试、测试是编译器和类型系统的职责”。本仓库显式覆盖这一条，理由是机器层已经把它当作门禁前提：

- `scripts/verify.ps1` 有逐包覆盖率下限，删测试会让门禁直接红。
- 门禁规则 `tests never write to the real audit directory`、`evidence chain runs on real fixtures` 都要求具名测试函数存在。
- WORM 账本哈希链、回退语义、证据链的回归保护只有这些测试提供。

因此在本仓库：**改行为要加或改测试；删测试属于破坏门禁。** 这条覆盖是项目级决定，只在本仓库生效。
