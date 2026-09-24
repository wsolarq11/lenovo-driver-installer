# 六条不变量

部署形态已定为**个人单机审计脚本**。产品尾（自签名、支持矩阵、安装器/卸载器、升级、遥测、品牌合规）不在本规格内，保持冻结。

六条不变量是审计脚本规格的不可回退约束。产品能力（签名、升级、遥测）只能作为核心之外的层加入，不得违反任何一条。

三处出现，但权威关系固定：`AGENTS.md` 是清单权威（硬门禁，CI 阻断），本文件是语义展开的唯一权威（每条“禁止什么、在哪里强制”）；`docs/decisions/distribution-frozen.md` 里的六条是决策时的引述（冻结，不随语义演进）。改语义只改本文件，改清单只改 `AGENTS.md`，决策记录不参与演进。

## 1. 确定性

- 约束：CLI 引擎零遥测、无隐藏联网、无后台服务。
- 强制点：`scripts/verify.ps1` 的“零网络导入”检查——`internal/install`、`internal/compare`、`internal/inventory`、`internal/plan`、`internal/audit` 不得 import `net/http` / `net/url`，只有 `download` / `api` 能碰网络。运行时只派生到原生 Windows API 与联想官方接口，从不派生 PowerShell。

## 2. 证据优先

- 约束：计划文件保留原始证据行；WPF 只做薄渲染，不折叠“待定”与来源审计。
- 强制点：`Source audit` 输出类别 + 结论 + 证据行，`setupapi.*.log` 的 `cmd:` 原始命令行原样进入计划。WPF 不实现比较/来源逻辑，只渲染 Go 引擎的 JSON。

## 3. 默认不动

- 约束：`Local newer`、未观察到设备问题、证据不足，一律不进自动安装集。无问题码只表示“未观察到问题”，不写成“正常”。
- 强制点：`CM_Get_DevNode_Status` 问题码为 0 只表示未观察到问题，不证明功能正常；计划不把“无问题码”写成“正常”。来源审计的 provenance 归因是**推断**级（本机实测 `setupapi.*` 日志会轮转截断、历史导入行不可复现，无法事后对照 DriverStore ground truth），不得当事实展示。

## 4. 事实分级

- 约束：计划、控制台、交互提示、GUI 导出统一输出 fact / inference / undetermined。
- 强制点：四级（实测 fact / 推断 inference / 待定 undetermined）贯穿所有输出通道，任何通道不得把推断或待定渲染成事实。

## 5. 审计先于状态变更

- 约束：历史账本追加写入，写入失败不提交成功结果。
- 强制点：安装前先写 `Install` 意图行、回退前先写 `Rollback` 意图行，写入失败则跳过该动作（不提交）。账本每行带 SHA-256 链哈希（`Hash` 结构性尾列），`verifyHistoryChain` 重算可证伪篡改/乱序/删除；回退动作派生 pending 前先校验链。

## 6. 回退语义

- 约束：设备回退是设备级操作（回退驱动程序或重装旧 INF）；`pnputil /delete-driver` 是包清理不是回退，不得作为自动回退动作。
- 强制点：回退优先 `DiRollbackDriver`（设备管理器“回退驱动程序”同一原语），无备份时用 `UpdateDriverForPlugAndPlayDevicesW` + `INSTALLFLAG_FORCE` 强制绑回旧 INF；`RemoveDriverPackage` 仅作为包清理原语保留，注释明确“不是回退”。回退成功判据 = 问题码恢复 **且** 驱动版本已变，不是 API 返回值。
