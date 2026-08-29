# 极端代码质量评审报告

评审日期：2026-08-29
评审对象：`main` 分支 `24f1ed0` 上的工作区变更，Trellis 0.6.16 更新
评审方式：极端可维护性评审

## 范围

工作区 diff 包含 23 个文件，`+3275 / -587` 行。实质性变更位于 `.trellis/scripts/`、`.agents/skills/`、`.dsh/skills/` 和 `.trellis/workflow.md`。Go 安装器不在本 diff 中，其迁移已单独提交。

## 结论

不要按当前形态合并这次更新。该 diff 通过大型状态机、重命名/反向引用重写、归档自动提交和会话指针重定向增加了高风险行为，同时把文件推到远超项目规模限制的程度，并且几乎没有新增自动化回归测试。

## 发现

### 1. 阻塞项：本 diff 将两个文件推到 1000 行以上

- `.trellis/scripts/common/task_store.py`：`985 -> 1874` 行。
  - `cmd_create` 383 行（`task_store.py:295-677`）
  - `cmd_archive` 182 行（`task_store.py:1223-1404`）
  - `cmd_rename` 128 行（`task_store.py:1003-1130`）
  - `_auto_commit_archive` 105 行（`task_store.py:1407-1511`）
- `.trellis/scripts/add_session.py`：`681 -> 1545` 行。
  - `add_session` 263 行（`add_session.py:1187-1449`）
  - `update_index` 112 行（`add_session.py:952-1063`）
  - `_auto_commit_workspace` 94 行（`add_session.py:1070-1163`）

这违反了项目的 500 行文件约束，也违反了“不允许把文件从 1000 行以下推到 1000 行以上而没有强结构理由”的评审门槛。这里没有这样的理由。

合并前应拆分：

- 将 `task_store.py` 拆成 `create.py`、`rename.py`、`archive.py`、`subtask.py`、`setters.py` 等聚焦模块，当前文件只保留命令分发。
- 将 `add_session.py` 拆成 `session/fingerprint.py`、`session/render.py`、`session/git.py`、`session/recorder.py` 等模块。

现有状态机注释很好，但实现仍把过多职责放在一个函数和一个文件里。

### 2. 阻塞项：`task_store.py` 仍有大量近似重复的命令体

- `cmd_add_subtask`（`task_store.py:1557-1627`）和 `cmd_remove_subtask`（`task_store.py:1634-1697`）重复了相同的父/子解析、JSON 读取、错误报告和双文件写入序列。真正差异只是追加/移除以及 parent/None 变更。
- `cmd_set_branch`（`task_store.py:1704`）、`cmd_set_base_branch`（`task_store.py:1746`）、`cmd_set_scope`（`task_store.py:1792`）、`cmd_set_meta`（`task_store.py:1834`）是四个近乎相同的字段设置命令。应合并为一个通用 `_set_task_field(args, field, label)` 帮助函数，或一个小型 `TaskFieldUpdater` 对象。

这正是评审要抓的复制粘贴控制流。一个帮助函数可删除约 200 至 300 行，并避免四个 setter 各自漂移。

### 3. 阻塞项：新增高风险行为没有任何测试

`.trellis/scripts/` 下没有任何测试文件。本 diff 新增了：

- `add_session.py` 中的待处理记录分类器和重试状态机
- 提交证据校验和指纹兼容
- 重命名规划、JSONL 重写、反向引用重写、会话指针重定向
- 带 index.lock 重试的归档自动提交
- `active_task.py` 中的 shell ticket 上下文键解析

这些都没有自动化覆盖。`scripts/verify.ps1` 只检查 Go 和 WPF PowerShell 层，完全不解析或测试 `.trellis` 下的 Python 脚本。

合并前应补充聚焦测试：

- `add_session.py`：absent/journal/index/committed 状态迁移、重复 marker、已提交记录 generation 行为、legacy fingerprint 查找。
- `task_store.py`：create 校验、父任务链接、rename dry-run 和实际应用、反向引用重写、归档子任务解链、自动提交范围。
- `active_task.py`：上下文键解析、shell ticket 新鲜度、任务引用隔离、会话指针重定向。

并在 `scripts/verify.ps1` 增加 Python 验证步骤，防止这些文件静默回归。

### 4. 高优先级：安全敏感的路径解析逻辑被重复实现

`paths.resolve_task_ref`（`paths.py:280-347`）和 `active_task.resolve_task_ref`（`active_task.py:202-250`）实现了相同的隔离逻辑，包括 symlink 的 `.trellis` 例外。本 diff 明确让 `active_task.py` 对齐 `paths.py`，使漂移风险可见。

任务引用会存入会话运行时文件并在后续回合重放，因此这不是无意义的便利重复。一旦出现偏差，可能重新引入路径逃逸。应抽取一个零依赖的 `task_refs.py` 模块，或提供可加载唯一实现的独立入口，并补充 `..`、绝对路径、symlink、workflow 路径映射回写测试。

### 5. 中优先级：CLI 入口以及 list/start 命令仍然过大

- `task.py` 共 801 行。
- `main` 200 行（`task.py:598-797`），主要是 parser 构造。
- `cmd_list` 111 行（`task.py:374-484`），内部还有嵌套 renderer。
- `cmd_start` 102 行（`task.py:171-272`）。
- `show_usage` 67 行（`task.py:525-591`）。

声明式命令注册表配合自动生成 argparse/help，可消除大量样板代码，并把命令元数据放在各命令处理器旁。

### 6. 卫生问题：Python 行尾没有统一

`.gitattributes` 对 Go、Markdown、JSON、YAML、PowerShell、bat 文件都有明确的 `eol=lf` 规则，但没有 `*.py`。所有被修改的 Python 文件在 `git diff` 时都会输出 `LF will be replaced by CRLF` 警告。

合并前加入 `*.py text eol=lf` 并统一文件行尾，避免这个大 diff 携带意外的行尾变更。

## 验证证据

- `git diff --check`：干净，仅存在 LF/CRLF 警告。
- AST 解析：16 个被修改的 Python 文件全部通过。
- `python .trellis/scripts/add_session.py --help`：成功。
- `python .trellis/scripts/task.py --help`：成功。
- 新旧行数：
  - `task_store.py`：985 -> 1874
  - `add_session.py`：681 -> 1545
  - `active_task.py`：765 -> 857
  - `task.py`：612 -> 801
  - `task_utils.py`：309 -> 498
- `.trellis/scripts/` 下未发现测试文件。
