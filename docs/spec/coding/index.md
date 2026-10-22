# 编码规范

本目录是本仓库**编码约定的单一权威**：回答“在某一层怎么写代码、错误与日志怎么管、目录怎么分、质量怎么判”。

它和 `docs/spec/` 其它文件的分工：

| 问什么 | 去哪 |
| --- | --- |
| 系统怎么分层、外部契约、行为语义、决策依据 | `docs/spec/`（fact-standard / architecture / contracts / behavior / invariants） |
| 在某一层怎么写代码 | 本目录 |
| 怎么构建、运行、验收 | `docs/howto/` |

`.trellis/spec/` 不再承载编码规范的副本，只保留指向本目录的指针。原因写在 `single-source.md`：那份副本不受版本控制，门禁看不到它，它只会漂。

## 文件索引

| 文件 | 回答的问题 |
| --- | --- |
| [directory-structure.md](./directory-structure.md) | 目录与包职责、模块拆分线、命名 |
| [error-handling.md](./error-handling.md) | 失败怎么表示、怎么传播、退出码 |
| [logging-guidelines.md](./logging-guidelines.md) | 日志格式、级别、产物位置 |
| [quality-guidelines.md](./quality-guidelines.md) | 必须遵守与禁止的模式、评审清单 |
| [single-source.md](./single-source.md) | 可枚举事实的唯一源与变更规程 |
| [guides/](./guides/index.md) | 写代码前的思考触发清单 |

## 开发前清单

- [ ] 读 `README.md`、`docs/spec/fact-standard.md` 与当前任务。
- [ ] 确认改动落在哪一层：确定性 / 联网 / 副作用（`docs/spec/architecture.md` §2.1 是唯一分类表）。
- [ ] 保留公开参数、交互选项、退出码、缓存规则、安装器回退，除非任务本身要改契约。
- [ ] 改动完成前跑 `scripts/verify.ps1`。

## 质量清单

- [ ] 没有模板占位符残留。
- [ ] 文档里的论断指向仓库中真实存在的文件。
- [ ] 没有新增外部依赖。
- [ ] `git diff --check` 干净。
- [ ] 新增生产文件不超过 `scripts/verify.ps1` 声明的行数上限。
