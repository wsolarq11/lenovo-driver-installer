# Lenovo 驱动安装器（作者自用的审计工具）

Go 引擎 + mygo 原生 UI 桌面的联想驱动审计/安装工具。运行时解析机型与主机编号，查询联想官方驱动接口，与本机已装版本对比，只安装被选中的适用驱动。Go CLI 是确定性引擎，桌面 GUI（`internal/gui`）只做薄渲染、不实现任何驱动决策逻辑。

## 文档地图

| 角色 | 入口 | 要回答的问题 |
| --- | --- | --- |
| 使用者 | `docs/howto/run.md` | 怎么跑、怎么审计一台机器、怎么排障 |
| 开发者 | `docs/howto/develop.md` · `docs/howto/verify-rollback.md` | 怎么改代码、门禁是什么、回退怎么验收 |
| 理解系统 | `docs/spec/index.md` | 分层、契约、行为、驱动决策标准的单一入口 |
| 决策与历史 | `docs/decisions/` · `docs/records/` | 为什么这样定（冻结）、历史证据与日志 |

编码规范在 `docs/spec/coding/`，与 `docs/spec/` 其余部分边界不同：前者回答“在某一层怎么写代码”，后者回答“系统怎么分层、外部契约是什么、行为语义、决策依据”。`.trellis/spec/` 只保留指向它的指针。

## 30 秒上手

首次运行会自动构建 `bin\lenovo-driver.exe`，随后进入交互选择：

```bat
install_lenovo_drivers.bat
```

只审计、不下载不安装：

```bat
install_lenovo_drivers.bat -DryRun
```

桌面界面（启动时经 UAC 提权，单实例窗口）：

```bat
install_lenovo_drivers_gui.bat
```

运行参数见 `-Help`（机器源 `internal/app/help.go`）；用法与排障见 `docs/howto/run.md`。

## 项目边界

部署形态是**作者自用的审计工具**：CLI 引擎零遥测、无隐藏联网、无后台服务；GUI 的更新检查只在点击托盘菜单时发生，未配置更新源的构建一个包都不发。分发尾一度冻结，现按 `docs/decisions/distribution-unfrozen.md` 解冻（安装器与签名更新通道已就位）；遥测与品牌合规仍冻结，且因缺少 Authenticode 证书，尚不对陌生用户分发。六条不变量见 `AGENTS.md`（清单权威）与 `docs/spec/invariants.md`（语义展开）。

进入纯维护模式（不再加特性）需同时满足：`scripts/verify.ps1` 全绿（含零网络导入检查）；WORM 账本哈希链可证伪篡改；回退只在“问题码恢复且驱动版本已变”时记 `RolledBack (recovered)`，恢复未经核验的两种情形（需重启、核验不可用）在账本里显式带 caveat 而不是记成已恢复，未恢复记 `RollbackFailed`；分发问题按 `docs/decisions/distribution-unfrozen.md` 解冻记录执行（D0 仍未回答，故仅作者自用）。
