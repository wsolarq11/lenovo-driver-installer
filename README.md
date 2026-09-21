# Lenovo 驱动安装器（个人单机审计脚本）

Go 引擎 + WPF 桌面的联想驱动审计/安装工具。运行时解析机型与主机编号，查询联想官方驱动接口，与本机已装版本对比，只安装被选中的适用驱动。Go CLI 是确定性引擎，WPF 只做薄渲染、不实现任何驱动决策逻辑。

## 文档地图

| 想做什么 | 看这里 |
| --- | --- |
| 理解“驱动该不该更新、跨 OS 装不装、以谁为准”的决策标准 | `docs/spec/fact-standard.md` |
| 理解六条不可回退的不变量 | `AGENTS.md`（权威清单）· `docs/spec/invariants.md`（语义展开） |
| 理解架构、仓库布局、端到端数据流 | `docs/spec/architecture.md` |
| 查外部契约：API / GUI JSON / 退出码 / 产物与账本列 | `docs/spec/contracts.md` |
| 查运行时行为：比较与选择 / 下载完整性 / 安装分发 / 回退 | `docs/spec/behavior.md` |
| 构建 / 运行 / 快速上手 / 排障 | `docs/howto/run.md` |
| 开发流水线 / 质量门禁 / CI / 真机 smoke | `docs/howto/develop.md` |
| 真机回退行为验收（一次性 VM 计划） | `docs/howto/verify-rollback.md` |
| 分发为何冻结（决策记录） | `docs/decisions/distribution-frozen.md` |
| 追溯历史：82JQ 证据 / 工作日志 / 评审 / 调研 | `docs/records/` |

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

完整参数表、退出码与排障见 `docs/howto/run.md`。

## 项目边界

部署形态是**个人单机审计脚本**：CLI 引擎零遥测、无隐藏联网、无后台服务。签名、支持矩阵、安装器/卸载器、升级、遥测、品牌合规等产品尾保持冻结，理由见 `docs/decisions/distribution-frozen.md`。六条不变量见 `AGENTS.md`。

进入纯维护模式（不再加特性）需同时满足：`scripts/verify.ps1` 全绿（含零网络导入检查）；WORM 账本哈希链可证伪篡改；回退只在“问题码恢复且驱动版本已变”时记 `RolledBack`，不记假恢复；分发问题保持冻结（D0–D5 未被重新决策推翻）。
