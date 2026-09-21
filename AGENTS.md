<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->

## 驱动审计脚本不变量

部署形态已定为个人单机审计脚本。产品尾（自签名、支持矩阵、安装器/卸载器、升级、遥测、品牌合规）不在本规格内，保持冻结。

以下性质是个人审计脚本规格的不可回退约束。产品能力（签名、升级、遥测）只能作为核心之外的层加入，不得违反：

1. 确定性：CLI 引擎零遥测、无隐藏联网、无后台服务。
2. 证据优先：计划文件保留原始证据行；WPF 只做薄渲染，不折叠“待定”与来源审计。
3. 默认不动：`Local newer`、未观察到设备问题、证据不足，一律不进自动安装集。无问题码只表示未观察到问题，不写成“正常”。
4. 事实分级：计划、控制台、交互提示、GUI 导出统一输出 fact/inference/undetermined。
5. 审计先于状态变更：历史账本追加写入，写入失败不提交成功结果。
6. 回退语义：设备回退是设备级操作（回退驱动程序或重装旧 INF）；`pnputil /delete-driver` 是包清理不是回退，不得作为自动回退动作。
