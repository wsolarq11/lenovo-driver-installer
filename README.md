# Lenovo 驱动安装器（个人单机审计脚本）

Go 引擎 + WPF 桌面的联想驱动审计/安装工具。运行时解析机型与主机编号，查询联想官方驱动接口，与本机已装版本对比，只安装被选中的适用驱动。Go CLI 是确定性引擎，WPF 只做薄渲染、不实现任何驱动决策逻辑。

## 文档地图

| 角色 | 入口 | 要回答的问题 |
| --- | --- | --- |
| 使用者 | `docs/howto/run.md` | 怎么跑、怎么审计一台机器、怎么排障 |
| 开发者 | `docs/howto/develop.md` · `docs/howto/verify-rollback.md` | 怎么改代码、门禁是什么、回退怎么验收 |
| 理解系统 | `docs/spec/index.md` | 分层、契约、行为、驱动决策标准的单一入口 |
| 决策与历史 | `docs/decisions/` · `docs/records/` | 为什么这样定（冻结）、历史证据与日志 |

编码规范（agent 注入）在 `.trellis/spec/`，与 `docs/spec/` 边界不同：前者回答“在某一层怎么写代码”，后者回答“系统怎么分层、外部契约是什么、行为语义、决策依据”。

## 30 秒上手

首次运行会自动构建 `bin\lenovo-driver.exe`，随后进入交互选择：

```bat
install_lenovo_drivers.bat
```

只审计、不下载不安装：

```bat
install_lenovo_drivers.bat -DryRun
```

桌面界面：

```bat
install_lenovo_drivers_wpf.bat
```

运行参数见 `-Help`（机器源 `internal/app/help.go`）；用法与排障见 `docs/howto/run.md`。

## 项目边界

部署形态是**个人单机审计脚本**：CLI 引擎零遥测、无隐藏联网、无后台服务。签名、支持矩阵、安装器/卸载器、升级、遥测、品牌合规等产品尾保持冻结，理由见 `docs/decisions/distribution-frozen.md`。六条不变量见 `AGENTS.md`（清单权威）与 `docs/spec/invariants.md`（语义展开）。

进入纯维护模式（不再加特性）需同时满足：`scripts/verify.ps1` 全绿（含零网络导入检查）；WORM 账本哈希链可证伪篡改；回退只在“问题码恢复且驱动版本已变”时记 `RolledBack`，不记假恢复；分发问题保持冻结（D0–D5 未被重新决策推翻）。
