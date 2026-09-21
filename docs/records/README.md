# records 层说明

本目录是**历史证据与日志**，不是规范，不参与行为承诺。规范在 `docs/spec/`。

## 衰减规则

- `worklog.md`：会话日志，append-only，不改写历史。
- `review-thermo-nuclear.md`：评审汇总，已归档；评审应“取代”而非“追加”，不再往里加新轮次。
- `benchmark-native-migration.md`：一次性调研，归档只读。
- `evidence-82jq.md`：82JQ 实测证据，`fact-standard.md` 的验证样本，随真机验证增补。

## 历史路径说明

本目录内（及 `.trellis/workspace/`、`.trellis/tasks/archive/`）历史记录中出现的旧路径（如 `docs/TECHNICAL.md`、`DRIVER_FACT_STANDARD.md`）是**当时的路径**，作为历史事实保留，不逐个改写。现行结构见 `README.md` 文档地图；`docs/TECHNICAL.md` 的内容已并入 `docs/spec/`。
